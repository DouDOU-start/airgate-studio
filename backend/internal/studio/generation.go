package studio

import (
	"fmt"
	"strings"
	"time"
)

// createGenerationTaskRequest 创建生成任务的请求体（与原插件形态保持兼容，
// platform 字段前端仍会传，这里接受但不参与执行——独立部署下渠道由 core 调度）。
type createGenerationTaskRequest struct {
	Kind       string                 `json:"kind"`
	Operation  string                 `json:"operation"`
	Platform   string                 `json:"platform"`
	Model      string                 `json:"model"`
	Prompt     string                 `json:"prompt"`
	GroupID    int64                  `json:"group_id,omitempty"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	Inputs     []generationInput      `json:"inputs,omitempty"`
	Mask       *generationInput       `json:"mask,omitempty"`
}

type generationInput struct {
	Type string `json:"type"`
	Role string `json:"role"`
	URL  string `json:"url"`
}

func normalizeGenerationRequest(req *createGenerationTaskRequest) {
	req.Kind = strings.TrimSpace(req.Kind)
	if req.Kind == "" {
		req.Kind = "image"
	}
	req.Operation = strings.TrimSpace(req.Operation)
	if req.Operation == "" {
		req.Operation = "generate"
	}
}

func resolveTaskType(kind, operation string) string {
	switch kind {
	case "image":
		switch operation {
		case "edit", "inpaint":
			return "image.edit"
		default:
			return "image.generate"
		}
	default:
		return kind + "." + operation
	}
}

// buildTaskInput 组装任务 input（JSONB 落库）。
// 展示性元数据（operation/size/quality）一并放进 input——本地任务表没有独立
// attributes 列，worker 只读取 model/prompt/images/mask 这几个键，其余键对执行无副作用。
func buildTaskInput(req createGenerationTaskRequest) map[string]interface{} {
	input := map[string]interface{}{
		"prompt":    req.Prompt,
		"model":     req.Model,
		"operation": req.Operation,
	}
	if req.GroupID > 0 {
		input["group_id"] = req.GroupID
	}
	for key, value := range req.Parameters {
		if key == "" || value == nil {
			continue
		}
		if key == "model" || key == "prompt" || key == "operation" {
			continue
		}
		if s, ok := value.(string); ok && strings.TrimSpace(s) == "" {
			continue
		}
		input[key] = value
	}
	images := extractImageInputs(req.Inputs)
	if len(images) > 0 {
		input["images"] = images
		if req.Operation == "edit" || req.Operation == "inpaint" {
			input["preserve_reference"] = true
		}
	}
	if req.Mask != nil && req.Mask.URL != "" {
		input["mask"] = req.Mask.URL
	}
	return input
}

// buildGenerationTaskResponse 把本地任务映射为对前端的响应（保持原插件时代的字段形状：
// result_content 为 markdown 图链文本，前端画廊按此解析）。
func buildGenerationTaskResponse(task *Task) map[string]interface{} {
	resp := map[string]interface{}{
		"id":             task.ID,
		"task_id":        task.ID,
		"public_task_id": task.PublicID,
		"status":         task.Status,
		"progress":       task.Progress,
		"created_at":     formatTaskTime(task.CreatedAt),
	}
	if task.CompletedAt != nil {
		resp["completed_at"] = formatTaskTime(*task.CompletedAt)
	}
	if task.Input != nil {
		if v, ok := task.Input["prompt"]; ok {
			resp["prompt"] = v
		}
		if images := stringSliceFromAny(task.Input["images"]); len(images) > 0 {
			resp["input_images"] = images
		}
		if mask, ok := task.Input["mask"].(string); ok && mask != "" {
			resp["input_mask"] = mask
		}
	}
	if task.Output != nil {
		if content, ok := task.Output["content"].(string); ok && content != "" {
			resp["result_content"] = content
		}
		if images := stringSliceFromAny(task.Output["images"]); len(images) > 0 {
			resp["images"] = images
		}
		if model, ok := task.Output["model"]; ok {
			resp["model"] = model
		}
		if usage, ok := task.Output["usage"]; ok {
			resp["usage"] = usage
		}
	}
	if task.ErrorMessage != "" {
		resp["error_message"] = task.ErrorMessage
	}
	// 从 input 补充展示字段
	if _, ok := resp["model"]; !ok {
		if v, ok := task.Input["model"]; ok {
			resp["model"] = v
		}
	}
	for _, key := range []string{"size", "quality", "operation"} {
		if v, ok := task.Input[key]; ok && fmt.Sprint(v) != "" {
			resp[key] = v
		}
	}
	return resp
}

// formatTaskTime 时间统一 RFC3339（与原 host 任务响应一致，前端直接 new Date 解析）。
func formatTaskTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func extractImageInputs(inputs []generationInput) []string {
	var images []string
	for _, input := range inputs {
		if input.URL == "" {
			continue
		}
		if input.Type != "" && input.Type != "image" {
			continue
		}
		if input.Role == "mask" {
			continue
		}
		images = append(images, input.URL)
	}
	return images
}

// stringFromAny 从 any 提取 string 并去首尾空白；非 string 返回空。
func stringFromAny(value interface{}) string {
	s, _ := value.(string)
	return strings.TrimSpace(s)
}

// intFromAny 从 any 提取整数（JSONB 落库后数字是 float64）；不可解析返回 0。
func intFromAny(value interface{}) int {
	switch n := value.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func stringSliceFromAny(value interface{}) []string {
	var out []string
	switch v := value.(type) {
	case []string:
		out = append(out, v...)
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
	case string:
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}
