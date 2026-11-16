package pluginstore

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

func importFixture(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestPrepareImportArchiveRejectsInvalidPackages(t *testing.T) {
	library := append([]byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3, 0, 62, 0}, []byte("static fixture")...)
	manifest := fmt.Sprintf(`{"id":"sample","version":"1.0.0","platform":"linux/amd64","files":{"sample.so":"%x"}}`, sha256.Sum256(library))
	for _, tc := range []struct {
		name   string
		change func(map[string][]byte)
	}{
		{"missing manifest", func(files map[string][]byte) { delete(files, "PLUGIN.json") }},
		{"multiple manifests", func(files map[string][]byte) { files["BINARY.json"] = []byte(manifest) }},
		{"multiple libraries", func(files map[string][]byte) { files["extra.so"] = library }},
		{"path traversal", func(files map[string][]byte) { files["../outside"] = []byte("data") }},
		{"identity mismatch", func(files map[string][]byte) {
			files["PLUGIN.json"] = []byte(strings.Replace(manifest, `"id":"sample"`, `"id":"other"`, 1))
		}},
		{"platform mismatch", func(files map[string][]byte) {
			files["PLUGIN.json"] = []byte(strings.Replace(manifest, "linux/amd64", "linux/arm64", 1))
		}},
		{"checksum mismatch", func(files map[string][]byte) { files["sample.so"] = append(bytes.Clone(library), 'x') }},
		{"missing bound library", func(files map[string][]byte) {
			files["PLUGIN.json"] = []byte(`{"id":"sample","version":"1.0.0","platform":"linux/amd64","files":{}}`)
		}},
		{"architecture mismatch", func(files map[string][]byte) {
			altered := bytes.Clone(library)
			altered[18] = 183
			files["sample.so"] = altered
			files["PLUGIN.json"] = []byte(strings.Replace(manifest, fmt.Sprintf("%x", sha256.Sum256(library)), fmt.Sprintf("%x", sha256.Sum256(altered)), 1))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string][]byte{"PLUGIN.json": []byte(manifest), "sample.so": bytes.Clone(library)}
			tc.change(files)
			if _, err := PrepareImportArchive(importFixture(t, files), "linux", "amd64"); err == nil {
				t.Fatal("invalid archive accepted")
			}
		})
	}
	for _, tc := range []struct {
		name      string
		entries   int
		symlink   bool
		duplicate bool
	}{
		{"entry limit", 257, false, false}, {"symbolic link", 1, true, false}, {"duplicate entries", 2, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			writer := zip.NewWriter(&output)
			for i := 0; i < tc.entries; i++ {
				name := fmt.Sprintf("item-%d", i)
				if tc.duplicate {
					name = "same"
				}
				header := &zip.FileHeader{Name: name}
				if tc.symlink {
					header.SetMode(os.ModeSymlink | 0o777)
				}
				entry, err := writer.CreateHeader(header)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = entry.Write([]byte("data")); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := PrepareImportArchive(output.Bytes(), "linux", "amd64"); err == nil {
				t.Fatal("invalid entry accepted")
			}
		})
	}
	archive := importFixture(t, map[string][]byte{"PLUGIN.json": []byte(manifest), "sample.so": library})
	central := bytes.Index(archive, []byte{'P', 'K', 1, 2})
	if central < 0 {
		t.Fatal("fixture central directory missing")
	}
	binary.LittleEndian.PutUint32(archive[central+24:central+28], uint32(maxImportExpandedBytes+1))
	if _, err := PrepareImportArchive(archive, "linux", "amd64"); err == nil || !strings.Contains(err.Error(), "expanded size") {
		t.Fatalf("expanded limit: %v", err)
	}
	if _, err := PrepareImportArchive(make([]byte, MaxImportArchiveBytes+1), "linux", "amd64"); err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("compressed limit: %v", err)
	}
}

func TestPrepareImportArchiveNormalizesUnifiedPackageWithoutExecuting(t *testing.T) {
	library := append([]byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3, 0, 62, 0}, []byte("inert test fixture")...)
	manifest := fmt.Sprintf(`{"format":"cpa-bps-binary-v1","version":"0.5.58","platform":"linux/amd64","license_required":true,"files":{"plugin/dist/cpa-bps.so":"%x"}}`, sha256.Sum256(library))
	archive := importFixture(t, map[string][]byte{
		"bundle/BINARY.json":            []byte(manifest),
		"bundle/plugin/dist/cpa-bps.so": library,
		"bundle/install.sh":             []byte("exit 97\n"),
		"bundle/panel/index.html":       []byte("leave existing panel unchanged"),
	})
	preview, err := PrepareImportArchive(archive, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if preview.PluginID != "cpa-bps" || preview.Version != "0.5.58" || !preview.LicenseRequired {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	reader, err := zip.NewReader(bytes.NewReader(preview.Archive), int64(len(preview.Archive)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 1 || reader.File[0].Name != "cpa-bps.so" {
		t.Fatalf("unexpected normalized files: %v", reader.File)
	}
	file, err := reader.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(file)
	if closeErr := file.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil || !bytes.Equal(got, library) {
		t.Fatalf("library changed: %v", err)
	}
}
