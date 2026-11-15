package cpa

import (
	"net"
	"slices"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

var kiloRetirementReasons = []string{"", "quota_return", "account_removed", "unique_account_limit", "max_ips_reduced", "fixed_capacity", "egress_unavailable"}

func validKiloAutoState(state *model.KiloProxyState) bool {
	if state.AutoError != nil && *state.AutoError != "storage_failed" {
		return false
	}
	if state.AutoAssignmentEnabled == nil && state.MaxUniqueAccounts == nil && state.IPGroups == nil {
		return state.AutoError == nil
	}
	if state.AutoAssignmentEnabled == nil || state.MaxUniqueAccounts == nil || state.IPGroups == nil || *state.IPGroups == nil {
		return false
	}
	if *state.MaxUniqueAccounts < 0 || int64(*state.MaxUniqueAccounts) > 9007199254740991 || (*state.AutoAssignmentEnabled && *state.MaxUniqueAccounts == 0) {
		return false
	}
	for _, group := range *state.IPGroups {
		if net.ParseIP(group.IP) == nil || group.AccountIDs == nil || group.ProxyIDs == nil || group.UniqueAccounts < 0 || group.MaxUniqueAccounts < 0 || int64(group.MaxUniqueAccounts) > 9007199254740991 || !slices.Contains(kiloRetirementReasons, group.Reason) || !slices.Contains([]string{"available", "assigned", "retiring", "draining", "retired", "unverified", "at_limit"}, group.State) {
			return false
		}
	}
	return true
}
