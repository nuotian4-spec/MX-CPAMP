package cliproxy

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

const kiloA1ParentPair = `[{"type":"function_call","name":"chain_tool","call_id":"call-resp-chain-1","arguments":"{}"},{"type":"function_call_output","call_id":"call-resp-chain-1","output":"aborted"}]`

func TestKiloA1NativeCallerAndMetadata(t *testing.T) {
	seen := make(chan kiloSessionSeen, 1)
	f := newKiloNativeFixture(t, kiloSessionUpstream(t, seen))
	code, body := kiloA1Request(t, f, "synthetic-kilo-key", "root", "", "new", `{"input":"seed owner"}`)
	if code != 200 {
		t.Fatalf("seed=%d %s", code, body)
	}
	f.assertReleased(t, 1)
	f.cfg.APIKeys = append(f.cfg.APIKeys, "synthetic-other-kilo")
	f.cfg.ClientKeyScopes = append(f.cfg.ClientKeyScopes, config.ClientKeyScope{APIKey: "synthetic-other-kilo", Channel: "kilo"})
	restartKiloSessionFixture(t, f)
	code, body = kiloA1Request(t, f, "synthetic-other-kilo", "child", "root", "warm_fork", `{"input":`+kiloA1ParentPair+`}`)
	if code != 409 {
		t.Fatalf("cross caller fork=%d %s", code, body)
	}
	req, _ := http.NewRequest("POST", f.server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-6.1-sol","input":"metadata conflict","client_metadata":{"thread_id":"body-thread","session_id":"root"}}`))
	req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Thread-Id", "header-thread")
	req.Header.Set("Session-Id", "root")
	response, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ = io.ReadAll(response.Body)
	if response.StatusCode != 409 || f.posts.Load() != 1 {
		t.Fatalf("metadata conflict=%d POST=%d body=%s", response.StatusCode, f.posts.Load(), body)
	}
	f.assertReleased(t, 0)
	t.Log("formal Router/AuthManager: cross-Caller warm_fork and header/body thread conflict rejected409 before admission; no additional POST/results, resources=0")
}

func TestKiloA1NativeCancelSiblingIsolation(t *testing.T) {
	seen := make(chan kiloSessionSeen, 3)
	root := kiloSessionUpstream(t, seen)
	started, canceled, finish := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-finish:
		default:
			close(finish)
		}
	}()
	f := newKiloNativeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		thread := ojson.GetBytes(raw, "client_metadata.thread_id").String()
		if thread == "root" {
			root(w, r)
			return
		}
		seen <- kiloSessionSeen{r.Header.Get("X-KILOCODE-MACHINEID"), r.Header.Get("X-Session-Id")}
		w.Header().Set("Content-Type", "text/event-stream")
		if thread == "cancel-child" {
			kiloA1Event(w, `{"type":"response.created","response":{"id":"resp-a1-cancel-child","status":"in_progress"}}`)
			<-r.Context().Done()
			close(canceled)
			return
		}
		close(started)
		select {
		case <-finish:
			kiloA1Terminal(w, "resp-a1-sibling", nil)
		case <-r.Context().Done():
			t.Error("canceling child terminated the sibling upstream context")
		}
	})
	code, body := kiloA1Request(t, f, "synthetic-kilo-key", "root", "", "new", `{"input":"seed owner"}`)
	if code != 200 {
		t.Fatalf("seed=%d %s", code, body)
	}
	parent := <-seen
	f.assertReleased(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	doc := ojson.NewDocString(`{"model":"gpt-6.1-sol","stream":true,"input":` + kiloA1ParentPair + `}`)
	_ = doc.Set("client_metadata", map[string]any{"thread_id": "cancel-child", "session_id": "root", "parent_thread_id": "root", "history_initialization": "warm_fork"})
	req, _ := http.NewRequestWithContext(ctx, "POST", f.server.URL+"/v1/responses", strings.NewReader(doc.String()))
	req.Header.Set("Authorization", "Bearer synthetic-kilo-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Thread-Id", "cancel-child")
	req.Header.Set("Session-Id", "root")
	response, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(line, "resp-a1-cancel-child") {
			break
		}
	}
	child := <-seen
	result := make(chan int, 1)
	go func() {
		code, _ := kiloA1Request(t, f, "synthetic-kilo-key", "sibling", "root", "warm_fork", `{"input":`+kiloA1ParentPair+`}`)
		result <- code
	}()
	m3bAwait(t, started)
	sibling := <-seen
	if child.Account != parent.Account || sibling.Account != parent.Account || child.Session == parent.Session || sibling.Session == parent.Session || child.Session == sibling.Session {
		t.Fatalf("fork identity/session mismatch parent=%+v child=%+v sibling=%+v", parent, child, sibling)
	}
	cancel()
	_ = response.Body.Close()
	m3bAwait(t, canceled)
	m3Wait(t, func() bool {
		return f.executor.KiloProxySnapshot().Proxies[0].Active == 1 && len(f.hook.snapshot()) == 2
	})
	select {
	case status := <-result:
		t.Fatalf("sibling completed before release: %d", status)
	default:
	}
	close(finish)
	select {
	case status := <-result:
		if status != 200 {
			t.Fatalf("sibling=%d", status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("sibling did not finish")
	}
	f.assertReleased(t, 3)
	successes := 0
	for _, result := range f.hook.snapshot() {
		if result.Success {
			successes++
		}
	}
	if f.posts.Load() != 3 || successes != 2 {
		t.Fatalf("cancellation replay/result mismatch POST=%d successes=%d", f.posts.Load(), successes)
	}
	t.Log("same-Caller full children: fixed identity, three independent upstream sessions; cancel closed only its stream, live sibling retained one lease and finished200; POST3 results3 resources0")
}
