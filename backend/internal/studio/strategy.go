package studio

import (
	"context"
	"strings"
	"sync"
	"time"
)

// ==================== 生图执行策略判定 ====================
//
// core 网关是零翻译纯透传：不同协议的模型必须打到各自的原生端点。
// 本文件只放"选哪条路"的纯函数与 model→protocols 目录缓存；
// 各路径的请求装配/响应归一见 genimage_openai.go / genimage_gemini.go。

// genStrategy 一次生成任务的执行路径。
type genStrategy string

const (
	// strategyImagenPredict Imagen 系：POST /v1beta/models/{model}:predict。
	strategyImagenPredict genStrategy = "imagen_predict"
	// strategyGeminiContent Gemini 原生多模态：POST /v1beta/models/{model}:generateContent。
	strategyGeminiContent genStrategy = "gemini_content"
	// strategyImagesGenerations OpenAI Images 文生图：POST /v1/images/generations。
	strategyImagesGenerations genStrategy = "images_generations"
	// strategyImagesEdits OpenAI Images 图生图：POST /v1/images/edits（multipart）。
	strategyImagesEdits genStrategy = "images_edits"
	// strategyChat 回退路径：POST /v1/chat/completions 多模态。
	strategyChat genStrategy = "chat"
)

// bareModelName 归一化模型名：去掉厂商前缀（"openai/gpt-image-1" → "gpt-image-1"）并小写。
func bareModelName(model string) string {
	name := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// hasProtocol 判断协议列表是否包含指定协议（大小写不敏感）。
func hasProtocol(protocols []string, want string) bool {
	for _, p := range protocols {
		if strings.EqualFold(strings.TrimSpace(p), want) {
			return true
		}
	}
	return false
}

// isImagenModel 模型名匹配 imagen*（Imagen 系走 :predict）。
func isImagenModel(model string) bool {
	return strings.HasPrefix(bareModelName(model), "imagen")
}

// isOpenAIImagesAPIModel 模型名匹配 gpt-image* / dall-e*（走 /v1/images 端点）。
func isOpenAIImagesAPIModel(model string) bool {
	name := bareModelName(model)
	return strings.HasPrefix(name, "gpt-image") ||
		strings.HasPrefix(name, "dall-e") ||
		strings.HasPrefix(name, "dalle")
}

// resolveStrategy 按模型名 + 声明协议 + 是否携带输入图判定执行路径。
//
// 判定顺序（先命中先赢）：
//  1. imagen* 且 protocols 含 gemini → :predict（Imagen 无图生图语义，输入图忽略）；
//  2. protocols 含 gemini（其他 gemini 图像模型）→ :generateContent；
//  3. protocols 含 openai 且模型名匹配 gpt-image*/dall-e* → 无输入图 generations、有输入图 edits；
//  4. 其余回退 chat 多模态路径。
func resolveStrategy(model string, protocols []string, hasInputImages bool) genStrategy {
	switch {
	case hasProtocol(protocols, "gemini") && isImagenModel(model):
		return strategyImagenPredict
	case hasProtocol(protocols, "gemini"):
		return strategyGeminiContent
	case hasProtocol(protocols, "openai") && isOpenAIImagesAPIModel(model):
		if hasInputImages {
			return strategyImagesEdits
		}
		return strategyImagesGenerations
	default:
		return strategyChat
	}
}

// guessProtocolsByModelName 模型协议启发式兜底：目录拉不到（或未收录该模型）时按名称前缀猜。
func guessProtocolsByModelName(model string) []string {
	name := bareModelName(model)
	switch {
	case strings.HasPrefix(name, "imagen"),
		strings.HasPrefix(name, "gemini"),
		strings.HasPrefix(name, "veo"):
		return []string{"gemini"}
	case strings.HasPrefix(name, "claude"):
		return []string{"anthropic"}
	default:
		// 生态默认形态是 OpenAI 兼容
		return []string{"openai"}
	}
}

// ==================== model → protocols 目录缓存 ====================

// modelProtocolLister 供 protocolCatalog 拉取 core /v1/models 的 model→protocols 目录。
type modelProtocolLister interface {
	ModelProtocols(ctx context.Context, apiKey string) (map[string][]string, error)
}

// protocolCatalog core 模型目录的 TTL 缓存。
//
// worker 单 goroutine 执行，锁只为防御性保护；拉取失败沿用旧缓存，
// 完全取不到时按模型名启发式兜底，绝不阻断任务执行。
type protocolCatalog struct {
	mu        sync.Mutex
	core      modelProtocolLister
	ttl       time.Duration
	fetchedAt time.Time
	byModel   map[string][]string
}

// defaultProtocolCatalogTTL 模型目录缓存有效期。
const defaultProtocolCatalogTTL = 5 * time.Minute

// newProtocolCatalog 创建目录缓存；ttl<=0 时用默认值。
func newProtocolCatalog(core modelProtocolLister, ttl time.Duration) *protocolCatalog {
	if ttl <= 0 {
		ttl = defaultProtocolCatalogTTL
	}
	return &protocolCatalog{core: core, ttl: ttl}
}

// ProtocolsFor 返回模型声明的协议列表；目录缺失或未收录该模型时按名称启发式兜底。
func (c *protocolCatalog) ProtocolsFor(ctx context.Context, apiKey, model string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byModel == nil || time.Since(c.fetchedAt) >= c.ttl {
		if m, err := c.core.ModelProtocols(ctx, apiKey); err == nil {
			c.byModel = m
			c.fetchedAt = time.Now()
		}
		// 拉取失败：沿用旧缓存（若有），下次调用再重试
	}
	if p, ok := c.byModel[model]; ok && len(p) > 0 {
		return p
	}
	return guessProtocolsByModelName(model)
}
