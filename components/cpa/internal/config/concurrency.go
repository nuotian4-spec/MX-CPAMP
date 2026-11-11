package config

import "fmt"

func validateConcurrencyLimit(limit *int) error {
	if limit != nil && (*limit < 1 || int64(*limit) > 2147483647) {
		return fmt.Errorf("concurrency-limit must be an integer between 1 and 2147483647")
	}
	return nil
}

// ValidateCredentialConcurrency checks every configured upstream credential family.
func (cfg *Config) ValidateCredentialConcurrency() error {
	if cfg == nil {
		return nil
	}
	for i := range cfg.GeminiKey {
		if err := validateConcurrencyLimit(cfg.GeminiKey[i].ConcurrencyLimit); err != nil {
			return fmt.Errorf("GeminiKey[%d]: %w", i, err)
		}
	}
	for i := range cfg.InteractionsKey {
		if err := validateConcurrencyLimit(cfg.InteractionsKey[i].ConcurrencyLimit); err != nil {
			return fmt.Errorf("InteractionsKey[%d]: %w", i, err)
		}
	}
	for i := range cfg.ClaudeKey {
		if err := validateConcurrencyLimit(cfg.ClaudeKey[i].ConcurrencyLimit); err != nil {
			return fmt.Errorf("ClaudeKey[%d]: %w", i, err)
		}
	}
	for i := range cfg.VertexCompatAPIKey {
		if err := validateConcurrencyLimit(cfg.VertexCompatAPIKey[i].ConcurrencyLimit); err != nil {
			return fmt.Errorf("VertexCompatAPIKey[%d]: %w", i, err)
		}
	}
	for i := range cfg.CodexKey {
		if err := validateConcurrencyLimit(cfg.CodexKey[i].ConcurrencyLimit); err != nil {
			return fmt.Errorf("CodexKey[%d]: %w", i, err)
		}
	}
	for i := range cfg.XAIKey {
		if err := validateConcurrencyLimit(cfg.XAIKey[i].ConcurrencyLimit); err != nil {
			return fmt.Errorf("XAIKey[%d]: %w", i, err)
		}
	}
	for i := range cfg.MetaKey {
		if err := validateConcurrencyLimit(cfg.MetaKey[i].ConcurrencyLimit); err != nil {
			return fmt.Errorf("MetaKey[%d]: %w", i, err)
		}
	}
	for i := range cfg.OpenAICompatibility {
		for j := range cfg.OpenAICompatibility[i].APIKeyEntries {
			if err := validateConcurrencyLimit(cfg.OpenAICompatibility[i].APIKeyEntries[j].ConcurrencyLimit); err != nil {
				return fmt.Errorf("openai-compatibility[%d].keys[%d]: %w", i, j, err)
			}
		}
	}
	return nil
}
