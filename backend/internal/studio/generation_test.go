package studio

import (
	"testing"
	"time"
)

func TestBuildTaskInputKeepsEditImagesAndMask(t *testing.T) {
	req := createGenerationTaskRequest{
		Kind:      "image",
		Operation: "edit",
		Model:     "gpt-image-2",
		Prompt:    "change the jacket color",
		Parameters: map[string]interface{}{
			"size": "1024x1024",
		},
		Inputs: []generationInput{
			{Type: "image", Role: "source", URL: "data:image/png;base64,source"},
			{Type: "image", Role: "mask", URL: "data:image/png;base64,input-mask-is-ignored-here"},
		},
		Mask: &generationInput{Type: "image", Role: "mask", URL: "data:image/png;base64,mask"},
	}

	input := buildTaskInput(req)
	images, ok := input["images"].([]string)
	if !ok {
		t.Fatalf("images type = %T, want []string", input["images"])
	}
	if len(images) != 1 || images[0] != "data:image/png;base64,source" {
		t.Fatalf("images = %#v", images)
	}
	if got := input["mask"]; got != "data:image/png;base64,mask" {
		t.Fatalf("mask = %v", got)
	}
	if got := input["size"]; got != "1024x1024" {
		t.Fatalf("size = %v", got)
	}
	if got := input["preserve_reference"]; got != true {
		t.Fatalf("preserve_reference = %v, want true", got)
	}
	if got := input["prompt"]; got != "change the jacket color" {
		t.Fatalf("prompt = %v, want original prompt", got)
	}
	// 独立部署下 operation 记入 input（本地任务表没有 attributes 列）
	if got := input["operation"]; got != "edit" {
		t.Fatalf("operation = %v, want edit", got)
	}
}

func TestBuildGenerationTaskResponseReturnsInputImages(t *testing.T) {
	task := &Task{
		ID:       12,
		PublicID: "uuid-12",
		Status:   TaskStatusCompleted,
		Progress: 100,
		Input: map[string]interface{}{
			"prompt": "turn it into anime",
			"model":  "gpt-image-2",
			"images": []interface{}{
				"data:image/png;base64,source",
			},
			"mask":      "data:image/png;base64,mask",
			"operation": "edit",
			"size":      "1024x1024",
		},
		CreatedAt: time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC),
	}

	resp := buildGenerationTaskResponse(task)
	images, ok := resp["input_images"].([]string)
	if !ok {
		t.Fatalf("input_images type = %T, want []string", resp["input_images"])
	}
	if len(images) != 1 || images[0] != "data:image/png;base64,source" {
		t.Fatalf("input_images = %#v", images)
	}
	if got := resp["input_mask"]; got != "data:image/png;base64,mask" {
		t.Fatalf("input_mask = %v", got)
	}
	if got := resp["operation"]; got != "edit" {
		t.Fatalf("operation = %v, want edit", got)
	}
	if got := resp["size"]; got != "1024x1024" {
		t.Fatalf("size = %v, want 1024x1024", got)
	}
	if got := resp["created_at"]; got != "2026-07-01T08:00:00Z" {
		t.Fatalf("created_at = %v", got)
	}
}

func TestBuildGenerationTaskResponseMapsOutput(t *testing.T) {
	done := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	task := &Task{
		ID:       7,
		PublicID: "uuid-7",
		Status:   TaskStatusCompleted,
		Progress: 100,
		Input: map[string]interface{}{
			"prompt":    "a cat",
			"model":     "gpt-image-2",
			"operation": "generate",
		},
		Output: map[string]interface{}{
			"images": []interface{}{"/assets-runtime/generated/abc.png"},
			"model":  "gpt-image-2-0709",
			"usage":  map[string]interface{}{"total_tokens": float64(120)},
		},
		CreatedAt:   time.Date(2026, 7, 1, 8, 59, 0, 0, time.UTC),
		CompletedAt: &done,
	}

	resp := buildGenerationTaskResponse(task)
	images, ok := resp["images"].([]string)
	if !ok || len(images) != 1 || images[0] != "/assets-runtime/generated/abc.png" {
		t.Fatalf("images = %#v", resp["images"])
	}
	// 存量任务（output 无 assets）应从 images 派生统一产物形态
	assets, ok := resp["assets"].([]any)
	if !ok || len(assets) != 1 {
		t.Fatalf("assets = %#v", resp["assets"])
	}
	if a, _ := assets[0].(map[string]any); a["type"] != "image" || a["url"] != "/assets-runtime/generated/abc.png" {
		t.Fatalf("assets[0] = %#v", assets[0])
	}
	// output.model 优先于 input.model
	if got := resp["model"]; got != "gpt-image-2-0709" {
		t.Fatalf("model = %v", got)
	}
	if _, ok := resp["usage"]; !ok {
		t.Fatalf("usage 缺失")
	}
	if got := resp["completed_at"]; got != "2026-07-01T09:00:00Z" {
		t.Fatalf("completed_at = %v", got)
	}
}

func TestResolveTaskType(t *testing.T) {
	tests := []struct {
		kind, operation, want string
	}{
		{"image", "generate", "image.generate"},
		{"image", "edit", "image.edit"},
		{"image", "inpaint", "image.edit"},
		{"video", "generate", "video.generate"},
	}
	for _, tt := range tests {
		if got := resolveTaskType(tt.kind, tt.operation); got != tt.want {
			t.Errorf("resolveTaskType(%q, %q) = %q, want %q", tt.kind, tt.operation, got, tt.want)
		}
	}
}
