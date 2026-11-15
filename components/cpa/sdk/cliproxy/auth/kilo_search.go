package auth

import (
	"context"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilobinding"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)

type kiloSearchContextKey struct{}
type kiloSearchContext struct{ branch, source string }

// WithKiloSearchContext is called only after the Alpha batch and channel checks.
// The branch is generated internally, never accepted from request metadata.
func WithKiloSearchContext(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, kiloSearchContextKey{}, kiloSearchContext{branch: "kilo-alpha:" + source + ":" + uuid.NewString(), source: source})
}

func kiloSearchBindingHints(ctx context.Context, h kilobinding.Hints, raw []byte) (kilobinding.Hints, error) {
	search, ok := ctx.Value(kiloSearchContextKey{}).(kiloSearchContext)
	if !ok {
		return h, nil
	}
	if h.Previous != "" || len(h.Calls) != 0 || !kiloSearchPlainInput(ojson.GetBytes(raw, "input")) {
		return h, NewRequestScopedError("Kilo Alpha requires plain text context; referenced or encrypted history has no independent-search mapping", 400)
	}
	h.Session, h.Thread, h.Parent, h.Initialization = search.branch, search.branch, search.source, "new"
	h.History = false
	return h, nil
}

func kiloSearchPlainInput(input ojson.Result) bool {
	if input.Type == ojson.String {
		return true
	}
	if !input.IsArray() {
		return false
	}
	for _, item := range input.Array() {
		if !item.IsObject() || (item.Get("type").String() != "" && item.Get("type").String() != "message") || item.Get("encrypted_content").Exists() || item.Get("encrypted_output").Exists() {
			return false
		}
		switch item.Get("role").String() {
		case "user", "assistant", "developer", "system":
		default:
			return false
		}
		content := item.Get("content")
		if content.Type == ojson.String {
			continue
		}
		if !content.IsArray() {
			return false
		}
		for _, part := range content.Array() {
			if (part.Get("type").String() != "input_text" && part.Get("type").String() != "output_text") || part.Get("text").Type != ojson.String {
				return false
			}
			for key := range part.Map() {
				if key != "type" && key != "text" && !(key == "annotations" && part.Get(key).IsArray() && len(part.Get(key).Array()) == 0) {
					return false
				}
			}
		}
	}
	return true
}

// KiloAlphaSearchSupport is an optional built-in executor capability.
type KiloAlphaSearchSupport interface{ SupportsKiloAlphaSearch(*Auth, string) bool }

// SupportsKiloSearchModel checks the actual scoped serving credentials and executor.
func (m *Manager) SupportsKiloSearchModel(ctx context.Context, model string, discovery bool) bool {
	if m == nil || channelscope.From(ctx) != "kilo" || !m.ProviderChannelAllowed(ctx, "kilo") {
		return false
	}
	model = strings.TrimSpace(model)
	executor, ok := m.Executor("kilo")
	if !ok {
		return false
	}
	serving := false
	for _, a := range m.List() {
		if a == nil || a.Provider != "kilo" || a.Disabled || a.Status == StatusDisabled || !registry.GetGlobalRegistry().ClientSupportsModel(a.ID, model) {
			continue
		}
		serving = true
		upstream := m.ResolveExecutionModel(a, model)
		if discovery {
			capability, ok := executor.(NativeToolSearchSupport)
			if !ok || !capability.SupportsNativeToolSearch(a, upstream) {
				return false
			}
		} else {
			capability, ok := executor.(KiloAlphaSearchSupport)
			if !ok || !capability.SupportsKiloAlphaSearch(a, upstream) {
				return false
			}
		}
	}
	return serving
}
