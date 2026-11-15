package model

import (
	"encoding/json"
	"errors"
)

type KiloSourcePolicy struct {
	Enabled         bool    `json:"enabled"`
	IntervalSeconds int     `json:"interval_seconds"`
	Revision        uint64  `json:"revision"`
	Paused          bool    `json:"paused"`
	PauseGeneration uint64  `json:"pause_generation"`
	Scope           string  `json:"scope"`
	Status          string  `json:"status"`
	ObservedAt      *string `json:"observed_at"`
	NextQueryAt     *string `json:"next_query_at"`
	ErrorCode       *string `json:"error_code"`
	Persisted       bool    `json:"persisted"`
	ConnectionRef   string  `json:"connection_ref,omitempty"`
}

func (p *KiloSourcePolicy) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return errors.New("来源策略回执不是对象")
	}
	for _, key := range []string{"enabled", "interval_seconds", "revision", "paused", "pause_generation", "scope", "status", "observed_at", "next_query_at", "error_code", "persisted"} {
		value, ok := fields[key]
		if !ok || (string(value) == "null" && key != "observed_at" && key != "next_query_at" && key != "error_code") {
			return errors.New("来源策略回执字段缺失")
		}
	}
	type plain KiloSourcePolicy
	return json.Unmarshal(raw, (*plain)(p))
}
