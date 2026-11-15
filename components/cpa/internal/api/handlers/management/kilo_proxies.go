package management

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func (h *Handler) kiloStateSource() (*executor.KiloExecutor, error) {
	if h.authManager == nil {
		return nil, kilo.ErrStateStorage
	}
	h.mu.Lock()
	cfg := h.cfg.CloneForRuntime()
	h.mu.Unlock()
	e := h.authManager.EnsureKiloExecutor(func() coreauth.ProviderExecutor { return executor.NewKiloExecutor(cfg) })
	source, ok := e.(*executor.KiloExecutor)
	if !ok {
		return nil, kilo.ErrStateStorage
	}
	return source, nil
}
func kiloProxyError(c *gin.Context, err error) {
	if errors.Is(err, kilo.ErrProxyIPAlreadyRegistered) {
		body := gin.H{"error": kilo.ErrProxyIPAlreadyRegistered.Error()}
		var duplicate *kilo.ProxyIPAlreadyRegisteredError
		if errors.As(err, &duplicate) {
			body["ip"], body["proxy_id"] = duplicate.IP, duplicate.ProxyID
		}
		c.JSON(http.StatusConflict, body)
		return
	}
	status, message := http.StatusServiceUnavailable, kilo.ErrStateStorage.Error()
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, message = 499, "Kilo proxy operation canceled"
	case errors.Is(err, kilo.ErrProxyHealth):
		status, message = http.StatusBadGateway, kilo.ErrProxyHealth.Error()
	case errors.Is(err, kilo.ErrStateConflict), errors.Is(err, kilo.ErrStateBusy):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, kilo.ErrStateInvalid):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, kilo.ErrProxyMissing):
		status, message = http.StatusNotFound, err.Error()
	}
	c.JSON(status, gin.H{"error": message})
}
func (h *Handler) GetKiloProxies(c *gin.Context) {
	source, err := h.kiloStateSource()
	if err != nil {
		kiloProxyError(c, err)
		return
	}
	view, err := source.KiloProxyState()
	if err != nil {
		kiloProxyError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, view)
}
func decodeKiloProxyChanges(c *gin.Context) (kilo.ProxyChanges, error) {
	change := kilo.ProxyChanges{ID: c.Param("id"), Create: c.Request.Method == http.MethodPost}
	egress := c.Request.Method == http.MethodPut
	pool := strings.HasSuffix(c.FullPath(), "/kilo/proxy-pool")
	if egress {
		change.EgressAccount = change.ID
		change.ID = ""
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || !ojson.ValidBytes(raw) {
		return change, kilo.ErrStateInvalid
	}
	root := ojson.ParseBytes(raw)
	if !root.IsObject() {
		return change, kilo.ErrStateInvalid
	}
	fields := map[string]ojson.Result{}
	root.ForEach(func(k, v ojson.Result) bool { fields[k.String()] = v; return true })
	rev, ok := fields["expected_revision"]
	if !ok || rev.Type != ojson.Number {
		return change, kilo.ErrStateInvalid
	}
	change.ExpectedRevision, err = strconv.ParseUint(rev.Raw, 10, 64)
	if err != nil {
		return change, kilo.ErrStateInvalid
	}
	delete(fields, "expected_revision")
	for name, v := range fields {
		if egress {
			switch name {
			case "capacity_mode", "allocation_mode", "max_ips", "specified_ips":
			case "mode", "primary_proxy_id":
				if v.Type != ojson.String {
					return change, kilo.ErrStateInvalid
				}
				if name == "mode" {
					change.Mode = v.String()
				} else {
					change.PrimaryProxyID = v.String()
				}
			case "candidate_proxy_ids":
				var err error
				change.CandidateProxyIDs, err = kilo.DecodeCandidates(v)
				if err != nil {
					return change, err
				}
			case "exclusive_proxy_ids":
				if !v.IsArray() {
					return change, kilo.ErrStateInvalid
				}
				for _, id := range v.Array() {
					if id.Type != ojson.String {
						return change, kilo.ErrStateInvalid
					}
					change.ExclusiveProxyIDs = append(change.ExclusiveProxyIDs, id.String())
				}
			default:
				return change, kilo.ErrStateInvalid
			}
			continue
		}
		if pool {
			if name == "ip_concurrency_limit" || name == "ip_entry_limit" {
				if name == "ip_entry_limit" {
					change.SetIPEntryLimit = true
					if v.Type == ojson.Null {
						continue
					}
				}
				n, e := strconv.Atoi(v.Raw)
				if e != nil || n < 1 || n > 100 {
					return change, kilo.ErrStateInvalid
				}
				if name == "ip_concurrency_limit" {
					change.IPConcurrencyLimit = &n
				} else {
					change.IPEntryLimit = &n
				}
				continue
			}
			if name == "auto_assignment_enabled" {
				if !v.IsBool() {
					return change, kilo.ErrStateInvalid
				}
				b := v.Bool()
				change.AutoAssignmentEnabled = &b
				continue
			}
			if name == "max_unique_accounts" {
				if v.Type != ojson.Number {
					return change, kilo.ErrStateInvalid
				}
				n, e := strconv.Atoi(v.Raw)
				if e != nil || n < 1 {
					return change, kilo.ErrStateInvalid
				}
				change.MaxUniqueAccounts = &n
				continue
			}
			if name != "proxy_limit" || v.Type != ojson.Number {
				return change, kilo.ErrStateInvalid
			}
			n, err := strconv.Atoi(v.Raw)
			if err != nil {
				return change, kilo.ErrStateInvalid
			}
			change.Limit = &n
			continue
		}
		switch name {
		case "fixed_exit_ip":
			if change.Create || v.Type != ojson.String {
				return change, kilo.ErrStateInvalid
			}
			ip := v.String()
			change.FixedIP = &ip
		case "url", "name", "account_id":
			if v.Type != ojson.String {
				return change, kilo.ErrStateInvalid
			}
			s := v.String()
			switch name {
			case "url":
				change.URL = &s
			case "name":
				change.Name = &s
			case "account_id":
				change.AccountID = &s
			}
		case "enabled":
			if !v.IsBool() {
				return change, kilo.ErrStateInvalid
			}
			b := v.Bool()
			change.Enabled = &b
		case "order":
			if change.Create || v.Type != ojson.Number {
				return change, kilo.ErrStateInvalid
			}
			n, e := strconv.Atoi(v.Raw)
			if e != nil {
				return change, kilo.ErrStateInvalid
			}
			change.Order = &n
		default:
			return change, kilo.ErrStateInvalid
		}
	}
	if egress {
		capacity, e := kilo.DecodeCapacityPolicy(root)
		if e != nil {
			return change, e
		}
		change.CapacityPolicy = capacity
		for _, name := range []string{"mode", "exclusive_proxy_ids", "primary_proxy_id"} {
			if _, ok := fields[name]; !ok {
				return change, kilo.ErrStateInvalid
			}
		}
		if (change.Mode == "direct" || change.Mode == "auto") && (len(change.ExclusiveProxyIDs) > 0 || change.PrimaryProxyID != "" || change.CandidateProxyIDs != nil) {
			return change, kilo.ErrStateInvalid
		}
	}
	if pool && change.Limit == nil && change.AutoAssignmentEnabled == nil && change.MaxUniqueAccounts == nil && change.IPConcurrencyLimit == nil && !change.SetIPEntryLimit {
		return change, kilo.ErrStateInvalid
	}
	if change.Create && change.URL == nil {
		return change, kilo.ErrStateInvalid
	}
	return change, nil
}
func (h *Handler) ChangeKiloProxies(c *gin.Context) {
	change, err := decodeKiloProxyChanges(c)
	if err != nil {
		kiloProxyError(c, err)
		return
	}
	if _, err = h.kiloStateSource(); err != nil {
		kiloProxyError(c, err)
		return
	}
	view, err := h.authManager.ChangeKiloEgressObserved(c.Request.Context(), change)
	if err != nil {
		kiloProxyError(c, err)
		return
	}
	status := http.StatusOK
	if change.Create {
		status = http.StatusCreated
	}
	c.JSON(status, view)
}
