package management

import (
	"context"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// Kilo secrets live only in this typed session, never in generic OAuth metadata.
type kiloDeviceSession struct {
	mu                                         sync.Mutex
	owner                                      *Handler
	ctx                                        context.Context
	cancel                                     context.CancelFunc
	state, accountID, mode, code, token, email string
	settings                                   coreauth.KiloAccountChanges
	authorization                              kilo.AuthorizationEgress
	inference                                  kilo.InferenceEgress
	releasePin                                 func()
	expiryTimer                                *time.Timer
	expires, nextPoll                          time.Time
	polling, committing                        bool
}

func (s *oauthSessionStore) kiloNow() time.Time {
	if s.kiloClock != nil {
		return s.kiloClock()
	}
	return time.Now()
}
func (f *kiloDeviceSession) terminalLocked(state string, now time.Time) {
	f.state, f.code, f.token = state, "", ""
	f.cancel()
	if f.expiryTimer != nil {
		f.expiryTimer.Stop()
		f.expiryTimer = nil
	}
	if f.releasePin != nil {
		f.releasePin()
		f.releasePin = nil
	}
	f.expires = now.Add(time.Minute)
	if state == "saved" {
		f.expires = now.Add(oauthSessionTTL)
	}
}
func (f *kiloDeviceSession) expireLocked(now time.Time) {
	if !f.committing && (f.state == "pending" || f.state == "approved") && f.ctx.Err() != nil {
		f.terminalLocked("cancelled", now)
		return
	}
	if !f.committing && (f.state == "pending" || f.state == "approved") && !now.Before(f.expires) {
		f.terminalLocked("expired", now)
	}
}
func (f *kiloDeviceSession) viewLocked(now time.Time) gin.H {
	view := gin.H{"status": f.state, "authorization_egress": f.authorization, "inference_egress": f.inference}
	if f.state == "approved" {
		view["email"] = f.email
		view["save_expires_in"] = max(0, int(f.expires.Sub(now)/time.Second))
	} else if f.state == "saved" {
		view["account_id"] = f.accountID
	}
	return view
}

// Purging never holds the generic store lock while cancelling an HTTP request.
func (s *oauthSessionStore) purgeKilo() {
	now := s.kiloNow()
	s.mu.RLock()
	flows := map[string]*kiloDeviceSession{}
	for id, session := range s.sessions {
		if session.Kilo != nil {
			flows[id] = session.Kilo
		}
	}
	s.mu.RUnlock()
	for id, flow := range flows {
		flow.mu.Lock()
		flow.expireLocked(now)
		remove := !flow.committing && !now.Before(flow.expires)
		flow.mu.Unlock()
		if remove {
			s.mu.Lock()
			if s.sessions[id].Kilo == flow {
				delete(s.sessions, id)
			}
			s.mu.Unlock()
		}
	}
}
func (s *oauthSessionStore) kiloSession(id string, owner *Handler) *kiloDeviceSession {
	s.purgeKilo()
	s.mu.RLock()
	flow := s.sessions[id].Kilo
	s.mu.RUnlock()
	if flow == nil || flow.owner != owner {
		return nil
	}
	return flow
}

// The single timer follows the existing authorization/save deadline. Polling does
// not extend it, and no model or proxy slot is retained while it waits.
func (f *kiloDeviceSession) armExpiryLocked(now time.Time, clock func() time.Time) {
	if f.expiryTimer != nil {
		f.expiryTimer.Stop()
	}
	f.expiryTimer = time.AfterFunc(max(time.Nanosecond, f.expires.Sub(now)), func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.expireLocked(clock())
	})
}
