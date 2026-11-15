package kilo

import (
	"context"
	"time"
)

type ProxyChangeCommit func(map[string]EgressAccountStatus) (ProxyStateView, error)
type ProxyChangeObserve func(context.Context) (ProxyChangeCommit, error)

type ProxyChangeObservation struct {
	IP  string
	Err error
}

type ProxyPreview struct {
	Status            string     `json:"status"`
	Reason            string     `json:"reason"`
	ObservedExitIP    *string    `json:"observed_exit_ip"`
	ObservedAt        *time.Time `json:"observed_at"`
	RegisteredProxyID string     `json:"registered_proxy_id,omitempty"`
}
