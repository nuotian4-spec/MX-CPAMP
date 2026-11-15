package config

import (
	"fmt"
	"strings"
)

func (cfg *SDKConfig) ValidateClientKeyScopes() error {
	if cfg == nil {
		return nil
	}
	keys := make(map[string]bool, len(cfg.APIKeys))
	for _, key := range cfg.APIKeys {
		keys[strings.TrimSpace(key)] = true
	}
	seen := make(map[string]bool, len(cfg.ClientKeyScopes))
	for i, binding := range cfg.ClientKeyScopes {
		key, channel := strings.TrimSpace(binding.APIKey), strings.TrimSpace(binding.Channel)
		if key == "" || !keys[key] {
			return fmt.Errorf("client-key-scopes[%d]: api-key must reference an access key", i)
		}
		if seen[key] {
			return fmt.Errorf("client-key-scopes[%d]: duplicate api-key binding", i)
		}
		if channel != "kilo" && channel != "codex" && channel != "bps" {
			return fmt.Errorf("client-key-scopes[%d]: unsupported channel", i)
		}
		seen[key] = true
	}
	return nil
}

// ValidateClientKeyScopeMutation preserves legacy keys while requiring scopes for new keys.
func (cfg *SDKConfig) ValidateClientKeyScopeMutation(previous *SDKConfig) error {
	if err := cfg.ValidateClientKeyScopes(); err != nil {
		return err
	}
	oldKeys := map[string]bool{}
	oldScopes := map[string]bool{}
	if previous != nil {
		for _, key := range previous.APIKeys {
			oldKeys[strings.TrimSpace(key)] = true
		}
		for _, binding := range previous.ClientKeyScopes {
			oldScopes[strings.TrimSpace(binding.APIKey)] = true
		}
	}
	scopes := map[string]bool{}
	for _, binding := range cfg.ClientKeyScopes {
		scopes[strings.TrimSpace(binding.APIKey)] = true
	}
	for _, key := range cfg.APIKeys {
		key = strings.TrimSpace(key)
		if !scopes[key] && (!oldKeys[key] || oldScopes[key]) {
			return fmt.Errorf("client_key_channel_required")
		}
	}
	return nil
}
