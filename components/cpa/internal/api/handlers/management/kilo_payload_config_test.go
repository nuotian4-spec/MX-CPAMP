package management

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"gopkg.in/yaml.v3"
)

func TestKiloPayloadManagedConfigGuard(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, kilo.StateFileName), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	h := &Handler{cfg: &config.Config{Kilo: config.KiloConfig{StateDir: dir}}}
	parse := func(raw string) *yaml.Node {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
			t.Fatal(err)
		}
		return doc.Content[0]
	}
	for _, test := range []struct {
		name, before, after string
		want                error
	}{
		{"legacy-toggle", "kilo: {proxy-limit: 2}", "kilo: {proxy-limit: 2, image-reduction-enabled: true}", nil},
		{"v8-toggle", "upstream: {kilo: {proxy-limit: 2}}", "upstream: {kilo: {proxy-limit: 2, image-reduction-enabled: true}}", nil},
		{"only-toggle", "other: 1", "kilo: {image-reduction-enabled: true}", nil},
		{"empty-toggle", "kilo: {}", "other: 1", nil},
		{"string", "kilo: {}", "kilo: {image-reduction-enabled: 'true'}", kilo.ErrStateInvalid},
		{"number", "kilo: {}", "kilo: {image-reduction-enabled: 1}", kilo.ErrStateInvalid},
		{"null", "kilo: {}", "kilo: {image-reduction-enabled: null}", kilo.ErrStateInvalid},
		{"section-scalar", "kilo: {}", "kilo: true", kilo.ErrStateInvalid},
		{"protected-change", "kilo: {proxy-limit: 2}", "kilo: {proxy-limit: 3, image-reduction-enabled: true}", kilo.ErrStateConflict},
		{"delete-section", "kilo: {proxy-limit: 2}", "other: 1", kilo.ErrStateConflict},
		{"nested-same-name", "kilo: {custom: {image-reduction-enabled: false}}", "kilo: {custom: {image-reduction-enabled: true}}", kilo.ErrStateConflict},
		{"unknown-key", "kilo: {}", "kilo: {future-key: 1}", kilo.ErrStateConflict},
		{"merge-toggle", "kilo: {proxy-limit: 2}", "defaults: &d {proxy-limit: 2}\nkilo: {<<: *d, image-reduction-enabled: true}", nil},
		{"merge-protected", "kilo: {proxy-limit: 2}", "defaults: &d {proxy-limit: 3}\nkilo: {<<: *d, image-reduction-enabled: true}", kilo.ErrStateConflict},
		{"root-merge-protected", "kilo: {proxy-limit: 2}", "defaults: &d {kilo: {proxy-limit: 3}}\n<<: *d", kilo.ErrStateConflict},
		{"alias-toggle", "kilo: {proxy-limit: 2}", "defaults: &d {proxy-limit: 2, image-reduction-enabled: true}\nkilo: *d", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := h.guardManagedKiloConfig(parse(test.after), parse(test.before))
			if !errors.Is(err, test.want) {
				t.Fatalf("guard=%v want=%v", err, test.want)
			}
			h.configFilePath = filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(h.configFilePath, []byte(test.before), 0600); err != nil {
				t.Fatal(err)
			}
			_, err = h.prepareLegacyKiloConfig([]byte(test.after))
			if !errors.Is(err, test.want) {
				t.Fatalf("legacy guard=%v want=%v", err, test.want)
			}
		})
	}
}
