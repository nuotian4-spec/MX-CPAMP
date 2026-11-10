package management

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/bps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/codex"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func readImportMetadata(c *gin.Context, provider string) (map[string]any, error) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 2<<20))
	if err != nil || !ojson.ValidBytes(raw) || !ojson.ParseBytes(raw).IsObject() {
		return nil, errors.New("invalid_credentials")
	}
	doc := ojson.NewDoc(raw)
	meta := make(map[string]any)
	for _, key := range []string{"profile", "client_id", "access_token", "refresh_token", "id_token", "account_id", "email", "expired", "login_method"} {
		value := doc.Get(key)
		if value.Exists() && value.Type != ojson.String {
			return nil, errors.New("invalid_credentials")
		}
		meta[key] = strings.TrimSpace(value.String())
	}
	if err = validateImportMetadata(meta, provider); err != nil {
		return nil, err
	}
	return meta, nil
}

func validateImportMetadata(meta map[string]any, provider string) error {
	client, profile := codex.ClientID, "codex"
	if provider == "bps" {
		client, profile = bps.ClientID, "excel"
	}
	if metadataStringValue(meta, "profile") != profile || metadataStringValue(meta, "client_id") != client {
		return errors.New("profile_client_mismatch")
	}
	for _, key := range []string{"access_token", "refresh_token", "account_id", "email"} {
		value := metadataStringValue(meta, key)
		if value == "" || strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("invalid_credentials")
		}
	}
	email := metadataStringValue(meta, "email")
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || strings.ContainsAny(email, "/\\<>:\"|?*") {
		return errors.New("invalid_identity")
	}
	if method := metadataStringValue(meta, "login_method"); method != "oauth" && method != "2fa" {
		return errors.New("invalid_login_method")
	}
	expiry, err := time.Parse(time.RFC3339, metadataStringValue(meta, "expired"))
	if err != nil || !expiry.After(time.Now()) {
		return errors.New("invalid_expiry")
	}
	meta["expired"] = expiry.UTC().Format(time.RFC3339)
	for _, key := range []string{"access_token", "id_token"} {
		token := metadataStringValue(meta, key)
		if token == "" {
			continue
		}
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			if key == "id_token" {
				return errors.New("invalid_identity_token")
			}
			continue
		}
		raw, errDecode := base64.RawURLEncoding.DecodeString(parts[1])
		if errDecode != nil || !ojson.ValidBytes(raw) || !ojson.ParseBytes(raw).IsObject() {
			return errors.New("invalid_token_claims")
		}
		claims := ojson.NewDoc(raw)
		claimClient := claims.Get("client_id").String()
		if claimClient != "" && claimClient != client {
			return errors.New("token_client_mismatch")
		}
		if key == "id_token" {
			aud := claims.Get("aud")
			match := aud.String() == client
			if aud.IsArray() {
				for _, value := range aud.Array() {
					if value.String() == client {
						match = true
					}
				}
			}
			if !match {
				return errors.New("token_client_mismatch")
			}
		}
		account := claims.Get("https://api\\.openai\\.com/auth.chatgpt_account_id").String()
		claimEmail := claims.Get("email").String()
		if claimEmail == "" {
			claimEmail = claims.Get("https://api\\.openai\\.com/profile.email").String()
		}
		if (account != "" && account != metadataStringValue(meta, "account_id")) || (claimEmail != "" && !strings.EqualFold(claimEmail, email)) {
			return errors.New("token_identity_mismatch")
		}
		if exp := claims.Get("exp").Int(); key == "access_token" && exp > 0 && expiry.Unix() != exp {
			return errors.New("token_expiry_mismatch")
		}
		if sub := claims.Get("sub").String(); sub != "" {
			if previous := metadataStringValue(meta, "sub"); previous != "" && previous != sub {
				return errors.New("token_identity_mismatch")
			}
			meta["sub"] = sub
		}
		if plan := claims.Get("https://api\\.openai\\.com/auth.chatgpt_plan_type").String(); plan != "" {
			meta["plan_type"] = plan
		}
	}
	meta["type"] = provider
	meta["last_refresh"] = time.Now().UTC().Format(time.RFC3339)
	if provider == "bps" {
		delete(meta, "id_token")
	}
	return nil
}

func sameImportedIdentity(existing *coreauth.Auth, provider string, meta map[string]any) bool {
	if existing == nil || existing.Provider != provider || metadataStringValue(existing.Metadata, "account_id") != metadataStringValue(meta, "account_id") || !strings.EqualFold(metadataStringValue(existing.Metadata, "email"), metadataStringValue(meta, "email")) {
		return false
	}
	oldSub, newSub := metadataStringValue(existing.Metadata, "sub"), metadataStringValue(meta, "sub")
	return oldSub == "" || newSub == "" || oldSub == newSub
}

func importedRecord(provider string, meta map[string]any) *coreauth.Auth {
	identity := metadataStringValue(meta, "account_id")
	if sub := metadataStringValue(meta, "sub"); sub != "" {
		identity += "\x00" + sub
	}
	digest := sha256.Sum256([]byte(identity))
	hash := hex.EncodeToString(digest[:])[:8]
	plan := metadataStringValue(meta, "plan_type")
	if plan == "" {
		plan = "free"
	}
	name := codex.CredentialFileName(metadataStringValue(meta, "email"), plan, hash, true)
	if provider == "bps" {
		name = "bps" + strings.TrimPrefix(name, "codex")
	}
	return &coreauth.Auth{ID: name, FileName: name, Provider: provider, Label: metadataStringValue(meta, "email"), Status: coreauth.StatusActive, Metadata: meta}
}

func setImportedStorage(record *coreauth.Auth) {
	if record.Provider == "bps" {
		record.Storage = &bps.TokenStorage{Metadata: record.Metadata}
		return
	}
	record.Storage = &codex.CodexTokenStorage{Type: "codex", Email: metadataStringValue(record.Metadata, "email"), AccountID: metadataStringValue(record.Metadata, "account_id"), AccessToken: metadataStringValue(record.Metadata, "access_token"), RefreshToken: metadataStringValue(record.Metadata, "refresh_token"), IDToken: metadataStringValue(record.Metadata, "id_token"), Expire: metadataStringValue(record.Metadata, "expired"), LastRefresh: metadataStringValue(record.Metadata, "last_refresh"), PlanType: metadataStringValue(record.Metadata, "plan_type")}
}

// saveImportedOAuth makes storage failure fatal and publishes under the manager's mutation gate.
func (h *Handler) saveImportedOAuth(ctx context.Context, provider string, meta map[string]any) (*coreauth.Auth, error) {
	if h == nil || h.authManager == nil || h.configSnapshot() == nil {
		return nil, errors.New("auth_manager_unavailable")
	}
	h.pluginLoginSaveMu.Lock()
	defer h.pluginLoginSaveMu.Unlock()
	store := h.tokenStoreWithBaseDir()
	if store == nil {
		return nil, errors.New("credential_store_unavailable")
	}
	record := importedRecord(provider, meta)
	for _, candidate := range h.authManager.List() {
		if sameImportedIdentity(candidate, provider, meta) {
			record = candidate.Clone()
			break
		}
	}
	ctx = coreauth.WithAuthCreationIntent(ctx)
	save := func(current *coreauth.Auth) error {
		merged := make(map[string]any, len(current.Metadata)+len(meta))
		for key, value := range current.Metadata {
			merged[key] = value
		}
		for key, value := range meta {
			merged[key] = value
		}
		if provider == "bps" {
			delete(merged, "id_token")
		}
		current.Metadata = merged
		current.RegistrationEpoch++
		setImportedStorage(current)
		if h.postAuthHook != nil {
			if err := h.postAuthHook(ctx, current); err != nil {
				return errors.New("credential_hook_failed")
			}
		}
		path, err := store.Save(ctx, current)
		if err != nil || strings.TrimSpace(path) == "" {
			return errors.New("credential_save_failed")
		}
		if current.Attributes == nil {
			current.Attributes = make(map[string]string)
		}
		current.Attributes[coreauth.AttributePath] = path
		current.Attributes[coreauth.AttributeSource] = path
		return nil
	}
	var err error
	if _, exists := h.authManager.GetByID(record.ID); exists {
		var modified bool
		record, modified, err = h.authManager.Modify(coreauth.WithSkipPersist(ctx), record.ID, 0, save)
		if err == nil && !modified {
			err = errors.New("credential_registration_failed")
		}
	} else {
		if err = save(record); err == nil {
			record, err = h.authManager.Register(coreauth.WithSkipPersist(ctx), record)
		}
	}
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, errors.New("credential_registration_failed")
	}
	if h.postAuthPersistHook != nil {
		if errHook := h.postAuthPersistHook(ctx, record); errHook != nil {
			return nil, errors.New("credential_registration_failed")
		}
	}
	actual, exists := h.authManager.GetByID(record.ID)
	if !exists || actual == nil || metadataStringValue(actual.Metadata, "access_token") != metadataStringValue(meta, "access_token") {
		return nil, errors.New("credential_registration_failed")
	}
	return actual, nil
}

func (h *Handler) importOpenAIOAuthV8(c *gin.Context, provider string) {
	meta, err := readImportMetadata(c, provider)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": err.Error()})
		return
	}
	record, err := h.saveImportedOAuth(c.Request.Context(), provider, meta)
	if err != nil {
		code, status := "credential_commit_failed", http.StatusBadGateway
		if h.authManager == nil {
			code, status = "auth_manager_unavailable", http.StatusServiceUnavailable
		}
		c.JSON(status, gin.H{"status": "error", "error": code})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "provider": provider, "auth_id": record.ID, "saved": true, "registered": true})
}

type bpsOAuthSession struct {
	mu         sync.Mutex
	owner      *Handler
	client     *bps.Client
	ctx        context.Context
	cancel     context.CancelFunc
	verifier   string
	exchanging bool
	done       chan struct{}
	target     *oauthRecoveryTarget
}

func (h *Handler) requestBPSToken(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "auth_manager_unavailable"})
		return
	}
	state, verifier, challenge, err := bps.NewPKCE()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "oauth_state_unavailable"})
		return
	}
	cfg := h.oauthConfigWithProxy(oauthSessionProxyURLFromContext(c))
	proxy := ""
	if cfg != nil {
		proxy = cfg.ProxyURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), oauthSessionTTL)
	flow := &bpsOAuthSession{owner: h, client: bps.NewClient(proxy), ctx: ctx, cancel: cancel, verifier: verifier}
	if target, ok := oauthSessionTargetFromContext(c); ok {
		target.VerifyAfterLogin = false
		flow.target = &target
	}
	if transport := c.Request.Context().Value("cliproxy.roundtripper"); transport != nil {
		flow.ctx = context.WithValue(ctx, "cliproxy.roundtripper", transport)
	}
	RegisterOAuthSessionWithProxy(state, "bps", proxy)
	oauthSessions.mu.Lock()
	session := oauthSessions.sessions[state]
	session.BPS = flow
	oauthSessions.sessions[state] = session
	oauthSessions.mu.Unlock()
	c.JSON(http.StatusOK, gin.H{"status": "ok", "url": bps.AuthorizationURL(state, challenge), "state": state})
}

func (h *Handler) handleBPSOAuthCallback(c *gin.Context, req siwcCallbackRequest, state string, session oauthSession) {
	flow := session.BPS
	if flow == nil || flow.owner != h {
		c.JSON(http.StatusNotFound, gin.H{"error": "oauth_state_not_found"})
		return
	}
	if req.Provider != "bps" || c.Request.Method != http.MethodPost {
		c.JSON(http.StatusBadRequest, gin.H{"error": "oauth_provider_mismatch"})
		return
	}
	redirect, err := url.Parse(strings.TrimSpace(req.RedirectURL))
	expected, _ := url.Parse(bps.RedirectURI)
	if err != nil || redirect.Scheme != expected.Scheme || redirect.Host != expected.Host || redirect.Path != expected.Path || redirect.RawPath != "" || redirect.User != nil || redirect.Fragment != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_callback_url"})
		return
	}
	query, errQuery := url.ParseQuery(redirect.RawQuery)
	if errQuery != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_callback_query"})
		return
	}
	for key, values := range query {
		switch key {
		case "state", "code", "error", "error_description", "iss", "client_id":
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_callback_query"})
			return
		}
		if len(values) != 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "conflicting_callback_parameters"})
			return
		}
	}
	if query.Get("state") != state || (req.Code != "" && req.Code != query.Get("code")) || (req.Error != "" && req.Error != query.Get("error")) || (query.Get("client_id") != "" && query.Get("client_id") != bps.ClientID) || (req.ClientID != "" && req.ClientID != bps.ClientID) || (query.Get("iss") != "" && query.Get("iss") != "https://auth.openai.com") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "callback_state_mismatch"})
		return
	}
	code := query.Get("code")
	if code == "" && query.Get("error") == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "callback_code_required"})
		return
	}
	flow.mu.Lock()
	defer flow.mu.Unlock()
	if flow.exchanging || flow.ctx.Err() != nil || !IsOAuthSessionPending(state, "bps") {
		c.JSON(http.StatusConflict, gin.H{"error": "oauth_session_not_pending"})
		return
	}
	if query.Get("error") != "" {
		SetOAuthSessionError(state, "oauth_authorization_failed")
		flow.cancel()
		flow.verifier = ""
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}
	flow.exchanging = true
	flow.done = make(chan struct{})
	go flow.exchange(state, code)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (flow *bpsOAuthSession) exchange(state, code string) {
	defer close(flow.done)
	flow.mu.Lock()
	verifier := flow.verifier
	flow.mu.Unlock()
	meta, err := flow.client.Exchange(flow.ctx, code, verifier)
	flow.mu.Lock()
	defer flow.mu.Unlock()
	defer flow.cancel()
	flow.verifier = ""
	if flow.ctx.Err() != nil || !IsOAuthSessionPending(state, "bps") {
		return
	}
	if err != nil {
		SetOAuthSessionError(state, "oauth_exchange_failed")
		return
	}
	meta["login_method"] = "oauth"
	if err = validateImportMetadata(meta, "bps"); err != nil {
		SetOAuthSessionError(state, "oauth_identity_invalid")
		return
	}
	if flow.target != nil {
		target, exists := flow.owner.authManager.GetByID(flow.target.AuthID)
		if !exists || !sameImportedIdentity(target, "bps", meta) {
			SetOAuthSessionError(state, "oauth_target_identity_mismatch")
			return
		}
	}
	if err = claimOAuthSessionCommit(state, "bps"); err != nil {
		return
	}
	record, err := flow.owner.saveImportedOAuth(context.WithoutCancel(flow.ctx), "bps", meta)
	if err != nil {
		FailOAuthSessionCommit(state, "credential_commit_failed")
		return
	}
	CompleteOAuthSessionWithOutcome(state, record.ID, authGrantFingerprint(record), true, true, false, 0, 0, record.CredentialVersion, record.RegistrationEpoch, "", "")
}
