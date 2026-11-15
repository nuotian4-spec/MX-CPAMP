package config

import (
	"fmt"
	"strings"
)

const BPSCompactThreshold = 200000

func (c BPSConfig) SearchEnabled() bool { return c.WebSearch == nil || *c.WebSearch }

func (c BPSConfig) DiscoverModels() bool { return c.AutoDiscover == nil || *c.AutoDiscover }
func (c BPSConfig) OmitHistory() bool {
	return c.OmitEncryptedHistory == nil || *c.OmitEncryptedHistory
}

func (c BPSConfig) UpstreamModel(model string) string {
	for _, manual := range c.Models {
		if strings.TrimSpace(manual) == model {
			return model
		}
	}
	if c.DiscoverModels() && c.ModelSuffix != "" {
		return strings.TrimSuffix(model, c.ModelSuffix)
	}
	return model
}

func (c BPSConfig) Validate() error {
	if c.CompactThreshold != 0 && c.CompactThreshold != BPSCompactThreshold {
		return fmt.Errorf("BPS压缩阈值必须为200000")
	}
	if c.ModelSuffix != "" && c.ModelSuffix != "-excel" {
		return fmt.Errorf("BPS模型后缀必须为空或-excel")
	}
	return nil
}
