package credentialconcurrency

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type resolver struct{ value store.Setup }

func (r *resolver) ResolveSetup(context.Context) (store.Setup, bool, error) {
	return r.value, true, nil
}
func reply(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func TestSnapshotWhitelistsSecretsAndHome(t *testing.T) {
	for _, mode := range []string{"local", "home"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reply(w, map[string]any{"mode": mode, "default_limit": 20, "pool_waiting": 3, "token": "TOKEN_CANARY", "credentials": []any{map[string]any{"auth_id": "id", "access_token": "ACCESS_CANARY", "api_key": "KEY_CANARY"}}})
			}))
			defer server.Close()
			s := New(&resolver{store.Setup{CPAUpstreamURL: server.URL, ManagementKey: "s"}}, nil)
			got, err := s.List(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(got)
			if strings.Contains(string(raw), "CANARY") {
				t.Fatal("快照泄露秘密")
			}
			if mode == "home" && string(raw) != `{"mode":"home","credentials":[]}` {
				t.Fatal(string(raw))
			}
		})
	}
}
func TestConfigKeyEditUsesExactIndexAndETag(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "override", true: "default"}[remove], func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("缺少认证")
				}
				switch r.URL.Path {
				case "/v8/management/credentials/concurrency":
					reply(w, map[string]any{"mode": "local", "default_limit": 20, "pool_waiting": 0, "credentials": []any{map[string]any{"auth_id": "target", "auth_index": "IDX2", "source": "config", "editable": true}}})
				case "/v8/management/config/api-keys":
					if r.Method == "GET" {
						w.Header().Set("ETag", `"revision"`)
						reply(w, map[string]any{"codex": []any{map[string]any{"name": "group", "keys": []any{map[string]any{"api-key": "ONE_CANARY", "auth_index": "IDX1", "concurrency-limit": 7}, map[string]any{"api-key": "TWO_CANARY", "auth_index": "IDX2", "concurrency-limit": 2, "weight": 4}}}}})
						return
					}
					if r.Method != "PUT" || r.Header.Get("If-Match") != `"revision"` {
						t.Error("未带ETag条件写")
					}
					writes++
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					keys := body["codex"].([]any)[0].(map[string]any)["keys"].([]any)
					first := keys[0].(map[string]any)
					second := keys[1].(map[string]any)
					if !reflect.DeepEqual(first, map[string]any{"api-key": "ONE_CANARY", "auth_index": "IDX1", "concurrency-limit": float64(7)}) {
						t.Error("修改其他凭证")
					}
					if second["api-key"] != "TWO_CANARY" || second["weight"] != float64(4) {
						t.Error("修改其他字段")
					}
					if remove {
						if _, exists := second["concurrency-limit"]; exists {
							t.Error("未恢复默认")
						}
					} else if second["concurrency-limit"] != float64(31) {
						t.Error("未设置目标上限")
					}
					reply(w, map[string]string{"status": "ok"})
				default:
					t.Error("非窄接口", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			s := New(&resolver{store.Setup{CPAUpstreamURL: server.URL, ManagementKey: "secret"}}, nil)
			limit := json.RawMessage("31")
			if remove {
				limit = json.RawMessage("null")
			}
			if err := s.Update(context.Background(), Input{AuthID: "target", ConnectionRef: listReference(t, s), Limit: limit}); err != nil {
				t.Fatal(err)
			}
			if writes != 1 {
				t.Fatal("写次数", writes)
			}
		})
	}
}
func TestRejectsAmbiguousChangedAndReadOnly(t *testing.T) {
	for _, kind := range []string{"home", "readonly", "duplicate", "missing", "etag", "changed", "target"} {
		t.Run(kind, func(t *testing.T) {
			var resolve resolver
			var homeEnabled atomic.Bool
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v8/management/credentials/concurrency" {
					mode := "local"
					if kind == "home" && homeEnabled.Load() {
						mode = "home"
					}
					reply(w, map[string]any{"mode": mode, "default_limit": 20, "pool_waiting": 0, "credentials": []any{map[string]any{"auth_id": "target", "auth_index": "IDX", "source": "config", "editable": kind != "readonly"}}})
					return
				}
				if r.Method == "GET" {
					if kind != "etag" {
						w.Header().Set("ETag", `"old"`)
					}
					keys := []any{map[string]any{"api-key": "SECRET", "auth_index": "IDX"}}
					if kind == "duplicate" {
						keys = append(keys, map[string]any{"auth_index": "IDX"})
					}
					if kind == "missing" {
						keys = []any{}
					}
					if kind == "target" {
						resolve.value.CPAUpstreamURL = "http://other.invalid"
					}
					reply(w, map[string]any{"codex": []any{map[string]any{"keys": keys}}})
					return
				}
				writes++
				w.WriteHeader(412)
			}))
			defer server.Close()
			resolve.value = store.Setup{CPAUpstreamURL: server.URL, ManagementKey: "s"}
			s := New(&resolve, nil)
			reference := listReference(t, s)
			homeEnabled.Store(true)
			err := s.Update(context.Background(), Input{AuthID: "target", ConnectionRef: reference, Limit: json.RawMessage("20")})
			status, _, _ := HTTPError(err)
			if err == nil || status != 409 {
				t.Fatal(err)
			}
			want := 0
			if kind == "changed" {
				want = 1
			}
			if writes != want {
				t.Fatal("意外写入次数", writes)
			}
		})
	}
}
func TestFileOverrideAndLimitValidation(t *testing.T) {
	for _, raw := range []string{"", "0", "-1", "1.2", "2147483648", `"20"`} {
		if _, err := ParseLimit(json.RawMessage(raw)); err == nil {
			t.Fatal("接受非法值", raw)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			reply(w, map[string]any{"mode": "local", "default_limit": 20, "pool_waiting": 0, "credentials": []any{map[string]any{"auth_id": "oauth.json", "source": "file", "editable": true}}})
			return
		}
		if r.Method != "PATCH" || r.URL.Path != "/v8/management/credentials/fields" {
			t.Error("接口错误")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !reflect.DeepEqual(body, map[string]any{"name": "oauth.json", "concurrency_limit": nil}) {
			t.Error(body)
		}
		reply(w, map[string]string{"status": "ok"})
	}))
	defer server.Close()
	s := New(&resolver{store.Setup{CPAUpstreamURL: server.URL, ManagementKey: "s"}}, nil)
	if err := s.Update(context.Background(), Input{AuthID: "oauth.json", ConnectionRef: listReference(t, s), Limit: json.RawMessage("null")}); err != nil {
		t.Fatal(err)
	}
}

func TestStaleDisplayedConnectionCannotWriteOtherCPA(t *testing.T) {
	var writesA, writesB atomic.Int32
	server := func(writes *atomic.Int32) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				reply(w, map[string]any{"mode": "local", "default_limit": 20, "pool_waiting": 0, "credentials": []any{map[string]any{"auth_id": "shared.json", "source": "file", "editable": true}}})
				return
			}
			writes.Add(1)
			reply(w, map[string]string{"status": "ok"})
		}))
	}
	a, b := server(&writesA), server(&writesB)
	defer a.Close()
	defer b.Close()
	resolve := &resolver{store.Setup{CPAUpstreamURL: a.URL, ManagementKey: "KEY_A"}}
	s := New(resolve, nil)
	shown, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resolve.value = store.Setup{CPAUpstreamURL: b.URL, ManagementKey: "KEY_B"}
	err = s.Update(context.Background(), Input{AuthID: "shared.json", ConnectionRef: shown.ConnectionRef, Limit: json.RawMessage("9")})
	status, _, _ := HTTPError(err)
	if err == nil || status != 409 || writesA.Load() != 0 || writesB.Load() != 0 {
		t.Fatalf("旧快照写入新CPA: status=%d writesA=%d writesB=%d", status, writesA.Load(), writesB.Load())
	}
}

func listReference(t *testing.T, s *Service) string {
	t.Helper()
	snapshot, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.ConnectionRef) != 32 {
		t.Fatal("连接引用未生成")
	}
	return snapshot.ConnectionRef
}
func TestConnectionReferenceRequiredStableAndInvalidated(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		reply(w, map[string]any{"mode": "local", "default_limit": 20, "pool_waiting": 0, "credentials": []any{map[string]any{"auth_id": "same", "source": "file", "editable": true}}})
	}))
	defer server.Close()
	resolve := &resolver{store.Setup{CPAUpstreamURL: server.URL, ManagementKey: "MANAGEMENT_SECRET"}}
	s := New(resolve, nil)
	reference := listReference(t, s)
	if listReference(t, s) != reference {
		t.Fatal("同连接引用不稳定")
	}
	for _, value := range []string{"", "bad", strings.Repeat("z", 32), strings.Repeat("0", 32)} {
		before := calls.Load()
		err := s.Update(context.Background(), Input{AuthID: "same", ConnectionRef: value, Limit: json.RawMessage("20")})
		status, _, _ := HTTPError(err)
		want := 400
		if value == strings.Repeat("0", 32) {
			want = 409
		}
		if err == nil || status != want || calls.Load() != before {
			t.Fatalf("无效引用触发上游请求: status=%d calls=%d", status, calls.Load()-before)
		}
	}
	restarted := New(resolve, nil)
	before := calls.Load()
	err := restarted.Update(context.Background(), Input{AuthID: "same", ConnectionRef: reference, Limit: json.RawMessage("20")})
	status, _, _ := HTTPError(err)
	if err == nil || status != 409 || calls.Load() != before {
		t.Fatal("重启后接受旧引用")
	}
	resolve.value.ManagementKey = "NEW_SECRET"
	err = s.Update(context.Background(), Input{AuthID: "same", ConnectionRef: reference, Limit: json.RawMessage("20")})
	status, _, _ = HTTPError(err)
	if err == nil || status != 409 || calls.Load() != before {
		t.Fatal("换管理密钥后接受旧引用")
	}
}
