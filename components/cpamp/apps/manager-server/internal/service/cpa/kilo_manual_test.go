package cpa

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type manualRoundTripper func(*http.Request) (*http.Response, error)

func (f manualRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestKiloManualDoesNotAddInferenceDeadlineOrRetry(t *testing.T) {
	calls := 0
	client := &http.Client{Timeout: time.Millisecond, Transport: manualRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		if _, ok := r.Context().Deadline(); ok {
			t.Error("手动生成被追加总超时")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"model":"kilo-auto/free","status":"incomplete","started_at":"2026-11-13T00:00:00Z","completed_at":"2026-11-13T00:00:01Z","duration_ms":1000,"text":"保留不完整文本","error_code":"incomplete"}`)), Header: make(http.Header)}, nil
	})}
	result, err := TestKiloAccount(context.Background(), client, "http://synthetic.invalid", "SYNTHETIC", "a.json", "kilo-auto/free", "x")
	if err != nil || calls != 1 || result.Status != "incomplete" || result.Text != "保留不完整文本" {
		t.Fatalf("调用或不完整文本错误:%+v %v 次数%d", result, err, calls)
	}
}
