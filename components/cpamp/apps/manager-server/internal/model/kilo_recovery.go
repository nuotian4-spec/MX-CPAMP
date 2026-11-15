package model

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

type KiloAutoRestriction struct {
	Reason           string  `json:"reason"`
	Generation       uint64  `json:"generation"`
	IdentityRevision uint64  `json:"identity_revision"`
	SourceRevision   uint64  `json:"source_revision"`
	RecoverAt        *string `json:"recover_at"`
	QueryStatus      string  `json:"query_status"`
	ErrorCode        *string `json:"error_code"`
	Persisted        bool    `json:"persisted"`
}

// UnmarshalJSON只接受完整状态字段，额外材料不进入公开投影。
func (v *KiloAutoRestriction) UnmarshalJSON(raw []byte) error {
	invalid := errors.New("自动限制回执无效")
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return invalid
	}
	for _, name := range []string{"reason", "generation", "identity_revision", "source_revision", "recover_at", "query_status", "error_code", "persisted"} {
		value, ok := object[name]
		if !ok || (name != "recover_at" && name != "error_code" && strings.TrimSpace(string(value)) == "null") {
			return invalid
		}
	}
	type fields KiloAutoRestriction
	var result fields
	if json.Unmarshal(raw, &result) != nil || result.Generation > 9007199254740991 || result.IdentityRevision > 9007199254740991 || result.SourceRevision > 9007199254740991 {
		return invalid
	}
	if !slices.Contains([]string{"none", "quota_exhausted", "account_restricted"}, result.Reason) || !slices.Contains([]string{"never", "running", "success", "failed", "cancelled", "no_source", "no_future_reset"}, result.QueryStatus) {
		return invalid
	}
	if result.RecoverAt != nil {
		parsed, err := time.Parse(time.RFC3339, *result.RecoverAt)
		if err != nil {
			return invalid
		}
		_, offset := parsed.Zone()
		if offset != 0 {
			return invalid
		}
	}
	if result.ErrorCode != nil && !slices.Contains([]string{"source_missing", "source_changed", "identity_unknown", "no_outer_material", "source_unavailable", "refresh_failed", "refresh_proxy_invalid", "needs_reimport", "usage_unauthorized", "usage_forbidden", "usage_rate_limited", "usage_http_error", "usage_invalid_response", "usage_network_error", "usage_cancelled", "egress_unavailable", "storage_failed"}, *result.ErrorCode) {
		return invalid
	}
	*v = KiloAutoRestriction(result)
	return nil
}
