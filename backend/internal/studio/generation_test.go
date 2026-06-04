package studio

import "testing"

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
}

func TestBuildGenerationTaskResponseReturnsInputImages(t *testing.T) {
	task := &hostTask{
		ID:       12,
		Status:   "completed",
		Progress: 100,
		Input: map[string]interface{}{
			"prompt": "turn it into anime",
			"model":  "gpt-image-2",
			"images": []interface{}{
				"data:image/png;base64,source",
			},
			"mask": "data:image/png;base64,mask",
		},
		Attributes: map[string]interface{}{
			"operation": "edit",
			"size":      "1024x1024",
		},
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
}
