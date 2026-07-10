package studio

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// ==================== generations 请求构造 ====================

func TestBuildImagesGenerationsPayload(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		want  map[string]any
	}{
		{
			name:  "仅必填",
			input: map[string]any{"model": "gpt-image-1", "prompt": "a cat"},
			want:  map[string]any{"model": "gpt-image-1", "prompt": "a cat"},
		},
		{
			name: "白名单参数透传",
			input: map[string]any{
				"model": "gpt-image-1", "prompt": "p",
				"n": float64(2), "size": "1536x1024", "quality": "high",
				"background": "transparent", "output_format": "webp",
			},
			want: map[string]any{
				"model": "gpt-image-1", "prompt": "p",
				"n": float64(2), "size": "1536x1024", "quality": "high",
				"background": "transparent", "output_format": "webp",
			},
		},
		{
			name: "空串与非白名单键剔除",
			input: map[string]any{
				"model": "gpt-image-1", "prompt": "p",
				"size": "  ", "quality": "",
				"operation": "generate", "group_id": float64(3), "images": []any{"x"},
			},
			want: map[string]any{"model": "gpt-image-1", "prompt": "p"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildImagesGenerationsPayload(tt.input); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("payload = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestBuildImagesEditsFields(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		want  map[string]string
	}{
		{
			name:  "仅必填",
			input: map[string]any{"model": "gpt-image-1", "prompt": "p"},
			want:  map[string]string{"model": "gpt-image-1", "prompt": "p"},
		},
		{
			name: "数字参数转字符串",
			input: map[string]any{
				"model": "gpt-image-1", "prompt": "p",
				"n": float64(2), "size": "auto", "quality": "medium",
			},
			want: map[string]string{
				"model": "gpt-image-1", "prompt": "p",
				"n": "2", "size": "auto", "quality": "medium",
			},
		},
		{
			name: "非白名单键剔除",
			input: map[string]any{
				"model": "m", "prompt": "p",
				"images": []any{"x"}, "mask": "y", "operation": "edit",
			},
			want: map[string]string{"model": "m", "prompt": "p"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildImagesEditsFields(tt.input); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("fields = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// ==================== edits multipart 文件构造 ====================

func TestBuildImagesEditsFiles(t *testing.T) {
	png := tinyPNGDataURI()
	jpegURI := "data:image/jpeg;base64,aXNqcGVn" // "isjpeg"

	t.Run("单图字段名 image", func(t *testing.T) {
		files, err := buildImagesEditsFiles(context.Background(),
			map[string]any{"images": []any{png}}, dataURILoader)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(files) != 1 || files[0].Field != "image" {
			t.Fatalf("files = %+v", files)
		}
		if files[0].MIME != "image/png" || !strings.HasSuffix(files[0].Name, ".png") {
			t.Fatalf("part 元数据 = %+v", files[0])
		}
		if len(files[0].Data) == 0 {
			t.Fatalf("文件内容为空")
		}
	})

	t.Run("多图字段名 image[] 且保序", func(t *testing.T) {
		files, err := buildImagesEditsFiles(context.Background(),
			map[string]any{"images": []any{png, jpegURI}}, dataURILoader)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(files) != 2 {
			t.Fatalf("part 数 = %d", len(files))
		}
		for i, f := range files {
			if f.Field != "image[]" {
				t.Fatalf("files[%d].Field = %s", i, f.Field)
			}
		}
		if files[1].MIME != "image/jpeg" || !strings.HasSuffix(files[1].Name, ".jpg") {
			t.Fatalf("jpeg part = %+v", files[1])
		}
	})

	t.Run("mask 独立字段", func(t *testing.T) {
		files, err := buildImagesEditsFiles(context.Background(),
			map[string]any{"images": []any{png}, "mask": png}, dataURILoader)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(files) != 2 || files[0].Field != "image" || files[1].Field != "mask" {
			t.Fatalf("files = %+v", files)
		}
	})

	t.Run("无输入图报错", func(t *testing.T) {
		if _, err := buildImagesEditsFiles(context.Background(), map[string]any{}, dataURILoader); err == nil {
			t.Fatalf("应返回错误")
		}
	})

	t.Run("读图失败报错", func(t *testing.T) {
		failLoader := func(context.Context, string) ([]byte, string, error) {
			return nil, "", fmt.Errorf("boom")
		}
		if _, err := buildImagesEditsFiles(context.Background(),
			map[string]any{"images": []any{png}}, failLoader); err == nil {
			t.Fatalf("应返回错误")
		}
	})
}

// ==================== 响应归一 ====================

func TestNormalizeImagesResponse(t *testing.T) {
	tests := []struct {
		name       string
		resp       map[string]any
		mime       string
		wantImages []string
		wantUsage  bool
		wantErr    bool
	}{
		{
			name: "b64_json → data URI",
			resp: map[string]any{
				"data":  []any{map[string]any{"b64_json": "QUJD"}},
				"usage": map[string]any{"total_tokens": float64(3)},
			},
			mime:       "image/png",
			wantImages: []string{"data:image/png;base64,QUJD"},
			wantUsage:  true,
		},
		{
			name: "url 原样保留",
			resp: map[string]any{
				"data": []any{map[string]any{"url": "https://cdn.example.com/a.png"}},
			},
			mime:       "image/png",
			wantImages: []string{"https://cdn.example.com/a.png"},
		},
		{
			name: "b64 与 url 混合保序",
			resp: map[string]any{
				"data": []any{
					map[string]any{"b64_json": "QUJD"},
					map[string]any{"url": "https://x/b.png"},
				},
			},
			mime:       "image/webp",
			wantImages: []string{"data:image/webp;base64,QUJD", "https://x/b.png"},
		},
		{
			name:    "data 为空报错",
			resp:    map[string]any{"data": []any{}},
			mime:    "image/png",
			wantErr: true,
		},
		{
			name:    "缺 data 报错",
			resp:    map[string]any{"created": float64(1)},
			mime:    "image/png",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeImagesResponse(tt.resp, tt.mime)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("应返回错误")
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !reflect.DeepEqual(got.Images, tt.wantImages) {
				t.Fatalf("images = %#v, want %#v", got.Images, tt.wantImages)
			}
			if tt.wantUsage && got.Usage == nil {
				t.Fatalf("usage 应透传")
			}
		})
	}
}

func TestImagesOutputMIME(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"", "image/png"},
		{"png", "image/png"},
		{"jpeg", "image/jpeg"},
		{"jpg", "image/jpeg"},
		{"WEBP", "image/webp"},
		{"unknown", "image/png"},
	}
	for _, tt := range tests {
		input := map[string]any{}
		if tt.format != "" {
			input["output_format"] = tt.format
		}
		if got := imagesOutputMIME(input); got != tt.want {
			t.Fatalf("imagesOutputMIME(%q) = %s, want %s", tt.format, got, tt.want)
		}
	}
}
