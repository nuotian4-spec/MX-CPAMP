package kilo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestKiloDeviceVerificationURL(t *testing.T) {
	for _, test := range []struct {
		raw   string
		valid bool
	}{
		{"https://kilo.ai/device", true}, {"https://app.kilo.ai/device", true}, {"https://api.kilo.ai:443/device", true},
		{"http://kilo.ai/device", false}, {"https://kilo.ai:444/device", false}, {"https://u@kilo.ai/device", false}, {"https://evil-kilo.ai/device", false}, {"https://kilo.ai.evil.invalid/device", false}, {"javascript:alert(1)", false},
	} {
		t.Run(test.raw, func(t *testing.T) {
			if ValidVerificationURL(test.raw) != test.valid {
				t.Fatalf("url validity=%v", !test.valid)
			}
		})
	}
}

// Protocol validation is isolated from the native Transport integration tests.
type deviceProtocolTransport struct{ response *http.Response }

func (t deviceProtocolTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return t.response, nil
}
func TestKiloDeviceRejectsInvalidStartAndToken(t *testing.T) {
	for _, body := range []string{
		`null`, `[]`, `not-json`, `{"code":"secret"}`, `{"code":"secret","user_code":"local","verificationUrl":"http://kilo.ai/device","expiresIn":30}`,
		`{"code":"secret","user_code":"local","verificationUrl":"https://kilo.ai/device","expiresIn":0}`,
		`{"code":"secret","user_code":"local","verificationUrl":"https://kilo.ai/device","expiresIn":86401}`,
		`{"code":"secret","user_code":"local","verificationUrl":"https://kilo.ai/device","expiresIn":1.5}`,
		strings.Repeat("x", 65537),
	} {
		w := httptest.NewRecorder()
		_, _ = io.WriteString(w, body)
		client := &http.Client{Transport: deviceProtocolTransport{w.Result()}}
		if _, err := StartDevice(context.Background(), client); err == nil {
			t.Fatal("invalid start accepted")
		}
	}
	for _, token := range []string{"", "a b", "a\tb", "a\nb", "é", "a\x7fb"} {
		if validDeviceToken(token) {
			t.Fatal("invalid token accepted")
		}
	}
	if !validDeviceToken("valid-ASCII.012_~") {
		t.Fatal("valid token rejected")
	}
}

func TestKiloDeviceBudgetAndCallerCancellation(t *testing.T) {
	for _, caller := range []bool{false, true} {
		t.Run(map[bool]string{false: "internal-budget", true: "caller-cancel"}[caller], func(t *testing.T) {
			entered, closed := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				close(entered)
				<-r.Context().Done()
				close(closed)
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			budget := 100 * time.Millisecond
			if caller {
				budget = time.Minute
			}
			done := make(chan error, 1)
			go func() { _, _, err := deviceHTTPBudget(ctx, server.Client(), "POST", server.URL, budget); done <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("HTTP not entered")
			}
			if caller {
				cancel()
			}
			var err error
			select {
			case err = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("HTTP not canceled")
			}
			if caller {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("caller error=%v", err)
				}
			} else {
				var upstream *DeviceError
				if !errors.As(err, &upstream) || upstream.Status != 503 || ctx.Err() != nil {
					t.Fatalf("budget error=%v", err)
				}
			}
			select {
			case <-closed:
			case <-time.After(5 * time.Second):
				t.Fatal("socket retained after deadline")
			}
		})
	}
}
