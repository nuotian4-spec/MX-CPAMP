package channelscope

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestScopeProviderBoundaryAndWrappedNoReplay(t *testing.T) {
	providers := []string{"kilo", "codex", "bps", "claude"}
	for _, scope := range []string{"", "kilo", "codex", "bps"} {
		want := []string{scope}
		if scope == "" {
			want = []string{"codex", "bps", "claude"}
		}
		if got := Filter(With(context.Background(), scope), providers); !reflect.DeepEqual(got, want) {
			t.Fatalf("scope=%q providers=%v", scope, got)
		}
	}
	cause := errors.New("synthetic")
	err := fmt.Errorf("bootstrap: %w", PreventReplay(cause))
	if !NoReplay(err) || !errors.Is(err, cause) {
		t.Fatal("wrapper lost marker or cause")
	}
	if PreventReplay(nil) != nil || NoReplay(cause) {
		t.Fatal("ordinary error semantics changed")
	}
}
