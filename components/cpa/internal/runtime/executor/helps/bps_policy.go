package helps

import (
	"fmt"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

// ApplyBPSRequestPolicy applies managed defaults before the final payload barrier.
func ApplyBPSRequestPolicy(doc *ojson.Doc, fast bool) error {
	entries := []any{}
	for _, entry := range doc.Get("context_management").Array() {
		if entry.Get("type").String() != "compaction" {
			entries = append(entries, entry.Value())
		}
	}
	entries = append(entries, map[string]any{"type": "compaction", "compact_threshold": config.BPSCompactThreshold})
	if err := doc.Set("context_management", entries); err != nil {
		return err
	}
	if fast {
		return doc.Set("service_tier", "priority")
	}
	return doc.Delete("service_tier")
}

// ValidateBPSRequestPolicy rejects final-rule conflicts without mutating the payload.
func ValidateBPSRequestPolicy(doc *ojson.Doc, fast bool) error {
	count := 0
	for _, entry := range doc.Get("context_management").Array() {
		if entry.Get("type").String() == "compaction" {
			count++
			if entry.Get("compact_threshold").Type != ojson.Number || entry.Get("compact_threshold").Float() != config.BPSCompactThreshold {
				return fmt.Errorf("BPS最终payload压缩阈值与受管200000冲突")
			}
		}
	}
	if count != 1 {
		return fmt.Errorf("BPS最终payload必须包含唯一压缩阈值")
	}
	tier := doc.Get("service_tier").String()
	if fast && tier != "priority" || !fast && tier != "" {
		return fmt.Errorf("BPS最终payload档位与Fast设置冲突")
	}
	return nil
}
