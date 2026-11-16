package pluginstore

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

const MaxImportArchiveBytes int64 = 64 << 20
const maxImportExpandedBytes uint64 = 128 << 20

// ImportedArchive contains static facts only; preparing it never loads a library.
type ImportedArchive struct {
	PluginID        string `json:"plugin_id"`
	Version         string `json:"version"`
	Platform        string `json:"platform"`
	LibraryPath     string `json:"library_path"`
	LibrarySHA256   string `json:"library_sha256"`
	LicenseRequired bool   `json:"license_required"`
	Integrity       string `json:"integrity"`
	Archive         []byte `json:"-"`
}

// PrepareImportArchive normalizes a manifest-bound library for InstallArchive.
func PrepareImportArchive(data []byte, goos, goarch string) (ImportedArchive, error) {
	var result ImportedArchive
	if int64(len(data)) > MaxImportArchiveBytes {
		return result, fmt.Errorf("plugin archive exceeds 64 MiB")
	}
	reader, errOpen := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if errOpen != nil {
		return result, fmt.Errorf("invalid plugin ZIP: %w", errOpen)
	}
	if len(reader.File) == 0 || len(reader.File) > 256 {
		return result, fmt.Errorf("plugin ZIP must contain 1 to 256 entries")
	}
	files := make(map[string]*zip.File)
	var manifestName string
	var expanded uint64
	for _, file := range reader.File {
		name, errName := cleanZipName(file.Name)
		if errName != nil {
			return result, errName
		}
		if strings.Contains(name, ":") {
			return result, fmt.Errorf("invalid ZIP entry path")
		}
		if _, exists := files[name]; exists {
			return result, fmt.Errorf("duplicate ZIP entry %q", name)
		}
		files[name] = file
		if file.FileInfo().IsDir() {
			continue
		}
		if !regularZipFile(file) {
			return result, fmt.Errorf("ZIP entry %q is not a regular file", name)
		}
		if file.UncompressedSize64 > maxImportExpandedBytes-expanded {
			return result, fmt.Errorf("plugin ZIP expanded size exceeds 128 MiB")
		}
		expanded += file.UncompressedSize64
		if path.Base(name) == "BINARY.json" || path.Base(name) == "PLUGIN.json" {
			if manifestName != "" {
				return result, fmt.Errorf("plugin ZIP contains multiple manifests")
			}
			manifestName = name
		}
	}
	if manifestName == "" {
		return result, fmt.Errorf("plugin ZIP requires BINARY.json or PLUGIN.json")
	}
	manifestData, errRead := readImportEntry(files[manifestName], 1<<20)
	if errRead != nil {
		return result, errRead
	}
	if !ojson.ValidBytes(manifestData) {
		return result, fmt.Errorf("invalid plugin manifest JSON")
	}
	manifest := ojson.ParseBytes(manifestData)
	if !manifest.IsObject() {
		return result, fmt.Errorf("plugin manifest must be an object")
	}
	result.Version = normalizeVersion(manifest.Get("version").String())
	result.Platform = manifest.Get("platform").String()
	result.PluginID = strings.TrimSpace(manifest.Get("id").String())
	result.LicenseRequired = manifest.Get("license_required").Bool()
	if !validPluginVersion(result.Version) {
		return result, fmt.Errorf("plugin manifest version is invalid")
	}
	if result.Platform != goos+"/"+goarch {
		return result, fmt.Errorf("plugin platform %q does not match host %s/%s", result.Platform, goos, goarch)
	}
	if goos != "linux" || (goarch != "amd64" && goarch != "arm64") {
		return result, fmt.Errorf("ZIP imports currently support Linux amd64/arm64 libraries")
	}
	base := path.Dir(manifestName)
	if base == "." {
		base = ""
	}
	var libraryName string
	for name, file := range files {
		if file.FileInfo().IsDir() || !hasDynamicLibraryExtension(name) {
			continue
		}
		if libraryName != "" {
			return result, fmt.Errorf("plugin ZIP contains multiple candidate libraries")
		}
		if base != "" && !strings.HasPrefix(name, base+"/") {
			return result, fmt.Errorf("plugin library is outside the manifest directory")
		}
		libraryName = name
	}
	if libraryName == "" {
		return result, fmt.Errorf("plugin ZIP does not contain a library")
	}
	relative := strings.TrimPrefix(strings.TrimPrefix(libraryName, base), "/")
	if result.PluginID == "" && relative == "plugin/dist/cpa-bps.so" {
		result.PluginID = "cpa-bps"
	}
	if !validPluginID(result.PluginID) {
		return result, fmt.Errorf("plugin manifest id is missing or invalid")
	}
	allowedName := result.PluginID + ".so"
	if path.Base(libraryName) != allowedName && path.Base(libraryName) != versionedPluginFileName(result.PluginID, result.Version, goos) {
		return result, fmt.Errorf("library filename does not match plugin identity")
	}
	checksums := manifest.Get("files")
	if !checksums.IsObject() {
		return result, fmt.Errorf("plugin manifest files must contain checksums")
	}
	var library []byte
	var errVerify error
	checksums.ForEach(func(key, value ojson.Result) bool {
		name, errName := cleanZipName(key.String())
		if errName != nil {
			errVerify = errName
			return false
		}
		full := path.Join(base, name)
		file, exists := files[full]
		if !exists || file.FileInfo().IsDir() {
			errVerify = fmt.Errorf("manifest entry %q is missing", name)
			return false
		}
		expected, errHex := hex.DecodeString(value.String())
		if errHex != nil || len(expected) != sha256.Size {
			errVerify = fmt.Errorf("invalid checksum for %q", name)
			return false
		}
		content, errContent := readImportEntry(file, int64(maxImportExpandedBytes))
		if errContent != nil {
			errVerify = errContent
			return false
		}
		actual := sha256.Sum256(content)
		if !bytes.Equal(expected, actual[:]) {
			errVerify = fmt.Errorf("checksum mismatch for %q", name)
			return false
		}
		if full == libraryName {
			library = content
			result.LibrarySHA256 = hex.EncodeToString(actual[:])
		}
		return true
	})
	if errVerify != nil {
		return result, errVerify
	}
	if library == nil {
		return result, fmt.Errorf("selected library is not bound by the manifest")
	}
	if len(library) < 20 || !bytes.Equal(library[:4], []byte{0x7f, 'E', 'L', 'F'}) || library[4] != 2 || library[5] != 1 {
		return result, fmt.Errorf("selected library is not a 64-bit little-endian ELF")
	}
	machine := uint16(62)
	if goarch == "arm64" {
		machine = 183
	}
	if binary.LittleEndian.Uint16(library[18:20]) != machine {
		return result, fmt.Errorf("library architecture does not match manifest platform")
	}
	var normalized bytes.Buffer
	writer := zip.NewWriter(&normalized)
	entry, errEntry := writer.Create(allowedName)
	if errEntry != nil {
		return result, errEntry
	}
	if _, errWrite := entry.Write(library); errWrite != nil {
		return result, errWrite
	}
	if errClose := writer.Close(); errClose != nil {
		return result, errClose
	}
	result.LibraryPath = relative
	result.Archive = normalized.Bytes()
	result.Integrity = "verified"
	return result, nil
}

func readImportEntry(file *zip.File, maximum int64) ([]byte, error) {
	if file.UncompressedSize64 > uint64(maximum) {
		return nil, fmt.Errorf("ZIP entry %q exceeds allowed size", file.Name)
	}
	stream, errOpen := file.Open()
	if errOpen != nil {
		return nil, errOpen
	}
	data, errRead := io.ReadAll(io.LimitReader(stream, maximum+1))
	errClose := stream.Close()
	if errRead != nil {
		return nil, errRead
	}
	if errClose != nil {
		return nil, errClose
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("ZIP entry %q exceeds allowed size", file.Name)
	}
	return data, nil
}
