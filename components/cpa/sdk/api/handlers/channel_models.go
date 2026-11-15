package handlers

import (
	"context"
	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
)

func FilterChannelModels(ctx context.Context, body []byte) []byte {
	return filterChannelModels(ctx, body, nil)
}

func filterChannelModels(ctx context.Context, body []byte, allowed func(string) bool) []byte {
	doc := ojson.NewDoc(body)
	for _, key := range []string{"data", "models"} {
		value := ojson.GetBytes(body, key)
		if !value.IsArray() {
			continue
		}
		kept := make([]any, 0)
		for _, item := range value.Array() {
			id := item.Get("id").String()
			if id == "" {
				id = item.Get("slug").String()
			}
			if id == "" {
				id = strings.TrimPrefix(item.Get("name").String(), "models/")
			}
			if allowed != nil {
				if allowed(id) {
					kept = append(kept, item.Value())
				}
				continue
			}
			providers := registry.GetGlobalRegistry().GetModelProviders(id)
			if len(providers) == 0 && channelscope.From(ctx) == "" {
				kept = append(kept, item.Value())
				continue
			}
			if len(channelscope.Filter(ctx, providers)) > 0 {
				kept = append(kept, item.Value())
			}
		}
		_ = doc.Set(key, kept)
	}
	return doc.Bytes()
}
