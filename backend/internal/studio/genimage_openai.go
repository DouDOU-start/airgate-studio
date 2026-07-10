package studio

import (
	"context"
	"fmt"
	"strings"
)

// ==================== OpenAI Images 路径（/v1/images/generations|edits） ====================
//
// gpt-image*/dall-e* 系模型的原生生图端点：size/quality 等参数原生生效。
// edits 为 multipart 直传文件；core 对 multipart 原样透传（渠道 model_mapping/
// param_override 不生效），故模型名必须是上游可识别的原名。

// imagesGenerationsParamKeys /v1/images/generations 允许从任务 input 透传的可选参数。
var imagesGenerationsParamKeys = []string{
	"n", "size", "quality", "background",
	"output_format", "output_compression", "style", "moderation",
}

// buildImagesGenerationsPayload 组装 generations 请求体：model/prompt + 白名单参数透传。
func buildImagesGenerationsPayload(input map[string]any) map[string]any {
	payload := map[string]any{
		"model":  input["model"],
		"prompt": input["prompt"],
	}
	for _, key := range imagesGenerationsParamKeys {
		v, ok := input[key]
		if !ok || v == nil {
			continue
		}
		if s, isStr := v.(string); isStr && strings.TrimSpace(s) == "" {
			continue
		}
		payload[key] = v
	}
	return payload
}

// imagesEditsFieldKeys /v1/images/edits 允许透传的表单字段（multipart 字段一律字符串）。
var imagesEditsFieldKeys = []string{
	"n", "size", "quality", "background", "output_format", "output_compression",
}

// buildImagesEditsFields 组装 edits 的普通表单字段。
func buildImagesEditsFields(input map[string]any) map[string]string {
	fields := map[string]string{
		"model":  stringFromAny(input["model"]),
		"prompt": stringFromAny(input["prompt"]),
	}
	for _, key := range imagesEditsFieldKeys {
		v, ok := input[key]
		if !ok || v == nil {
			continue
		}
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" {
			continue
		}
		fields[key] = s
	}
	return fields
}

// imagesOutputMIME b64_json 响应不带 MIME，按请求的 output_format 推断（默认 png）。
func imagesOutputMIME(input map[string]any) string {
	switch strings.ToLower(stringFromAny(input["output_format"])) {
	case "jpeg", "jpg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

// normalizeImagesResponse 把 /v1/images/* 标准响应归一为 generationResult：
// data[].b64_json → data URI（MIME 按请求 output_format），data[].url 原样保留；
// token usage 原样透传。
func normalizeImagesResponse(resp map[string]any, mime string) (*generationResult, error) {
	data, _ := resp["data"].([]any)
	var images []string
	for _, item := range data {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if b64, ok := m["b64_json"].(string); ok && b64 != "" {
			images = append(images, "data:"+mime+";base64,"+b64)
			continue
		}
		if u, ok := m["url"].(string); ok && u != "" {
			images = append(images, u)
		}
	}
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

// generateViaImagesGenerations 文生图：POST /v1/images/generations。
func generateViaImagesGenerations(ctx context.Context, core coreGateway, apiKey string, input map[string]any) (*generationResult, error) {
	resp, err := core.ImagesGenerations(ctx, apiKey, buildImagesGenerationsPayload(input))
	if err != nil {
		return nil, err
	}
	return normalizeImagesResponse(resp, imagesOutputMIME(input))
}

// buildImagesEditsFiles 把任务 input 的输入图/掩码读成 multipart 文件 part。
//
// 单图字段名 image、多图 image[]（OpenAI 对 gpt-image 系的数组约定）；mask 独立字段。
func buildImagesEditsFiles(ctx context.Context, input map[string]any, loadImage imageLoader) ([]filePart, error) {
	srcs := stringSliceFromAny(input["images"])
	if len(srcs) == 0 {
		return nil, fmt.Errorf("图生图任务缺少输入图片")
	}
	imageField := "image"
	if len(srcs) > 1 {
		imageField = "image[]"
	}
	files := make([]filePart, 0, len(srcs)+1)
	for i, src := range srcs {
		data, mime, err := loadImage(ctx, src)
		if err != nil {
			return nil, fmt.Errorf("读取输入图片失败: %w", err)
		}
		ext := extFromMIME(mime)
		files = append(files, filePart{
			Field: imageField,
			Name:  fmt.Sprintf("image-%d%s", i, ext),
			MIME:  mimeFromExt(ext),
			Data:  data,
		})
	}
	if mask := stringFromAny(input["mask"]); mask != "" {
		data, mime, err := loadImage(ctx, mask)
		if err != nil {
			return nil, fmt.Errorf("读取掩码图片失败: %w", err)
		}
		ext := extFromMIME(mime)
		files = append(files, filePart{Field: "mask", Name: "mask" + ext, MIME: mimeFromExt(ext), Data: data})
	}
	return files, nil
}

// generateViaImagesEdits 图生图：POST /v1/images/edits（multipart）。
func generateViaImagesEdits(ctx context.Context, core coreGateway, apiKey string, input map[string]any, loadImage imageLoader) (*generationResult, error) {
	files, err := buildImagesEditsFiles(ctx, input, loadImage)
	if err != nil {
		return nil, err
	}
	resp, err := core.ImagesEdits(ctx, apiKey, buildImagesEditsFields(input), files)
	if err != nil {
		return nil, err
	}
	return normalizeImagesResponse(resp, imagesOutputMIME(input))
}
