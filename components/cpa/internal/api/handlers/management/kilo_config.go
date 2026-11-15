package management

import (
	"os"
	"path/filepath"
	"reflect"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"gopkg.in/yaml.v3"
)

func kiloConfigNode(root *yaml.Node) *yaml.Node {
	if n := configV8Node(root, []string{"upstream", "kilo"}); n != nil {
		return n
	}
	return configV8Node(root, []string{"kilo"})
}
func redactKiloConfigNode(root *yaml.Node) bool {
	proxies := configV8Node(kiloConfigNode(root), []string{"proxies"})
	if proxies == nil {
		return false
	}
	changed := false
	for _, p := range proxies.Content {
		changed = deleteConfigV8Path(p, []string{"url"}) || changed
	}
	return changed
}
func redactKiloConfigYAML(raw []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 || !redactKiloConfigNode(doc.Content[0]) {
		return raw, nil
	}
	return yaml.Marshal(&doc)
}

// Omitted URLs in a redacted export preserve only the same stable bootstrap ID.
func preserveKiloConfigSecrets(root, before *yaml.Node) {
	next := configV8Node(kiloConfigNode(root), []string{"proxies"})
	previous := configV8Node(kiloConfigNode(before), []string{"proxies"})
	if next == nil || previous == nil {
		return
	}
	for _, p := range next.Content {
		id := configV8Node(p, []string{"id"})
		if id == nil || configV8Node(p, []string{"url"}) != nil {
			continue
		}
		for _, old := range previous.Content {
			oldID := configV8Node(old, []string{"id"})
			url := configV8Node(old, []string{"url"})
			if oldID != nil && id.Value == oldID.Value && url != nil {
				p.Content = append(p.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "url"}, cloneConfigV8Node(url))
				break
			}
		}
	}
}
func (h *Handler) guardManagedKiloConfig(root, before *yaml.Node) error {
	if _, err := kiloProtectedConfig(root); err != nil {
		return err
	}
	managed := false
	if h.authManager != nil {
		if e, ok := h.authManager.Executor("kilo"); ok {
			if source, ok := e.(interface {
				KiloProxyState() (kilo.ProxyStateView, error)
			}); ok {
				view, err := source.KiloProxyState()
				if err != nil {
					return kilo.ErrStateStorage
				}
				managed = view.Managed
			}
		}
	}
	if !managed && h.cfg != nil {
		_, err := os.Stat(filepath.Join(config.KiloStateDirectory(h.cfg), kilo.StateFileName))
		if err == nil {
			managed = true
		} else if !os.IsNotExist(err) {
			return kilo.ErrStateStorage
		}
	}
	if !managed {
		return nil
	}
	a, err := kiloProtectedConfig(root)
	if err != nil {
		return err
	}
	b, err := kiloProtectedConfig(before)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(a, b) {
		return kilo.ErrStateConflict
	}
	return nil
}

func (h *Handler) prepareLegacyKiloConfig(raw []byte) ([]byte, error) {
	beforeRaw, err := os.ReadFile(h.configFilePath)
	if err != nil {
		return nil, kilo.ErrStateStorage
	}
	var next, before yaml.Node
	if yaml.Unmarshal(raw, &next) != nil || yaml.Unmarshal(beforeRaw, &before) != nil || len(next.Content) == 0 || len(before.Content) == 0 {
		return nil, kilo.ErrStateInvalid
	}
	preserveKiloConfigSecrets(next.Content[0], before.Content[0])
	if err = h.guardManagedKiloConfig(next.Content[0], before.Content[0]); err != nil {
		return nil, err
	}
	return yaml.Marshal(&next)
}

// Decode YAML aliases and merge keys before comparing every protected leaf.
func kiloProtectedConfig(root *yaml.Node) (map[string]any, error) {
	var document map[string]any
	if root != nil {
		if err := root.Decode(&document); err != nil {
			return nil, kilo.ErrStateInvalid
		}
	}
	value, exists := document["kilo"]
	if upstream, ok := document["upstream"].(map[string]any); ok {
		if nested, found := upstream["kilo"]; found {
			value, exists = nested, true
		}
	}
	if !exists {
		return map[string]any{}, nil
	}
	section, ok := value.(map[string]any)
	if !ok {
		return nil, kilo.ErrStateInvalid
	}
	if enabled, present := section["image-reduction-enabled"]; present {
		if _, valid := enabled.(bool); !valid {
			return nil, kilo.ErrStateInvalid
		}
		delete(section, "image-reduction-enabled")
	}
	if section == nil {
		section = map[string]any{}
	}
	return section, nil
}
