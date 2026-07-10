package studio

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ==================== Gemini 路径（:predict / :generateContent） ====================
//
// Imagen 系走 :predict（instances/parameters 形态，响应 predictions[] base64）；
// 其他 gemini 图像模型（gemini-2.5-flash-image 类）走 :generateContent 原生多模态。

// imagenAspectRatios Imagen predict 支持的宽高比集合。
var imagenAspectRatios = []string{"1:1", "3:4", "4:3", "9:16", "16:9"}

// aspectRatioFromSize 把 size（"WxH" 或 "a:b"）映射到 Imagen 支持的最近宽高比。
// "auto"/空/不可解析返回空串（不下发 aspectRatio，用上游默认）。
func aspectRatioFromSize(size string) string {
	size = strings.ToLower(strings.TrimSpace(size))
	if size == "" || size == "auto" {
		return ""
	}
	for _, ar := range imagenAspectRatios {
		if size == ar {
			return ar
		}
	}
	ratio := parseSizeRatio(size)
	if ratio <= 0 {
		return ""
	}
	best, bestDiff := "", math.MaxFloat64
	for _, ar := range imagenAspectRatios {
		if diff := math.Abs(parseSizeRatio(ar) - ratio); diff < bestDiff {
			best, bestDiff = ar, diff
		}
	}
	return best
}

// parseSizeRatio 解析 "WxH" 或 "a:b" 为宽高比值；不可解析返回 0。
func parseSizeRatio(size string) float64 {
	sep := ""
	switch {
	case strings.Contains(size, "x"):
		sep = "x"
	case strings.Contains(size, ":"):
		sep = ":"
	default:
		return 0
	}
	parts := strings.SplitN(size, sep, 2)
	w, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	h, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0
	}
	return w / h
}

// imagenSampleImageSize quality 档位 → Imagen sampleImageSize（1K/2K）；空 quality 不下发。
func imagenSampleImageSize(quality string) string {
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case "":
		return ""
	case "hd", "high", "2k":
		return "2K"
	default:
		return "1K"
	}
}

// buildImagenPredictPayload 组装 :predict 请求：
// instances[0].prompt + parameters（n→sampleCount、size→aspectRatio、
// quality→sampleImageSize、negative_prompt→negativePrompt、seed 透传）。
func buildImagenPredictPayload(input map[string]any) map[string]any {
	params := map[string]any{"sampleCount": 1}
	if n := intFromAny(input["n"]); n > 0 {
		params["sampleCount"] = n
	}
	if ar := aspectRatioFromSize(stringFromAny(input["size"])); ar != "" {
		params["aspectRatio"] = ar
	}
	if s := imagenSampleImageSize(stringFromAny(input["quality"])); s != "" {
		params["sampleImageSize"] = s
	}
	if np := stringFromAny(input["negative_prompt"]); np != "" {
		params["negativePrompt"] = np
	}
	if seed, ok := input["seed"]; ok && seed != nil {
		params["seed"] = seed
	}
	return map[string]any{
		"instances":  []map[string]any{{"prompt": input["prompt"]}},
		"parameters": params,
	}
}

// imagesFromPredictResponse predictions[].bytesBase64Encoded → data URI（MIME 缺省 png）。
func imagesFromPredictResponse(resp map[string]any) []string {
	preds, _ := resp["predictions"].([]any)
	var out []string
	for _, item := range preds {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		b64, _ := m["bytesBase64Encoded"].(string)
		if b64 == "" {
			continue
		}
		mime, _ := m["mimeType"].(string)
		if mime == "" {
			mime = "image/png"
		}
		out = append(out, "data:"+mime+";base64,"+b64)
	}
	return out
}

// generateViaImagenPredict Imagen 文生图：POST /v1beta/models/{model}:predict。
// Imagen predict 无图生图语义，任务携带的输入图忽略。
func generateViaImagenPredict(ctx context.Context, core coreGateway, apiKey string, input map[string]any) (*generationResult, error) {
	model := stringFromAny(input["model"])
	resp, err := core.GeminiPredict(ctx, apiKey, model, buildImagenPredictPayload(input))
	if err != nil {
		return nil, err
	}
	images := imagesFromPredictResponse(resp)
	if len(images) == 0 {
		return nil, fmt.Errorf("生成响应中未包含图片")
	}
	return &generationResult{Images: images, Model: model}, nil
}

// buildGeminiContentPayload 组装 :generateContent 请求：text + inlineData 图片 parts。
// mask 无原生语义，作为附加输入图传递（与 chat 路径语义一致）。
func buildGeminiContentPayload(ctx context.Context, input map[string]any, loadImage imageLoader) (map[string]any, error) {
	parts := []map[string]any{{"text": input["prompt"]}}
	srcs := stringSliceFromAny(input["images"])
	if mask := stringFromAny(input["mask"]); mask != "" {
		srcs = append(srcs, mask)
	}
	for _, src := range srcs {
		data, mime, err := loadImage(ctx, src)
		if err != nil {
			return nil, fmt.Errorf("读取输入图片失败: %w", err)
		}
		parts = append(parts, map[string]any{
			"inlineData": map[string]any{
				"mimeType": mimeFromExt(extFromMIME(mime)),
				"data":     base64.StdEncoding.EncodeToString(data),
			},
		})
	}
	return map[string]any{
		"contents": []map[string]any{{"role": "user", "parts": parts}},
		// 图像模型须显式声明图像输出模态
		"generationConfig": map[string]any{"responseModalities": []string{"TEXT", "IMAGE"}},
	}, nil
}

// imagesFromGeminiResponse candidates[].content.parts[].inlineData（兼容 inline_data）→ data URI。
func imagesFromGeminiResponse(resp map[string]any) []string {
	candidates, _ := resp["candidates"].([]any)
	var out []string
	for _, c := range candidates {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		content, ok := cm["content"].(map[string]any)
		if !ok {
			continue
		}
		parts, _ := content["parts"].([]any)
		for _, p := range parts {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			inline, ok := pm["inlineData"].(map[string]any)
			if !ok {
				inline, ok = pm["inline_data"].(map[string]any)
			}
			if !ok {
				continue
			}
			b64, _ := inline["data"].(string)
			if b64 == "" {
				continue
			}
			mime, _ := inline["mimeType"].(string)
			if mime == "" {
				mime, _ = inline["mime_type"].(string)
			}
			if mime == "" {
				mime = "image/png"
			}
			out = append(out, "data:"+mime+";base64,"+b64)
		}
	}
	return out
}

// generateViaGeminiContent Gemini 原生多模态生图：POST /v1beta/models/{model}:generateContent。
func generateViaGeminiContent(ctx context.Context, core coreGateway, apiKey string, input map[string]any, loadImage imageLoader) (*generationResult, error) {
	model := stringFromAny(input["model"])
	payload, err := buildGeminiContentPayload(ctx, input, loadImage)
	if err != nil {
		return nil, err
	}
	resp, err := core.GeminiGenerateContent(ctx, apiKey, model, payload)
	if err != nil {
		return nil, err
	}
	images := imagesFromGeminiResponse(resp)
	if len(images) == 0 {
		return nil, fmt.Errorf("生成响应中未包含图片")
	}
	result := &generationResult{Images: images, Model: model}
	if v, ok := resp["modelVersion"].(string); ok && v != "" {
		result.Model = v
	}
	if u, ok := resp["usageMetadata"].(map[string]any); ok {
		result.Usage = u
	}
	return result, nil
}
