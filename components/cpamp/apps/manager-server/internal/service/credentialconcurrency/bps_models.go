package credentialconcurrency

import (
	"context"
	"net/http"
	"strings"
)

type BPSModels struct {
	Models        []string `json:"models"`
	ConnectionRef string   `json:"connection_ref"`
	Revision      string   `json:"revision"`
}

func (s *Service) GetBPSModels(ctx context.Context) (BPSModels, error) {
	setup, err := s.setup(ctx)
	if err != nil {
		return BPSModels{}, err
	}
	var config map[string]any
	revision, err := s.request(ctx, setup, http.MethodGet, "/config", "", "", nil, &config)
	if err != nil {
		return BPSModels{}, err
	}
	result := BPSModels{Models: []string{}, Revision: revision}
	oauth, _ := config["oauth"].(map[string]any)
	providers, _ := oauth["providers"].(map[string]any)
	bps, _ := providers["bps"].(map[string]any)
	if entries, ok := bps["models"].([]any); ok {
		for _, entry := range entries {
			if value, ok := entry.(string); ok {
				result.Models = append(result.Models, value)
			}
		}
	}
	result.ConnectionRef, err = s.bindConnection(ctx, setup)
	if err == nil && revision == "" {
		return BPSModels{}, failure(409, "config_revision_missing", "配置缺少版本，请刷新后重试")
	}
	return result, err
}

func (s *Service) PutBPSModels(ctx context.Context, input BPSModels) error {
	if input.Models == nil || input.Revision == "" || input.ConnectionRef == "" {
		return failure(400, "models_invalid", "请刷新后提交模型清单")
	}
	seen := map[string]bool{}
	models := make([]string, 0, len(input.Models))
	for _, id := range input.Models {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 256 || strings.ContainsAny(id, "\r\n\t") {
			return failure(400, "models_invalid", "模型ID无效")
		}
		if !seen[id] {
			models = append(models, id)
			seen[id] = true
		}
	}
	release, err := s.mutations.AcquireAll(ctx)
	if err != nil {
		return failure(409, "mutation_busy", "配置正在更新，请稍后刷新")
	}
	defer release()
	setup, err := s.setup(ctx)
	if err != nil {
		return err
	}
	if err = s.checkConnectionRef(input.ConnectionRef, setup); err != nil {
		return err
	}
	_, err = s.request(ctx, setup, http.MethodPut, "/config/oauth/providers/bps/models", input.Revision, input.ConnectionRef, models, nil)
	return err
}
