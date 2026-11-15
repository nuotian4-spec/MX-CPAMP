package channelscope

import (
	"context"
	"errors"
	"strings"
)

type key struct{}

func With(ctx context.Context, channel string) context.Context {
	return context.WithValue(ctx, key{}, channel)
}
func From(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(key{}).(string)
	return value
}
func Allows(ctx context.Context, provider string) bool {
	scope := From(ctx)
	provider = strings.ToLower(strings.TrimSpace(provider))
	if scope == "" {
		return provider != "kilo"
	}
	return provider == scope
}
func Filter(ctx context.Context, providers []string) []string {
	result := make([]string, 0, len(providers))
	for _, provider := range providers {
		if Allows(ctx, provider) {
			result = append(result, provider)
		}
	}
	return result
}
func NoReplay(err error) bool {
	var marker interface{ NoReplay() bool }
	return errors.As(err, &marker) && marker.NoReplay()
}

type noReplayError struct{ cause error }

func (e *noReplayError) Error() string { return e.cause.Error() }
func (e *noReplayError) Unwrap() error { return e.cause }
func (*noReplayError) NoReplay() bool  { return true }

func PreventReplay(err error) error {
	if err == nil || NoReplay(err) {
		return err
	}
	return &noReplayError{cause: err}
}
