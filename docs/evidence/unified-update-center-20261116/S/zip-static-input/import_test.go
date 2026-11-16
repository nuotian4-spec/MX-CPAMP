package pluginstore

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
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
