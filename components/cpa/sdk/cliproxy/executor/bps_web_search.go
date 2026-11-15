package executor

import "context"

type bpsWebSearchKey struct{}
type bpsWebSearchBackendKey struct{}

type BPSSearchMode string

const (
	BPSSearchLive    BPSSearchMode = "live"
	BPSSearchCached  BPSSearchMode = "cached"
	BPSSearchIndexed BPSSearchMode = "indexed"
)

type BPSSearchRequest struct {
	Mode                          BPSSearchMode
	Query, CallerScope, SessionID string
	AllowedDomains                []string
	RequireSources                bool
}

type BPSSearchSource struct{ URL, Title string }

type BPSSearchResult struct {
	Text            string
	Sources         []BPSSearchSource
	SourcesProvided bool
}

const (
	BPSSearchBackendUnavailable = "backend_unavailable"
	BPSSearchNoCredential       = "no_credential"
	BPSSearchHTTPRejected       = "http_rejected"
	BPSSearchInvalidResponse    = "invalid_response"
	BPSSearchPolicyRejected     = "policy_rejected"
)

// BPSSearchError reports the nested backend failure without attributing its status to BPS.
type BPSSearchError struct {
	Kind   string
	Status int
}

func (e *BPSSearchError) Error() string { return "BPS web search: " + e.Kind }

type BPSWebSearchBackend struct {
	Preflight func(context.Context, BPSSearchRequest) error
	Execute   func(context.Context, BPSSearchRequest) (BPSSearchResult, error)
}

func WithBPSWebSearchBackend(ctx context.Context, backend BPSWebSearchBackend) context.Context {
	return context.WithValue(ctx, bpsWebSearchBackendKey{}, backend)
}

func BPSWebSearchBackendFromContext(ctx context.Context) (BPSWebSearchBackend, bool) {
	if ctx == nil {
		return BPSWebSearchBackend{}, false
	}
	backend, ok := ctx.Value(bpsWebSearchBackendKey{}).(BPSWebSearchBackend)
	return backend, ok
}

// BPSWebSearchFunc executes an internal Claude search request through the host router.
type BPSWebSearchFunc func(context.Context, []byte) ([]byte, error)

func WithBPSWebSearch(ctx context.Context, execute BPSWebSearchFunc) context.Context {
	return context.WithValue(ctx, bpsWebSearchKey{}, execute)
}

func BPSWebSearch(ctx context.Context) BPSWebSearchFunc {
	execute, _ := ctx.Value(bpsWebSearchKey{}).(BPSWebSearchFunc)
	return execute
}
