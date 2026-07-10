package studio

import (
	"context"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
)

// ==================== Imagen predict 参数映射 ====================

func TestAspectRatioFromSize(t *testing.T) {
	tests := []struct {
		size string
		want string
	}{
		{"", ""},
		{"auto", ""},
		// 直给支持的宽高比
		{"1:1", "1:1"},
		{"16:9", "16:9"},
		{"9:16", "9:16"},
		// WxH → 最近宽高比
		{"1024x1024", "1:1"},
		{"1536x1024", "4:3"}, // 1.5 与 4:3(1.33) 最近
		{"1024x1536", "3:4"},
		{"1792x1024", "16:9"},
		{"1024x1792", "9:16"},
		{"3840x2160", "16:9"},
		{"2160x3840", "9:16"},
		// 非支持集的 a:b → 就近
		{"2:1", "16:9"},
		{"1:2", "9:16"},
		// 不可解析
		{"banana", ""},
		{"0x100", ""},
		{"12", ""},
	}
	for _, tt := range tests {
		t.Run(tt.size, func(t *testing.T) {
			if got := aspectRatioFromSize(tt.size); got != tt.want {
				t.Fatalf("aspectRatioFromSize(%q) = %q, want %q", tt.size, got, tt.want)
			}
		})
	}
}

func TestBuildImagenPredictPayload(t *testing.T) {
	tests := []struct {
		name       string
		input      map[string]any
		wantParams map[string]any
	}{
		{
			name:       "默认 sampleCount=1",
			input:      map[string]any{"model": "imagen-4", "prompt": "a cat"},
			wantParams: map[string]any{"sampleCount": 1},
		},
		{
			name: "n→sampleCount / size→aspectRatio / quality→sampleImageSize",
			input: map[string]any{
				"model": "imagen-4", "prompt": "p",
				"n": float64(3), "size": "16:9", "quality": "high",
			},
			wantParams: map[string]any{"sampleCount": 3, "aspectRatio": "16:9", "sampleImageSize": "2K"},
		},
		{
			name: "WxH 尺寸就近映射 + 低档 quality",
			input: map[string]any{
				"model": "imagen-4", "prompt": "p",
				"size": "1024x1536", "quality": "standard",
			},
			wantParams: map[string]any{"sampleCount": 1, "aspectRatio": "3:4", "sampleImageSize": "1K"},
		},
		{
			name: "negative_prompt / seed 透传",
			input: map[string]any{
				"model": "imagen-4", "prompt": "p",
				"negative_prompt": "blurry", "seed": float64(42),
			},
			wantParams: map[string]any{"sampleCount": 1, "negativePrompt": "blurry", "seed": float64(42)},
		},
		{
			name: "auto 尺寸与空 quality 不下发",
			input: map[string]any{
				"model": "imagen-4", "prompt": "p", "size": "auto", "quality": "",
			},
			wantParams: map[string]any{"sampleCount": 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := buildImagenPredictPayload(tt.input)
			instances, ok := payload["instances"].([]map[string]any)
			if !ok || len(instances) != 1 || instances[0]["prompt"] != tt.input["prompt"] {
				t.Fatalf("instances = %#v", payload["instances"])
			}
			if got := payload["parameters"]; !reflect.DeepEqual(got, tt.wantParams) {
				t.Fatalf("parameters = %#v, want %#v", got, tt.wantParams)
			}
		})
	}
}

func TestImagenSampleImageSize(t *testing.T) {
	tests := []struct{ quality, want string }{
		{"", ""},
		{"hd", "2K"},
		{"high", "2K"},
		{"2k", "2K"},
		{"standard", "1K"},
		{"medium", "1K"},
		{"low", "1K"},
		{"auto", "1K"},
	}
	for _, tt := range tests {
		if got := imagenSampleImageSize(tt.quality); got != tt.want {
			t.Fatalf("imagenSampleImageSize(%q) = %q, want %q", tt.quality, got, tt.want)
		}
	}
}

// ==================== predict 响应解析 ====================

func TestImagesFromPredictResponse(t *testing.T) {
	tests := []struct {
		name string
		resp map[string]any
		want []string
	}{
		{
			name: "标准 predictions",
			resp: map[string]any{
				"predictions": []any{
					map[string]any{"bytesBase64Encoded": "QUJD", "mimeType": "image/png"},
					map[string]any{"bytesBase64Encoded": "REVG", "mimeType": "image/jpeg"},
				},
			},
			want: []string{"data:image/png;base64,QUJD", "data:image/jpeg;base64,REVG"},
		},
		{
			name: "缺 mimeType 回退 png",
			resp: map[string]any{
				"predictions": []any{map[string]any{"bytesBase64Encoded": "QUJD"}},
			},
			want: []string{"data:image/png;base64,QUJD"},
		},
		{
			name: "空 predictions",
			resp: map[string]any{"predictions": []any{}},
			want: nil,
		},
		{
			name: "缺 predictions",
			resp: map[string]any{},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := imagesFromPredictResponse(tt.resp); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestGenerateViaImagenPredict(t *testing.T) {
	core := &fakeCore{predictResp: predictResponse(tinyPNGBase64)}
	input := map[string]any{"model": "imagen-4", "prompt": "a cat", "n": float64(2), "size": "1:1"}
	result, err := generateViaImagenPredict(context.Background(), core, "sk-x", input)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(result.Images) != 1 || !strings.HasPrefix(result.Images[0], "data:image/png;base64,") {
		t.Fatalf("images = %#v", result.Images)
	}
	if result.Model != "imagen-4" {
		t.Fatalf("model = %s", result.Model)
	}
	if len(core.predictModels) != 1 || core.predictModels[0] != "imagen-4" {
		t.Fatalf("predict 调用模型 = %v", core.predictModels)
	}

	// 无图响应报错
	core = &fakeCore{predictResp: map[string]any{"predictions": []any{}}}
	if _, err := generateViaImagenPredict(context.Background(), core, "sk-x", input); err == nil || !strings.Contains(err.Error(), "未包含图片") {
		t.Fatalf("err = %v", err)
	}
}

// ==================== generateContent 请求构造 / 响应解析 ====================

func TestBuildGeminiContentPayload(t *testing.T) {
	png := tinyPNGDataURI()
	payload, err := buildGeminiContentPayload(context.Background(), map[string]any{
		"prompt": "repaint",
		"images": []any{png},
		"mask":   png,
	}, dataURILoader)
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	contents, ok := payload["contents"].([]map[string]any)
	if !ok || len(contents) != 1 || contents[0]["role"] != "user" {
		t.Fatalf("contents = %#v", payload["contents"])
	}
	parts := contents[0]["parts"].([]map[string]any)
	// text + 1 输入图 + 1 mask（作为附加图）
	if len(parts) != 3 {
		t.Fatalf("parts 数 = %d, want 3", len(parts))
	}
	if parts[0]["text"] != "repaint" {
		t.Fatalf("parts[0] = %#v", parts[0])
	}
	wantData, _ := base64.StdEncoding.DecodeString(tinyPNGBase64)
	for i := 1; i <= 2; i++ {
		inline, ok := parts[i]["inlineData"].(map[string]any)
		if !ok {
			t.Fatalf("parts[%d] 缺 inlineData: %#v", i, parts[i])
		}
		if inline["mimeType"] != "image/png" {
			t.Fatalf("parts[%d].mimeType = %v", i, inline["mimeType"])
		}
		gotData, _ := base64.StdEncoding.DecodeString(inline["data"].(string))
		if !reflect.DeepEqual(gotData, wantData) {
			t.Fatalf("parts[%d] 内容与输入图不一致", i)
		}
	}

	// 图像输出模态声明
	gc, ok := payload["generationConfig"].(map[string]any)
	if !ok || !reflect.DeepEqual(gc["responseModalities"], []string{"TEXT", "IMAGE"}) {
		t.Fatalf("generationConfig = %#v", payload["generationConfig"])
	}
}

func TestImagesFromGeminiResponse(t *testing.T) {
	tests := []struct {
		name string
		resp map[string]any
		want []string
	}{
		{
			name: "inlineData（camelCase）",
			resp: geminiContentResponse("QUJD"),
			want: []string{"data:image/png;base64,QUJD"},
		},
		{
			name: "inline_data（snake_case 兼容）",
			resp: map[string]any{
				"candidates": []any{map[string]any{
					"content": map[string]any{
						"parts": []any{map[string]any{
							"inline_data": map[string]any{"mime_type": "image/webp", "data": "QUJD"},
						}},
					},
				}},
			},
			want: []string{"data:image/webp;base64,QUJD"},
		},
		{
			name: "纯文本响应无图",
			resp: map[string]any{
				"candidates": []any{map[string]any{
					"content": map[string]any{
						"parts": []any{map[string]any{"text": "无法生成"}},
					},
				}},
			},
			want: nil,
		},
		{
			name: "空响应",
			resp: map[string]any{},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := imagesFromGeminiResponse(tt.resp); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestGenerateViaGeminiContent(t *testing.T) {
	core := &fakeCore{geminiResp: geminiContentResponse("QUJD")}
	input := map[string]any{"model": "gemini-2.5-flash-image", "prompt": "a cat"}
	result, err := generateViaGeminiContent(context.Background(), core, "sk-x", input, dataURILoader)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(result.Images) != 1 {
		t.Fatalf("images = %#v", result.Images)
	}
	// modelVersion 优先于请求模型名；usageMetadata 透传
	if result.Model != "gemini-2.5-flash-image-001" {
		t.Fatalf("model = %s", result.Model)
	}
	if result.Usage == nil {
		t.Fatalf("usage 应透传 usageMetadata")
	}
	if len(core.geminiModels) != 1 || core.geminiModels[0] != "gemini-2.5-flash-image" {
		t.Fatalf("generateContent 调用模型 = %v", core.geminiModels)
	}

	// 纯文本响应报错
	core = &fakeCore{geminiResp: map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{"parts": []any{map[string]any{"text": "拒绝"}}},
		}},
	}}
	if _, err := generateViaGeminiContent(context.Background(), core, "sk-x", input, dataURILoader); err == nil || !strings.Contains(err.Error(), "未包含图片") {
		t.Fatalf("err = %v", err)
	}
}
