package channelscope

import "context"

type kiloPayloadKey struct{}

// KiloPayloadRoute limits the HTTP payload policy to the supported model POSTs.
func KiloPayloadRoute(method, path string) bool {
	if method != "POST" {
		return false
	}
	return path == "/v1/responses" || path == "/backend-api/codex/responses" || path == "/v1/chat/completions"
}

// WithKiloPayload carries the authenticated HTTP policy through detached contexts.
func WithKiloPayload(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, kiloPayloadKey{}, enabled)
}

func KiloPayload(ctx context.Context) bool {
	if ctx == nil || From(ctx) != "kilo" {
		return false
	}
	enabled, _ := ctx.Value(kiloPayloadKey{}).(bool)
	return enabled
}
