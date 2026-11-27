package siwc

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

type fixtureConnection struct{}

func (fixtureConnection) BindConnection(context.Context, store.Setup) (string, error) {
	return "fixture-ref", nil
}
func (fixtureConnection) CheckConnection(context.Context, string, store.Setup) error { return nil }

type noNetworkTransport struct{ t *testing.T }

func (n noNetworkTransport) RoundTrip(*http.Request) (*http.Response, error) {
	n.t.Error("过期会话触达网络")
	return nil, nil
}

func TestSIWCExpiredSessionDoesNotCallCPA(t *testing.T) {
	for _, operation := range []string{"status", "callback", "cancel"} {
		t.Run(operation, func(t *testing.T) {
			now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
			s := New(nil, fixtureConnection{}, &http.Client{Transport: noNetworkTransport{t: t}})
			s.now = func() time.Time { return now }
			identity := model.SIWCIdentity{ConnectionRef: "fixture-ref", AuthID: "fixture.json", AuthIndex: "fixture-index"}
			s.sessions["fixture-state"] = &session{identity: identity, expires: now}
			input := model.SIWCSessionInput{SIWCIdentity: identity, State: "fixture-state"}
			var err error
			switch operation {
			case "status":
				_, err = s.Status(context.Background(), input)
			case "cancel":
				_, err = s.Cancel(context.Background(), input)
			case "callback":
				err = s.Callback(context.Background(), model.SIWCCallbackInput{SIWCSessionInput: input,
					RedirectURL: "http://127.0.0.1:1455/auth/callback?state=fixture-state&error=access_denied"})
			}
			status, code, _ := HTTPError(err)
			if status != 410 || code != "siwc_session_expired" {
				t.Fatalf("TTL边界未拒绝: %d %s", status, code)
			}
		})
	}
}
