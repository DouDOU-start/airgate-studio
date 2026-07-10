package studio

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// ==================== 策略判定 ====================

func TestResolveStrategy(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		protocols []string
		hasImages bool
		want      genStrategy
	}{
		// Imagen 系 → predict（不受输入图影响）
		{"imagen×gemini", "imagen-4.0-generate-001", []string{"gemini"}, false, strategyImagenPredict},
		{"imagen×gemini 带输入图", "imagen-3", []string{"gemini"}, true, strategyImagenPredict},
		{"imagen 多协议含 gemini", "imagen-4", []string{"openai", "gemini"}, false, strategyImagenPredict},
		{"厂商前缀 imagen", "google/imagen-4-ultra", []string{"gemini"}, false, strategyImagenPredict},
		// 其他 gemini 图像模型 → generateContent
		{"gemini 图像模型", "gemini-2.5-flash-image", []string{"gemini"}, false, strategyGeminiContent},
		{"gemini 带输入图", "gemini-2.5-flash-image", []string{"gemini"}, true, strategyGeminiContent},
		{"gemini 协议非 imagen 名", "nano-banana", []string{"gemini"}, false, strategyGeminiContent},
		// gpt-image / dall-e × openai → generations / edits
		{"gpt-image 无输入图", "gpt-image-1", []string{"openai"}, false, strategyImagesGenerations},
		{"gpt-image 有输入图", "gpt-image-1", []string{"openai"}, true, strategyImagesEdits},
		{"gpt-image-1-mini", "gpt-image-1-mini", []string{"openai"}, false, strategyImagesGenerations},
		{"dall-e-3", "dall-e-3", []string{"openai"}, false, strategyImagesGenerations},
		{"dall-e-2 有输入图", "dall-e-2", []string{"openai"}, true, strategyImagesEdits},
		{"厂商前缀 dall-e", "openai/dall-e-3", []string{"openai"}, false, strategyImagesGenerations},
		{"大小写不敏感", "GPT-Image-1", []string{"OpenAI"}, false, strategyImagesGenerations},
		// 回退 chat
		{"imagen 无 gemini 协议", "imagen-4", []string{"openai"}, false, strategyChat},
		{"gpt-image 无 openai 协议", "gpt-image-1", []string{"anthropic"}, false, strategyChat},
		{"openai 普通多模态模型", "gpt-4o", []string{"openai"}, false, strategyChat},
		{"openai 其他图像模型", "flux-dev", []string{"openai"}, true, strategyChat},
		{"anthropic 模型", "claude-sonnet-4-5", []string{"anthropic"}, false, strategyChat},
		{"协议为空", "whatever-model", nil, false, strategyChat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveStrategy(tt.model, tt.protocols, tt.hasImages); got != tt.want {
				t.Fatalf("resolveStrategy(%q, %v, %v) = %s, want %s",
					tt.model, tt.protocols, tt.hasImages, got, tt.want)
			}
		})
	}
}

func TestGuessProtocolsByModelName(t *testing.T) {
	tests := []struct {
		model string
		want  []string
	}{
		{"imagen-4.0-generate-001", []string{"gemini"}},
		{"gemini-2.5-flash-image", []string{"gemini"}},
		{"google/gemini-2.0-flash", []string{"gemini"}},
		{"veo-3", []string{"gemini"}},
		{"claude-sonnet-4-5", []string{"anthropic"}},
		{"gpt-image-1", []string{"openai"}},
		{"dall-e-3", []string{"openai"}},
		{"flux-dev", []string{"openai"}},
		{"unknown-model", []string{"openai"}},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := guessProtocolsByModelName(tt.model); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("guessProtocolsByModelName(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestGuessModalityByModelName(t *testing.T) {
	tests := []struct {
		model string
		want  string
	}{
		{"gpt-image-1", modalityImage},
		{"imagen-4.0-generate-001", modalityImage},
		{"gemini-2.5-flash-image", modalityImage},
		{"veo-3", modalityVideo},
		{"google/veo-3.1", modalityVideo},
		{"sora-2", modalityVideo},
		{"kling-v2", modalityVideo},
		// "wan" 前缀有歧义（wan2.x 视频 / wanx-t2i 文生图），不猜、默认 image
		{"wanx2.1-t2i-turbo", modalityImage},
		{"suno-v5", modalityMusic},
		{"lyria-2", modalityMusic},
		{"unknown-model", modalityImage},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := guessModalityByModelName(tt.model); got != tt.want {
				t.Fatalf("guessModalityByModelName(%q) = %s, want %s", tt.model, got, tt.want)
			}
		})
	}
}

// ==================== 目录缓存 ====================

// fakeLister modelProtocolLister 的脚本化假实现。
type fakeLister struct {
	byModel map[string][]string
	err     error
	calls   int
}

func (f *fakeLister) ModelProtocols(_ context.Context, _ string) (map[string][]string, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.byModel, nil
}

func TestProtocolCatalogCachesWithinTTL(t *testing.T) {
	lister := &fakeLister{byModel: map[string][]string{"gpt-image-1": {"openai"}}}
	c := newProtocolCatalog(lister, time.Minute)

	for i := 0; i < 3; i++ {
		got := c.ProtocolsFor(context.Background(), "sk-x", "gpt-image-1")
		if !reflect.DeepEqual(got, []string{"openai"}) {
			t.Fatalf("第 %d 次 ProtocolsFor = %v", i+1, got)
		}
	}
	if lister.calls != 1 {
		t.Fatalf("TTL 内应只拉取一次目录，calls = %d", lister.calls)
	}
}

func TestProtocolCatalogRefetchesAfterTTL(t *testing.T) {
	lister := &fakeLister{byModel: map[string][]string{"m": {"openai"}}}
	c := newProtocolCatalog(lister, time.Minute)

	c.ProtocolsFor(context.Background(), "sk-x", "m")
	// 手动使缓存过期
	c.fetchedAt = time.Now().Add(-2 * time.Minute)
	lister.byModel = map[string][]string{"m": {"gemini"}}

	if got := c.ProtocolsFor(context.Background(), "sk-x", "m"); !reflect.DeepEqual(got, []string{"gemini"}) {
		t.Fatalf("过期后应取到新目录，got %v", got)
	}
	if lister.calls != 2 {
		t.Fatalf("calls = %d, want 2", lister.calls)
	}
}

func TestProtocolCatalogFallsBackToHeuristic(t *testing.T) {
	t.Run("目录拉取失败", func(t *testing.T) {
		lister := &fakeLister{err: fmt.Errorf("core 不可达")}
		c := newProtocolCatalog(lister, time.Minute)
		if got := c.ProtocolsFor(context.Background(), "sk-x", "imagen-4"); !reflect.DeepEqual(got, []string{"gemini"}) {
			t.Fatalf("拉取失败应按模型名兜底，got %v", got)
		}
	})
	t.Run("目录未收录该模型", func(t *testing.T) {
		lister := &fakeLister{byModel: map[string][]string{"other": {"openai"}}}
		c := newProtocolCatalog(lister, time.Minute)
		if got := c.ProtocolsFor(context.Background(), "sk-x", "gemini-2.5-flash-image"); !reflect.DeepEqual(got, []string{"gemini"}) {
			t.Fatalf("未收录模型应按名称兜底，got %v", got)
		}
	})
	t.Run("拉取失败沿用旧缓存", func(t *testing.T) {
		lister := &fakeLister{byModel: map[string][]string{"m": {"gemini"}}}
		c := newProtocolCatalog(lister, time.Minute)
		c.ProtocolsFor(context.Background(), "sk-x", "m")
		// 缓存过期 + 拉取开始报错 → 沿用旧目录
		c.fetchedAt = time.Now().Add(-2 * time.Minute)
		lister.err = fmt.Errorf("core 不可达")
		if got := c.ProtocolsFor(context.Background(), "sk-x", "m"); !reflect.DeepEqual(got, []string{"gemini"}) {
			t.Fatalf("拉取失败应沿用旧缓存，got %v", got)
		}
	})
}

func TestBareModelName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"gpt-image-1", "gpt-image-1"},
		{"OpenAI/GPT-Image-1", "gpt-image-1"},
		{"  google/imagen-4 ", "imagen-4"},
		{"a/b/c-model", "c-model"},
	}
	for _, tt := range tests {
		if got := bareModelName(tt.in); got != tt.want {
			t.Fatalf("bareModelName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
