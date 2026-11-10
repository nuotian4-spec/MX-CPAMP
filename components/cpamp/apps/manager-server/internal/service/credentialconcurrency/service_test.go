package credentialconcurrency

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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
			if err := s.Update(context.Background(), Input{AuthID: "target", Limit: limit}); err != nil {
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
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v8/management/credentials/concurrency" {
					mode := "local"
					if kind == "home" {
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
			err := s.Update(context.Background(), Input{AuthID: "target", Limit: json.RawMessage("20")})
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
	if err := s.Update(context.Background(), Input{AuthID: "oauth.json", Limit: json.RawMessage("null")}); err != nil {
		t.Fatal(err)
	}
}
