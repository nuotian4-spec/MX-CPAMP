package cliproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// This optional bridge runs both production routers inside isolated test processes.
func TestKiloNativeManagerJointBridge(t *testing.T) {
	goBin := os.Getenv("KILO_JOINT_GO_BIN")
	managerDir := os.Getenv("KILO_JOINT_MANAGER_DIR")
	if goBin == "" || managerDir == "" {
		t.Skip("joint manager test runtime not configured")
	}
	started, stopped := make(chan struct{}), make(chan struct{})
	var onceStart, onceStop sync.Once
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"joint-synthetic\",\"status\":\"in_progress\"}}\n\n")
		w.(http.Flusher).Flush()
		onceStart.Do(func() { close(started) })
		<-r.Context().Done()
		onceStop.Do(func() { close(stopped) })
	})
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var barrier <-chan struct{}
		switch r.URL.Path {
		case "/started":
			barrier = started
		case "/released":
			barrier = stopped
		default:
			http.NotFound(w, r)
			return
		}
		select {
		case <-barrier:
		case <-r.Context().Done():
			return
		case <-time.After(10 * time.Second):
			http.Error(w, "test barrier timed out", 504)
			return
		}
		if r.URL.Path == "/released" {
			f.assertReleased(t, 1)
		}
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "ok")
	}))
	defer control.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, goBin, "test", "-count=1", "-v", "-run", "^TestKiloManagerJointRuntime$", "./internal/http/router")
	command.Dir = managerDir
	command.Env = append(os.Environ(), "KILO_JOINT_CPA_URL="+f.server.URL, "KILO_JOINT_CONTROL_URL="+control.URL, "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly")
	output, err := command.CombinedOutput()
	t.Logf("Manager joint router result:\n%s", output)
	if err != nil {
		t.Fatalf("manager joint test failed: %v", err)
	}
	f.assertReleased(t, 1)
	if f.posts.Load() != 1 || f.connects.Load() != 1 {
		t.Fatalf("joint runtime POST=%d CONNECT=%d", f.posts.Load(), f.connects.Load())
	}
}
