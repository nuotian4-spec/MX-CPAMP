package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestBPSRelayEffectiveContract(t *testing.T) {
	raw, err := os.ReadFile("/checks/cases.json")
	if err != nil { t.Fatal(err) }
	var cases []struct {
		Name string `json:"name"`
		Config map[string]any `json:"config"`
		Expected struct {
			Transport string `json:"image_transport"`
			Origin string `json:"image_relay_base_url"`
			Enabled bool `json:"enabled"`
			Images bool `json:"images_enabled"`
			State string `json:"state"`
		} `json:"expected"`
		Reject bool `json:"reject"`
	}
	if err = json.Unmarshal(raw, &cases); err != nil { t.Fatal(err) }
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			data, _ := json.Marshal(tc.Config)
			cfg, err := ParseConfigBytes(data)
			if tc.Reject {
				if err == nil { t.Fatal("接受无效父映射") }
				return
			}
			if err != nil { t.Fatal(err) }
			if cfg.BPSProtocol.Transport() != tc.Expected.Transport || cfg.BPSProtocol.ImageRelayBaseURL != tc.Expected.Origin || cfg.IsBPSProtocolEnabled() != tc.Expected.Enabled || cfg.IsBPSImagesEnabled() != tc.Expected.Images { t.Fatal("有效协议字段不符") }
			state := strings.ReplaceAll(strings.ReplaceAll(tc.Expected.State, "@CWD@", cwd), "@HOME@", home)
			if got := BPSStateDirectory(cfg); got != state { t.Fatalf("状态目录不符: %s", got) }
		})
	}
}
