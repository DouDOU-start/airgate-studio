package studio

import (
	"fmt"
	"strings"
	"time"
)

// createGenerationTaskRequest 创建生成任务的请求体。
// 渠道由 core 按 key 所属分组调度，请求里只有「分组 + 模型」两个选择维度。
type createGenerationTaskRequest struct {
	Kind       string                 `json:"kind"`
	Operation  string                 `json:"operation"`
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

// supportedKinds 当前支持的生成模态白名单。
// 扩展口：video / music 等新模态在 core 异步链路就绪后加入此表，
// 并在 worker.executeTask 的模态分发处补对应执行分支（见 CLAUDE.md「红线」）。
var supportedKinds = map[string]bool{
	"image": true,
}

// taskKind 从 task_type（"<kind>.<operation>"）取模态部分。
func taskKind(taskType string) string {
	if i := strings.Index(taskType, "."); i >= 0 {
		return taskType[:i]
	}
	return taskType
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

// buildGenerationTaskResponse 把本地任务映射为对前端的响应。
// 产物以 images（图片 URL 数组）+ assets（统一形态 [{type,url}]）下发。
func buildGenerationTaskResponse(task *Task) map[string]interface{} {
	resp := map[string]interface{}{
		"id":         task.ID,
		"status":     task.Status,
		"progress":   task.Progress,
		"created_at": formatTaskTime(task.CreatedAt),
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
		images := stringSliceFromAny(task.Output["images"])
		if len(images) > 0 {
			resp["images"] = images
		}
		// 产物统一形态：output 自带 assets（新模态直接写入）优先，
		// 否则从 images 派生（覆盖 assets 字段引入前的存量任务）。
		if assets, ok := task.Output["assets"]; ok {
			resp["assets"] = assets
		} else if len(images) > 0 {
			resp["assets"] = imageAssets(images)
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

// imageAssets 把图片 URL 列表包装为统一产物形态 [{type:"image", url}]。
// 视频/音乐等模态的 executor 落库时直接写 output.assets（type 对应 video/audio）。
// 返回 []any 与 JSONB 反序列化后的形态一致，消费方（outputAssetURLs 等）无需分支。
func imageAssets(urls []string) []any {
	out := make([]any, 0, len(urls))
	for _, u := range urls {
		out = append(out, map[string]any{"type": "image", "url": u})
	}
	return out
}

// outputAssetURLs 收集任务 output 里全部产物 URL（images 与 assets[].url 去重合并），
// 供删除任务时清理本地文件——新模态只要把产物写进 output.assets 即自动纳入清理。
func outputAssetURLs(output map[string]any) []string {
	if output == nil {
		return nil
	}
	seen := map[string]bool{}
	var urls []string
	add := func(u string) {
		if u != "" && !seen[u] {
			seen[u] = true
			urls = append(urls, u)
		}
	}
	for _, u := range stringSliceFromAny(output["images"]) {
		add(u)
	}
	if assets, ok := output["assets"].([]any); ok {
		for _, item := range assets {
			if m, ok := item.(map[string]any); ok {
				add(stringFromAny(m["url"]))
			}
		}
	}
	return urls
}

// formatTaskTime 时间统一 RFC3339（前端直接 new Date 解析）。
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
