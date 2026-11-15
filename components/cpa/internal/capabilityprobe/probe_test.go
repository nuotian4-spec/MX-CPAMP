package capabilityprobe

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const candidate = `config-version: 8
access:
  api-keys: [key-kilo, key-codex, key-bps, key-legacy]
  client-key-scopes:
    - api-key: key-kilo
      channel: kilo
    - api-key: key-codex
      channel: codex
    - api-key: key-bps
      channel: bps
`

func TestPolicyUsesProductionAuthenticationAndChannelDecisions(t *testing.T) {
	evidence, err := Probe([]byte(candidate), strings.Repeat("1", 40))
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Kind != "authz-policy-matrix" || evidence.KeyCount != 4 || len(evidence.Observations) != 22 {
		t.Fatalf("unexpected coverage: %+v", evidence)
	}
	observations := make(map[string]Observation)
	for _, observation := range evidence.Observations {
		observations[observation.ID] = observation
	}
	for index, scope := range []string{"kilo", "codex", "bps", ""} {
		prefix := "key-" + string(rune('0'+index))
		auth := observations[prefix+"/authenticate"]
		if auth.Authenticated == nil || !*auth.Authenticated || auth.Scope != scope {
			t.Fatalf("authentication mismatch for %s", prefix)
		}
		for _, channel := range []string{"kilo", "codex", "bps"} {
			actual := observations[prefix+"/channel/"+channel].Allowed
			expected := scope == channel || scope == "" && channel != "kilo"
			if actual == nil || *actual != expected {
				t.Fatalf("policy mismatch: %s -> %s", prefix, channel)
			}
		}
	}
	for _, id := range []string{"invalid/authenticate", "missing/authenticate"} {
		actual := observations[id]
		if actual.Authenticated == nil || *actual.Authenticated || actual.ErrorCode == "" {
			t.Fatalf("invalid request authenticated: %s", id)
		}
	}
	raw, _ := json.Marshal(evidence)
	for _, secret := range []string{"key-kilo", "key-codex", "key-bps", "key-legacy"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("secret leaked into evidence")
		}
	}
}

func TestCLIReadsExactBytesWithoutChangingConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.yaml")
	if err := os.WriteFile(path, []byte(candidate), 0600); err != nil {
		t.Fatal(err)
	}
	var output, errors bytes.Buffer
	if code := RunCLI([]string{"--capability-probe", "--config", path}, &output, &errors, strings.Repeat("2", 40)); code != 0 {
		t.Fatalf("probe failed: %d %s", code, errors.String())
	}
	var evidence Evidence
	if err := json.Unmarshal(output.Bytes(), &evidence); err != nil {
		t.Fatal(err)
	}
	checksum := sha256.Sum256([]byte(candidate))
	if evidence.ConfigSHA256 != hex.EncodeToString(checksum[:]) {
		t.Fatal("config byte binding mismatch")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != candidate {
		t.Fatal("probe changed candidate bytes")
	}
}

func TestInvalidConfigurationProducesNoEvidence(t *testing.T) {
	if _, err := Probe([]byte("access: ["), "none"); err == nil {
		t.Fatal("invalid configuration accepted")
	}
	var output, errors bytes.Buffer
	if code := RunCLI([]string{"--capability-probe=false"}, &output, &errors, "none"); code == 0 || output.Len() != 0 {
		t.Fatal("invalid probe invocation emitted evidence")
	}
}

func TestDispatchPrecedesPluginBootstrapAndAccountInitialization(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "cmd", "server", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, "func main() {")
	branch := strings.Index(text[start:], "capabilityprobe.Requested(os.Args[1:])")
	bootstrap := strings.Index(text[start:], "pluginHost := pluginhost.New()")
	dotenv := strings.Index(text[start:], "godotenv.Load(")
	if branch < 0 || bootstrap <= branch || dotenv <= branch {
		t.Fatal("probe dispatch is not before service initialization")
	}
	for _, argument := range []string{"--capability-probe", "-capability-probe", "--capability-probe=false"} {
		if !Requested([]string{argument}) {
			t.Fatal("probe argument missed by early dispatch")
		}
	}
	if Requested([]string{"--config", "config.yaml"}) {
		t.Fatal("ordinary server command was diverted")
	}
}
