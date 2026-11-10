package executor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	auth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	translator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestCodexDuplexConcurrencyWaitingCancellation(t *testing.T) {
	serverDone := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			serverDone <- err
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, _, err = conn.ReadMessage(); err != nil {
			serverDone <- err
			return
		}
		for _, payload := range []string{`{"type":"response.created","response":{"id":"r1","output":[]}}`, `{"type":"response.completed","response":{"id":"r1","output":[]}}`} {
			if err = conn.WriteMessage(websocket.TextMessage, []byte(payload)); err != nil {
				serverDone <- err
				return
			}
		}
		if _, payload, err := conn.ReadMessage(); err == nil {
			serverDone <- fmt.Errorf("unadmitted create reached upstream: %s", payload)
		} else {
			serverDone <- nil
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	waiting := make(chan struct{})
	input := make(chan core.WebsocketInput, 1)
	var active atomic.Int32
	active.Store(1)
	admission := &core.GenerationAdmission{InitialRelease: func() { active.Add(-1) }, Acquire: func(ctx context.Context) (func(), error) { close(waiting); <-ctx.Done(); return nil, ctx.Err() }}
	ctx = core.WithGenerationAdmission(core.WithWebsocketInput(core.WithDownstreamWebsocket(ctx), input), admission)
	exec := NewCodexWebsocketsExecutor(&config.Config{Codex: config.CodexConfig{ResponseSteering: true}})
	exec.store = &codexWebsocketSessionStore{sessions: make(map[string]*codexWebsocketSession)}
	a := &auth.Auth{ID: "capacity-cancel", Provider: "codex", Attributes: map[string]string{"api_key": "fake-key", "base_url": server.URL, "websockets": "true"}}
	result, err := exec.ExecuteStream(ctx, a, core.Request{Model: "test-model", Payload: []byte(`{"model":"test-model","input":[]}`)}, core.Options{SourceFormat: translator.FromString("codex"), Metadata: map[string]any{core.ExecutionSessionMetadataKey: t.Name()}})
	if err != nil {
		t.Fatal(err)
	}
	admission.Transfer()
	completed := false
	for !completed {
		select {
		case chunk := <-result.Chunks:
			if chunk.Err != nil {
				t.Fatal(chunk.Err)
			}
			completed = ojson.GetBytes(chunk.Payload, "type").String() == "response.completed"
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if active.Load() != 0 {
		t.Fatal("idle socket retains first generation")
	}
	input <- core.WebsocketInput{Payload: []byte(`{"type":"response.create","model":"test-model","input":[]}`)}
	select {
	case <-waiting:
	case <-ctx.Done():
		t.Fatal("follow-up did not enter admission")
	}
	cancel()
	select {
	case _, open := <-result.Chunks:
		if open {
			for range result.Chunks {
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("admission blocked cancellation")
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("upstream did not close")
	}
	if active.Load() != 0 {
		t.Fatalf("active=%d", active.Load())
	}
}
func TestCodexDuplexConcurrencyWaitAllowsSteering(t *testing.T) {
	serverDone := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			serverDone <- err
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		readType := func(want string) error {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				return err
			}
			if got := ojson.GetBytes(payload, "type").String(); got != want {
				return fmt.Errorf("got %s want %s", got, want)
			}
			return nil
		}
		write := func(payload string) error { return conn.WriteMessage(websocket.TextMessage, []byte(payload)) }
		if err = readType("response.create"); err != nil {
			serverDone <- err
			return
		}
		if err = write(`{"type":"response.created","response":{"id":"r1","output":[]}}`); err != nil {
			serverDone <- err
			return
		}
		if err = readType("response.steer"); err != nil {
			serverDone <- err
			return
		}
		for _, payload := range []string{`{"type":"response.steer.accepted","steer":{"id":"s1","previous_response_id":"r1"}}`, `{"type":"response.incomplete","response":{"id":"r1","output":[]}}`, `{"type":"response.created","response":{"id":"auto","previous_response_id":"r1","output":[]}}`, `{"type":"response.completed","response":{"id":"auto","output":[]}}`} {
			if err = write(payload); err != nil {
				serverDone <- err
				return
			}
		}
		if err = readType("response.create"); err != nil {
			serverDone <- err
			return
		}
		for _, payload := range []string{`{"type":"response.created","response":{"id":"r2","output":[]}}`, `{"type":"response.completed","response":{"id":"r2","output":[]}}`} {
			if err = write(payload); err != nil {
				serverDone <- err
				return
			}
		}
		_, _, _ = conn.ReadMessage()
		serverDone <- nil
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	input := make(chan core.WebsocketInput, 2)
	permits := make(chan struct{}, 1)
	var active atomic.Int32
	active.Store(1)
	release := func() { active.Add(-1); permits <- struct{}{} }
	admission := &core.GenerationAdmission{InitialRelease: release, Acquire: func(ctx context.Context) (func(), error) {
		select {
		case <-permits:
			if active.Add(1) > 1 {
				t.Error("generation capacity exceeded")
			}
			return release, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	ctx = core.WithGenerationAdmission(core.WithWebsocketInput(core.WithDownstreamWebsocket(ctx), input), admission)
	exec := NewCodexWebsocketsExecutor(&config.Config{Codex: config.CodexConfig{ResponseSteering: true}})
	exec.store = &codexWebsocketSessionStore{sessions: make(map[string]*codexWebsocketSession)}
	a := &auth.Auth{ID: "capacity-steer", Provider: "codex", Attributes: map[string]string{"api_key": "fake-key", "base_url": server.URL, "websockets": "true"}}
	result, err := exec.ExecuteStream(ctx, a, core.Request{Model: "test-model", Payload: []byte(`{"model":"test-model","input":[]}`)}, core.Options{SourceFormat: translator.FromString("codex"), Metadata: map[string]any{core.ExecutionSessionMetadataKey: t.Name()}})
	if err != nil {
		t.Fatal(err)
	}
	admission.Transfer()
	done := false
	for !done {
		select {
		case chunk, ok := <-result.Chunks:
			if !ok {
				t.Fatal("source ended early")
			}
			if chunk.Err != nil {
				t.Fatal(chunk.Err)
			}
			kind := ojson.GetBytes(chunk.Payload, "type").String()
			id := ojson.GetBytes(chunk.Payload, "response.id").String()
			if kind == "response.created" && id == "r1" {
				input <- core.WebsocketInput{Payload: []byte(`{"type":"response.create","model":"test-model","input":[]}`)}
				input <- core.WebsocketInput{Payload: []byte(`{"type":"response.steer","previous_response_id":"r1","input":"continue"}`)}
			}
			done = kind == "response.completed" && id == "r2"
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	cancel()
	for range result.Chunks {
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("upstream not closed")
	}
	if active.Load() != 0 {
		t.Fatalf("active=%d", active.Load())
	}
}
