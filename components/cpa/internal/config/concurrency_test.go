package config

import (
	"fmt"
	"testing"
)

func TestCredentialConcurrencyConfigValidation(t *testing.T) {
	for _, provider := range []string{"gemini", "interactions", "claude", "codex", "xai", "meta", "vertex", "openai-compatibility"} {
		for _, value := range []string{"1", "20", "2147483647", "0", "-1", "1.5", "2147483648", `"2"`, "null"} {
			t.Run(provider+"/"+value, func(t *testing.T) {
				raw := []byte(fmt.Sprintf("config-version: 8\napi-keys:\n  %s:\n    - name: test\n      base-url: https://example.invalid\n      keys:\n        - api-key: fake-key\n          concurrency-limit: %s\n", provider, value))
				_, err := ParseConfigBytes(raw)
				valid := value == "1" || value == "20" || value == "2147483647" || value == "null"
				if (err == nil) != valid {
					t.Fatalf("valid=%v error=%v", valid, err)
				}
				if valid {
					if err = ValidateV8Config(raw); err != nil {
						t.Fatalf("v8 schema: %v", err)
					}
				}
			})
		}
	}
}
