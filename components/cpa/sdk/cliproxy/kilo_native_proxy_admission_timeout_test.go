package cliproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	native "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
)

func TestKiloA2ProbeTimeoutPreservesPending(t *testing.T) {
	for _, action := range []string{"preview", "create", "edit"} {
		t.Run(action, func(t *testing.T) {
			f, method, path, body, before, block, entered, finished := kiloA2TimeoutFixture(t, action)
			block.Store(true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			recorder := kiloA2ContextRequest(f, ctx, method, path, body)
			m3bAwait(t, entered)
			m3bAwait(t, finished)
			if ctx.Err() != nil {
				t.Fatalf("internal observation deadline canceled request: %v", ctx.Err())
			}
			wantStatus := http.StatusOK
			if action == "create" {
				wantStatus = http.StatusCreated
			}
			if recorder.Code != wantStatus {
				t.Fatalf("internal observation timeout: status=%d want=%d body=%s", recorder.Code, wantStatus, recorder.Body.String())
			}
			data := recorder.Body.Bytes()
			after := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
			if action == "preview" {
				if ojson.GetBytes(data, "status").String() != "pending" || ojson.GetBytes(data, "reason").String() != "probe_failed" || ojson.GetBytes(data, "observed_exit_ip").Type != ojson.Null || !bytes.Equal(before, after) {
					t.Fatalf("timeout preview changed state or admission: %s", data)
				}
				if _, err := os.Stat(filepath.Join(config.KiloStateDirectory(f.cfg), kilo.StateFileName)); !os.IsNotExist(err) {
					t.Fatalf("timeout preview persisted state: %v", err)
				}
				return
			}
			if ojson.GetBytes(after, "revision").Uint() != ojson.GetBytes(before, "revision").Uint()+1 || ojson.GetBytes(after, "proxies.1.admission_status").String() != "pending" || ojson.GetBytes(after, "proxies.1.admission_reason").String() != "probe_failed" || ojson.GetBytes(after, "proxies.1.healthy").Bool() {
				t.Fatalf("timeout did not save pending exactly once: %s", after)
			}
			state, _, err := (kilo.ProxyStateStore{Dir: config.KiloStateDirectory(f.cfg)}).Load(kilo.ProxyState{})
			wantHistory := 0
			if action == "edit" {
				wantHistory = 1
				if len(state.IPHistory["203.0.113.10"].ProxyIDs) != 1 || state.IPHistory["203.0.113.10"].ProxyIDs[0] != state.Proxies[1].ID {
					t.Fatalf("timeout edit lost prior IP registration: %+v", state.IPHistory)
				}
			}
			if err != nil || len(state.IPHistory) != wantHistory || state.Proxies[1].URL != ojson.Get(body, "url").String() {
				t.Fatalf("timeout persistence mismatch: %+v %v", state, err)
			}
			restarted := native.NewKiloExecutor(f.cfg)
			view, err := restarted.KiloProxyState()
			if err != nil || view.Proxies[1].AdmissionStatus != "pending" || view.Proxies[1].AdmissionReason != "probe_failed" || view.Proxies[1].Healthy {
				t.Fatalf("timeout pending lost after restart: %+v %v", view, err)
			}
		})
	}
}

func TestKiloA2RequestCancellationRemainsCancellation(t *testing.T) {
	for _, action := range []string{"preview", "create", "edit"} {
		for _, cause := range []string{"cancel", "deadline"} {
			t.Run(action+"/"+cause, func(t *testing.T) {
				f, method, path, body, before, block, entered, finished := kiloA2TimeoutFixture(t, action)
				statePath := filepath.Join(config.KiloStateDirectory(f.cfg), kilo.StateFileName)
				disk, diskErr := os.ReadFile(statePath)
				block.Store(true)
				ctx, cancel := context.WithCancel(context.Background())
				wantErr := context.Canceled
				if cause == "deadline" {
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), time.Second)
					wantErr = context.DeadlineExceeded
				}
				defer cancel()
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() { done <- kiloA2ContextRequest(f, ctx, method, path, body) }()
				m3bAwait(t, entered)
				if cause == "cancel" {
					cancel()
				}
				recorder := <-done
				m3bAwait(t, finished)
				if !errors.Is(ctx.Err(), wantErr) || recorder.Code != 499 {
					t.Fatalf("request cancellation lost: err=%v status=%d body=%s", ctx.Err(), recorder.Code, recorder.Body.String())
				}
				after := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
				afterDisk, afterErr := os.ReadFile(statePath)
				if !bytes.Equal(before, after) || !bytes.Equal(disk, afterDisk) || os.IsNotExist(diskErr) != os.IsNotExist(afterErr) {
					t.Fatalf("canceled request published or persisted pending: %s", after)
				}
			})
		}
	}
}

func kiloA2TimeoutFixture(t *testing.T, action string) (*kiloNativeFixture, string, string, string, []byte, *atomic.Bool, <-chan struct{}, <-chan struct{}) {
	t.Helper()
	f := newKiloNativeFixture(t, kiloSuccessStream, true)
	block := &atomic.Bool{}
	entered, finished := make(chan struct{}), make(chan struct{})
	f.probeHandler = func(w http.ResponseWriter, r *http.Request) {
		if block.Load() {
			close(entered)
			<-r.Context().Done()
			close(finished)
			return
		}
		_, _ = io.WriteString(w, "203.0.113.10")
	}
	raw := f.cfg.Kilo.Proxies[0].URL
	method, path := http.MethodPost, kiloProxiesURL
	body := fmt.Sprintf(`{"expected_revision":0,"url":%q}`, raw)
	before := m2Request(t, f, "GET", kiloProxiesURL, "", 200)
	switch action {
	case "preview":
		path += "/preview"
		body = fmt.Sprintf(`{"url":%q}`, raw)
	case "edit":
		before = m2Request(t, f, method, path, body, 201)
		path += "/" + ojson.GetBytes(before, "proxies.1.id").String()
		method = http.MethodPatch
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		u.User = url.UserPassword("synthetic-timeout-user", "synthetic-timeout-password")
		body = fmt.Sprintf(`{"expected_revision":1,"url":%q}`, u.String())
	}
	return f, method, path, body, before, block, entered, finished
}

func kiloA2ContextRequest(f *kiloNativeFixture, ctx context.Context, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	req.RemoteAddr = "127.0.0.1:4242"
	req.Header.Set("Authorization", "Bearer "+kiloManagementKey)
	recorder := httptest.NewRecorder()
	f.server.Config.Handler.ServeHTTP(recorder, req)
	return recorder
}
