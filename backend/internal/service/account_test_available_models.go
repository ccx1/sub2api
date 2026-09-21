package service

import (
	"context"
	"sort"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// GetAvailableModels 让管理员和账号所有者的测试弹窗共用模型目录。
// 上游发现失败时保留本地回退，仅返回模型信息，不返回账号凭证。
func (s *AccountTestService) GetAvailableModels(ctx context.Context, account *Account) any {
	if account.IsOpenAI() {
		if models, err := s.FetchOpenAIAccountModels(ctx, account); err == nil {
			return models
		}
		return openAIAvailableTestModels(account)
	}
	if account.IsGemini() {
		return geminiAvailableTestModels(account)
	}
	if account.Platform == PlatformAntigravity {
		return antigravity.DefaultModels()
	}
	if account.Platform == PlatformGrok {
		return grokAvailableTestModels(account)
	}
	if account.IsOAuth() {
		return claude.DefaultModels
	}
	return mappedAvailableTestModels(account.GetModelMapping(), claude.DefaultModels, testModelAdapter[claude.Model]{
		id: func(m claude.Model) string { return m.ID },
		fallback: func(id string) claude.Model {
			return claude.Model{ID: id, Type: "model", DisplayName: id}
		},
	})
}

func openAIAvailableTestModels(account *Account) []openai.Model {
	if account.IsOpenAIPassthroughEnabled() {
		return openai.DefaultModels
	}
	return mappedAvailableTestModels(account.GetModelMapping(), openai.DefaultModels, testModelAdapter[openai.Model]{
		id: func(m openai.Model) string { return m.ID },
		fallback: func(id string) openai.Model {
			return openai.Model{ID: id, Object: "model", Type: "model", DisplayName: id}
		},
	})
}

func geminiAvailableTestModels(account *Account) []geminicli.Model {
	if account.IsOAuth() {
		if account.IsGeminiGoogleOne() {
			return geminicli.GoogleOneModels
		}
		return geminicli.DefaultModels
	}
	return mappedAvailableTestModels(account.GetModelMapping(), geminicli.DefaultModels, testModelAdapter[geminicli.Model]{
		id: func(m geminicli.Model) string { return m.ID },
		fallback: func(id string) geminicli.Model {
			return geminicli.Model{ID: id, Type: "model", DisplayName: id}
		},
	})
}

func grokAvailableTestModels(account *Account) []xai.Model {
	defaults := xai.DefaultModels()
	hasMapping := false
	switch raw := account.Credentials["model_mapping"].(type) {
	case map[string]any:
		hasMapping = len(raw) > 0
	case map[string]string:
		hasMapping = len(raw) > 0
	}
	if !hasMapping {
		return defaults
	}
	return mappedAvailableTestModels(account.GetModelMapping(), defaults, testModelAdapter[xai.Model]{
		id: func(m xai.Model) string { return m.ID },
		fallback: func(id string) xai.Model {
			return xai.Model{ID: id, Object: "model", OwnedBy: "xai", DisplayName: id}
		},
	})
}

type testModelAdapter[T any] struct {
	id       func(T) string
	fallback func(string) T
}

func mappedAvailableTestModels[T any](mapping map[string]string, defaults []T, adapter testModelAdapter[T]) []T {
	if len(mapping) == 0 {
		return defaults
	}
	byID := make(map[string]T, len(defaults))
	for _, model := range defaults {
		byID[adapter.id(model)] = model
	}
	ids := make([]string, 0, len(mapping))
	for id := range mapping {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	models := make([]T, 0, len(ids))
	for _, id := range ids {
		model, ok := byID[id]
		if !ok {
			model = adapter.fallback(id)
		}
		models = append(models, model)
	}
	return models
}
