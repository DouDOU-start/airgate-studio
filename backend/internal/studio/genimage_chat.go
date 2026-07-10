package studio

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// genimage_chat.go：chat/completions 多模态回退路径的请求装配与响应解析。
// 模型未命中 images/:predict/:generateContent 任何专用路径时走这里（见 strategy.go）。

// generateViaChat 回退路径：OpenAI 兼容 chat/completions 多模态图像输出。
//
// 请求形态：messages 单条 user，content 为 parts —— text=prompt + 每张输入图 image_url；
// mask（inpaint 场景）作为附加 image_url 传递（chat 协议无独立掩码语义）。
func generateViaChat(ctx context.Context, core coreGateway, apiKey string, input map[string]any) (*generationResult, error) {
	model, _ := input["model"].(string)
	prompt, _ := input["prompt"].(string)

	parts := []map[string]any{
		{"type": "text", "text": prompt},
	}
	for _, img := range stringSliceFromAny(input["images"]) {
		parts = append(parts, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": img},
		})
	}
	if mask, ok := input["mask"].(string); ok && mask != "" {
		parts = append(parts, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": mask},
		})
	}

	payload := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{"role": "user", "content": parts},
		},
	}

	resp, err := core.ChatCompletions(ctx, apiKey, payload)
	if err != nil {
		return nil, err
	}

	images := extractImagesFromResponse(resp)
	if len(images) == 0 {
		return nil, fmt.Errorf("生成响应中未包含图片")
	}

	result := &generationResult{Images: images}
	if m, ok := resp["model"].(string); ok {
		result.Model = m
	}
	if u, ok := resp["usage"].(map[string]any); ok {
		result.Usage = u
	}
	return result, nil
}

// extractImagesFromResponse 从 chat/completions 响应提取图片 URL，兼容两种形态：
//  1. choices[0].message.images[].image_url.url（OpenRouter 风格多模态图像输出）；
//  2. choices[0].message.content 文本中的 markdown 图链 ![..](url) 及裸 data:image base64 链。
//
// 结果去重保序。
func extractImagesFromResponse(resp map[string]any) []string {
	choices, ok := resp["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return nil
	}
	message, ok := choice["message"].(map[string]any)
	if !ok {
		return nil
	}

	var out []string
	seen := make(map[string]bool)
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		out = append(out, u)
	}

	// 形态 1：message.images 数组
	if images, ok := message["images"].([]any); ok {
		for _, item := range images {
			switch v := item.(type) {
			case string:
				add(v)
			case map[string]any:
				if iu, ok := v["image_url"].(map[string]any); ok {
					if u, ok := iu["url"].(string); ok {
						add(u)
					}
				} else if u, ok := v["url"].(string); ok {
					add(u)
				}
			}
		}
	}

	// 形态 2：content 文本中的图链
	if content, ok := message["content"].(string); ok && content != "" {
		for _, u := range extractImageLinksFromText(content) {
			add(u)
		}
	}
	return out
}

var (
	// markdownImagePattern markdown 图片语法 ![alt](url)。
	markdownImagePattern = regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)\)`)
	// bareDataImagePattern 裸 data:image base64 链接。
	bareDataImagePattern = regexp.MustCompile(`data:image/[a-zA-Z0-9.+-]+;base64,[A-Za-z0-9+/=_-]+`)
)

// extractImageLinksFromText 从文本提取图片链接（markdown 图链优先，其次裸 data: 链）。
func extractImageLinksFromText(text string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, m := range markdownImagePattern.FindAllStringSubmatch(text, -1) {
		u := m[1]
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	// markdown 已覆盖的 data: 链不重复计
	stripped := markdownImagePattern.ReplaceAllString(text, "")
	for _, u := range bareDataImagePattern.FindAllString(stripped, -1) {
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}
