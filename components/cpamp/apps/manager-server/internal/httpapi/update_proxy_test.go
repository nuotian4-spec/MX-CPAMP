package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/testutil"
)

func TestServerV8UpdateProxyPreservesCPARequestAndResponse(t *testing.T) {
	type observed struct{ method, target, authorization, contentType, body string }
	requests := make(chan observed, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- observed{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(body)}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"fixture-operation","status":"installing"}`))
	}))
	t.Cleanup(upstream.Close)
	handler, _ := newCompatHandler(t, testutil.NewConfig(t), &store.Setup{
		CPAUpstreamURL: upstream.URL, ManagementKey: "saved-cpa-key", Queue: "usage", PopSide: "right",
	})
	initial := httptest.NewRequest(http.MethodGet, "/v8/management/updates/operations/latest?type=plugin", nil)
	initial.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
	initial.Header.Set("X-CPA-Connection-Ref", "required")
	initialRR := httptest.NewRecorder()
	handler.ServeHTTP(initialRR, initial)
	testutil.RequireStatus(t, initialRR, http.StatusAccepted)
	reference := initialRR.Header().Get("X-CPA-Connection-Ref")
	if len(reference) != 32 {
		t.Fatalf("initial connection reference = %q", reference)
	}
	if !strings.Contains(initialRR.Header().Get("Access-Control-Expose-Headers"), "X-CPA-Connection-Ref") {
		t.Fatal("browser cannot read the connection reference")
	}
	<-requests
	for _, tc := range []struct{ method, target, contentType, body string }{
		{http.MethodGet, "/v8/management/updates/operations/latest?type=plugin", "", ""},
		{http.MethodGet, "/v8/management/updates/operations/operation-1", "", ""},
		{http.MethodGet, "/v8/management/updates/core", "", ""},
		{http.MethodPost, "/v8/management/updates/core/previews", "application/json", `{"release_id":"v1.2.3"}`},
		{http.MethodPost, "/v8/management/updates/core/previews/candidate-1/install", "application/json", `{"confirm":true}`},
		{http.MethodPost, "/v8/management/plugins/imports", "application/zip", "PK\x03\x04\x00\xffZIP-fixture"},
		{http.MethodGet, "/v8/management/plugins/imports/import-1", "", ""},
		{http.MethodDelete, "/v8/management/plugins/imports/import-1", "", ""},
		{http.MethodPost, "/v8/management/plugins/imports/import-1/install", "application/json", `{"confirm":true,"confirm_unverified":true}`},
	} {
		t.Run(tc.method+" "+tc.target, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
			req.Header.Set("X-CPA-Connection-Ref", reference)
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			testutil.RequireStatus(t, rr, http.StatusAccepted)
			if rr.Body.String() != `{"id":"fixture-operation","status":"installing"}` {
				t.Fatalf("response body = %q", rr.Body.String())
			}
			got := <-requests
			want := observed{tc.method, tc.target, "Bearer saved-cpa-key", tc.contentType, tc.body}
			if got != want {
				t.Fatalf("CPA request = %#v, want %#v", got, want)
			}
		})
	}
}

func TestServerV8UpdateProxyRejectsOtherScopesAndCallerKeys(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	handler, _ := newCompatHandler(t, testutil.NewConfig(t), &store.Setup{
		CPAUpstreamURL: upstream.URL, ManagementKey: "saved-cpa-key", Queue: "usage", PopSide: "right",
	})
	for _, tc := range []struct {
		method, target, key string
		status              int
	}{
		{http.MethodGet, "/v8/management/updates/core", "", http.StatusUnauthorized},
		{http.MethodGet, "/v8/management/updates/core", "wrong-key", http.StatusUnauthorized},
		{http.MethodPost, "/v8/management/plugins/imports", "saved-cpa-key", http.StatusUnauthorized},
		{http.MethodPost, "/v8/management/plugins/imports", testutil.AdminKey, http.StatusBadRequest},
		{http.MethodGet, "/v8/management/updates/core", testutil.AdminKey, http.StatusBadRequest},
		{http.MethodDelete, "/v8/management/updates/operations/operation-1", testutil.AdminKey, http.StatusMethodNotAllowed},
		{http.MethodPut, "/v8/management/updates/core", testutil.AdminKey, http.StatusMethodNotAllowed},
		{http.MethodGet, "/v8/management/plugins/imports/import-1/install", testutil.AdminKey, http.StatusMethodNotAllowed},
		{http.MethodGet, "/v8/management/config", testutil.AdminKey, http.StatusNotFound},
		{http.MethodPut, "/v8/management/config/plugins/configs/sample/enabled", testutil.AdminKey, http.StatusNotFound},
		{http.MethodDelete, "/v8/management/credentials", testutil.AdminKey, http.StatusNotFound},
		{http.MethodPost, "/v8/management/plugins/store/sample/install", testutil.AdminKey, http.StatusNotFound},
		{http.MethodGet, "/v8/management/updates/core-extra", testutil.AdminKey, http.StatusNotFound},
		{http.MethodGet, "/v8/management/plugins/imports/import-1/extra", testutil.AdminKey, http.StatusNotFound},
		{http.MethodGet, "/v8/management/updates/operations/%2e%2e", testutil.AdminKey, http.StatusNotFound},
	} {
		t.Run(tc.method+" "+tc.target+" "+tc.key, func(t *testing.T) {
			rr := testutil.Request(t, handler, tc.method, tc.target, "", tc.key)
			testutil.RequireStatus(t, rr, tc.status)
			if calls != 0 {
				t.Fatalf("rejected request reached CPA: calls=%d", calls)
			}
		})
	}
}

func TestServerV8UpdateProxyPinsConnectionAcrossConcurrentReadsAndTargetChange(t *testing.T) {
	var originalCalls, replacementCalls atomic.Int32
	makeUpstream := func(calls *atomic.Int32) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"operation":null}`))
		}))
		t.Cleanup(server.Close)
		return server
	}
	original, replacement := makeUpstream(&originalCalls), makeUpstream(&replacementCalls)
	handler, db := newCompatHandler(t, testutil.NewConfig(t), nil)
	saveConnection := func(target string) {
		t.Helper()
		if err := db.SaveManagerConfig(context.Background(), store.ManagerConfig{
			CPAConnection: store.ManagerCPAConnectionConfig{CPABaseURL: target, ManagementKey: "saved-cpa-key"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	saveConnection(original.URL)
	request := func(method, path, reference string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(`{"confirm":true}`))
		req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
		req.Header.Set("X-CPA-Connection-Ref", reference)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}
	var group sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			results <- request(http.MethodGet, "/v8/management/updates/operations/latest?type=plugin", "required")
		}()
	}
	group.Wait()
	close(results)
	reference := ""
	for rr := range results {
		testutil.RequireStatus(t, rr, http.StatusOK)
		got := rr.Header().Get("X-CPA-Connection-Ref")
		if len(got) != 32 || (reference != "" && reference != got) {
			t.Fatalf("concurrent reference = %q, first = %q", got, reference)
		}
		reference = got
	}
	testutil.RequireStatus(t, request(http.MethodPost, "/v8/management/updates/core/previews", reference), http.StatusOK)
	before := originalCalls.Load()
	saveConnection(replacement.URL)
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodPost, "/v8/management/plugins/imports"},
		{http.MethodPost, "/v8/management/updates/core/previews"},
		{http.MethodPost, "/v8/management/updates/core/previews/fixed-candidate/install"},
		{http.MethodPost, "/v8/management/plugins/imports/fixed-import/install"},
		{http.MethodDelete, "/v8/management/plugins/imports/fixed-import"},
		{http.MethodGet, "/v8/management/updates/operations/fixed-operation"},
	} {
		testutil.RequireStatus(t, request(endpoint.method, endpoint.path, reference), http.StatusConflict)
	}
	if originalCalls.Load() != before || replacementCalls.Load() != 0 {
		t.Fatalf("stale update forwarded: original=%d before=%d replacement=%d", originalCalls.Load(), before, replacementCalls.Load())
	}
}
