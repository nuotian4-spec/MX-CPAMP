package pluginhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type quotaPresenceClient struct {
	host       *Host
	raw        string
	calls      int
	callbackID string
	cancel     bool
}

func (c *quotaPresenceClient) Shutdown() {}
func (c *quotaPresenceClient) Call(ctx context.Context, method string, raw []byte) ([]byte, error) {
	c.calls++
	if method != pluginabi.MethodQuotaFetch {
		return nil, fmt.Errorf("unexpected method %s", method)
	}
	var request rpcQuotaFetchRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	c.callbackID = request.HostCallbackID
	if request.AuthID != "synthetic-auth" || request.AuthIndex != "synthetic-index" || request.Provider != "cpa-bps" || !callbackContextExists(c.host, c.callbackID) {
		return nil, errors.New("request identity or callback missing")
	}
	if c.cancel {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return []byte(c.raw), nil
}

func TestRPCQuotaPresenceActualEnvelopeAndManagementJSON(t *testing.T) {
	cases := []struct {
		name, summary string
		want          []pluginapi.QuotaMetric
		bad           bool
	}{
		{name: "missing_value", summary: `[{"key":"reset_cards_available","label":"resets"}]`},
		{name: "null_value", summary: `[{"key":"reset_cards_available","label":"resets","value":null}]`},
		{name: "zero", summary: `[{"key":"reset_cards_available","label":"resets","value":0}]`, want: []pluginapi.QuotaMetric{{Key: "reset_cards_available", Label: "resets", Value: 0}}},
		{name: "three", summary: `[{"key":"reset_cards_available","label":"resets","value":3}]`, want: []pluginapi.QuotaMetric{{Key: "reset_cards_available", Label: "resets", Value: 3}}},
		{name: "invalid_key_missing", summary: `[{"key":123}]`, bad: true},
		{name: "invalid_key_null", summary: `[{"key":123,"value":null}]`, bad: true},
		{name: "invalid_label_missing", summary: `[{"label":123}]`, bad: true},
		{name: "invalid_label_null", summary: `[{"label":123,"value":null}]`, bad: true},
		{name: "invalid_unit_missing", summary: `[{"unit":123}]`, bad: true},
		{name: "invalid_unit_null", summary: `[{"unit":123,"value":null}]`, bad: true},
		{name: "invalid_format_missing", summary: `[{"format":123}]`, bad: true},
		{name: "invalid_format_null", summary: `[{"format":123,"value":null}]`, bad: true},
		{name: "invalid_currency_missing", summary: `[{"currency":123}]`, bad: true},
		{name: "invalid_currency_null", summary: `[{"currency":123,"value":null}]`, bad: true},
		{name: "absent_summary"}, {name: "null_summary", summary: "null"},
		{name: "mixed", summary: `[{"key":"unknown"},{"key":"balance","value":-1.25},{"key":"reset_cards_available","value":null},{"key":"ratio","value":0.5},{"key":"real_zero","value":0}]`, want: []pluginapi.QuotaMetric{{Key: "balance", Value: -1.25}, {Key: "ratio", Value: 0.5}, {Key: "real_zero", Value: 0}}},
		{name: "string", summary: `[{"key":"reset_cards_available","value":"0"}]`, bad: true},
		{name: "boolean", summary: `[{"key":"reset_cards_available","value":false}]`, bad: true},
		{name: "object", summary: `[{"key":"reset_cards_available","value":{}}]`, bad: true},
		{name: "array", summary: `[{"key":"reset_cards_available","value":[]}]`, bad: true},
		{name: "overflow", summary: `[{"key":"reset_cards_available","value":1e999}]`, bad: true},
		{name: "summary_object", summary: `{"value":0}`, bad: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core := `{"subscription":{"plan":"pro","tierName":"Exact plan"},"server_time_offset_ms":17,"groups":[{"displayName":"quota","buckets":[{"window":"daily","remainingFraction":0.5}]}]`
			if tc.summary != "" {
				core += ",\"summary\":" + tc.summary
			}
			core += "}"
			client := &quotaPresenceClient{raw: `{"ok":true,"result":` + core + "}"}
			provider := rpcQuotaProvider{rpcPluginAdapter: &rpcPluginAdapter{id: "cpa-bps", client: client}, identifier: "cpa-bps"}
			host := newHostWithRecords(capabilityRecord{id: "cpa-bps", plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{QuotaProvider: provider}}})
			provider.rpcPluginAdapter.host = host
			client.host = host
			response, handled, err := host.FetchQuotaByPlugin(context.Background(), "cpa-bps", pluginapi.QuotaFetchRequest{AuthID: "synthetic-auth", AuthIndex: "synthetic-index", Provider: "cpa-bps"})
			if !handled || client.calls != 1 || callbackContextExists(host, client.callbackID) {
				t.Fatal("RPC count or callback lifecycle changed")
			}
			if tc.bad {
				if err == nil {
					t.Fatal("illegal value accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if response.Subscription == nil || response.Subscription.Plan != "pro" || response.Subscription.TierName != "Exact plan" || response.ServerTimeOffsetMs != 17 || len(response.Groups) != 1 {
				t.Fatalf("core fields lost: %+v", response)
			}
			if !reflect.DeepEqual(response.Summary, tc.want) {
				t.Fatalf("summary=%+v want=%+v", response.Summary, tc.want)
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.JSON(http.StatusOK, response)
			var public pluginapi.QuotaFetchResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &public); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(public.Summary, tc.want) {
				t.Fatal("management JSON fabricated zero or reordered metrics")
			}
			t.Logf("quota.fetch=1 reset=0 consume_POST=0 metrics=%d", len(public.Summary))
		})
	}
}

func TestRPCQuotaPresenceEnvelopeFailureAndCancellation(t *testing.T) {
	for _, name := range []string{"envelope_error", "canceled", "core_error"} {
		t.Run(name, func(t *testing.T) {
			host := New()
			client := &quotaPresenceClient{host: host, raw: `{"ok":false,"error":{"code":"quota_error","message":"synthetic failure","http_status":403}}`}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "canceled" {
				client.cancel = true
				cancel()
			}
			if name == "core_error" {
				client.raw = `{"ok":true,"result":{"subscription":{"plan":false},"summary":[{"key":"reset_cards_available"}]}}`
			}
			provider := rpcQuotaProvider{rpcPluginAdapter: &rpcPluginAdapter{id: "cpa-bps", host: host, client: client}}
			_, err := provider.FetchQuota(ctx, pluginapi.QuotaFetchRequest{AuthID: "synthetic-auth", AuthIndex: "synthetic-index", Provider: "cpa-bps"})
			if err == nil || client.calls != 1 || callbackContextExists(host, client.callbackID) {
				t.Fatal("error or callback lifecycle lost")
			}
			if name == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if name == "envelope_error" {
				var status interface{ StatusCode() int }
				if !errors.As(err, &status) || status.StatusCode() != 403 {
					t.Fatal(err)
				}
			}
		})
	}
}
