// Package capabilityprobe observes config policy without starting services.
package capabilityprobe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	configaccess "github.com/router-for-me/CLIProxyAPI/v8/internal/access/config_access"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
)

const Validator = "mx-authz-policy-v1"

type Observation struct {
	ID            string   `json:"id"`
	Authenticated *bool    `json:"authenticated,omitempty"`
	Scope         string   `json:"scope,omitempty"`
	ErrorCode     string   `json:"error_code,omitempty"`
	Allowed       *bool    `json:"allowed,omitempty"`
	Visible       []string `json:"visible,omitempty"`
}

type Evidence struct {
	Schema       int           `json:"schema"`
	Kind         string        `json:"kind"`
	Validator    string        `json:"validator"`
	SourceCommit string        `json:"source_commit"`
	ConfigSHA256 string        `json:"config_sha256"`
	KeyCount     int           `json:"key_count"`
	Observations []Observation `json:"observations"`
}

func Requested(args []string) bool {
	for _, arg := range args {
		name := strings.TrimLeft(arg, "-")
		if name == "capability-probe" || strings.HasPrefix(name, "capability-probe=") {
			return true
		}
	}
	return false
}

func boolean(value bool) *bool { return &value }

// Probe uses the production parser, access provider and channel policy functions.
// It does not construct an auth store, plugin host, executor or HTTP client.
func Probe(raw []byte, commit string) (Evidence, error) {
	cfg, err := config.ParseConfigBytes(raw)
	if err != nil {
		return Evidence{}, errors.New("configuration_parse_failed")
	}
	if err = cfg.SDKConfig.ValidateClientKeyScopes(); err != nil {
		return Evidence{}, errors.New("configuration_scope_invalid")
	}
	keys := make([]string, 0, len(cfg.APIKeys))
	seen := make(map[string]bool)
	for _, value := range cfg.APIKeys {
		key := strings.TrimSpace(value)
		if key != "" && !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	if len(keys) == 0 {
		return Evidence{}, errors.New("configuration_keys_empty")
	}
	configaccess.Register(&cfg.SDKConfig)
	defer configaccess.Register(nil)
	manager := access.NewManager()
	manager.SetProviders(access.RegisteredProviders())
	checksum := sha256.Sum256(raw)
	evidence := Evidence{Schema: 1, Kind: "authz-policy-matrix", Validator: Validator,
		SourceCommit: commit, ConfigSHA256: hex.EncodeToString(checksum[:]), KeyCount: len(keys)}
	channels := []string{"kilo", "codex", "bps"}
	for index, key := range keys {
		request, _ := http.NewRequest(http.MethodGet, "http://policy.local/v1/models", nil)
		request.Header.Set("Authorization", "Bearer "+key)
		result, authErr := manager.Authenticate(context.Background(), request)
		authenticated := authErr == nil && result != nil
		scope := ""
		if result != nil {
			scope = result.Metadata["channel_scope"]
		}
		prefix := fmt.Sprintf("key-%d", index)
		evidence.Observations = append(evidence.Observations, Observation{ID: prefix + "/authenticate", Authenticated: boolean(authenticated), Scope: scope})
		ctx := channelscope.With(context.Background(), scope)
		for _, channel := range channels {
			evidence.Observations = append(evidence.Observations, Observation{ID: prefix + "/channel/" + channel, Allowed: boolean(authenticated && channelscope.Allows(ctx, channel))})
		}
		visible := []string{}
		if authenticated {
			visible = channelscope.Filter(ctx, channels)
		}
		evidence.Observations = append(evidence.Observations, Observation{ID: prefix + "/visible", Visible: visible})
	}
	invalid := "mx-capability-invalid"
	for seen[invalid] {
		invalid += "-invalid"
	}
	for _, item := range []struct{ name, token string }{{"invalid", invalid}, {"missing", ""}} {
		request, _ := http.NewRequest(http.MethodGet, "http://policy.local/v1/models", nil)
		if item.token != "" {
			request.Header.Set("Authorization", "Bearer "+item.token)
		}
		result, authErr := manager.Authenticate(context.Background(), request)
		code := ""
		if authErr != nil {
			code = string(authErr.Code)
		}
		evidence.Observations = append(evidence.Observations, Observation{ID: item.name + "/authenticate", Authenticated: boolean(authErr == nil && result != nil), ErrorCode: code})
	}
	return evidence, nil
}

func RunCLI(args []string, output, errorOutput io.Writer, commit string) int {
	flags := flag.NewFlagSet("capability-probe", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	probe := flags.Bool("capability-probe", false, "Observe local authorization policy")
	path := flags.String("config", "", "Read-only candidate configuration")
	if err := flags.Parse(args); err != nil || !*probe || *path == "" || flags.NArg() != 0 {
		_, _ = fmt.Fprintln(errorOutput, "capability_probe_arguments_invalid")
		return 2
	}
	raw, err := os.ReadFile(*path)
	if err != nil {
		_, _ = fmt.Fprintln(errorOutput, "capability_probe_config_unreadable")
		return 1
	}
	evidence, err := Probe(raw, commit)
	if err != nil {
		_, _ = fmt.Fprintln(errorOutput, err.Error())
		return 1
	}
	if err := json.NewEncoder(output).Encode(evidence); err != nil {
		_, _ = fmt.Fprintln(errorOutput, "capability_probe_output_failed")
		return 1
	}
	return 0
}
