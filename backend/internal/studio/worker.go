package studio

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

// coreGateway 抽象 core 的生成相关调用，便于单测注入假实现。
// core 零翻译纯透传，多协议模型须打到各自原生端点（策略判定见 strategy.go）。
type coreGateway interface {
	ChatCompletions(ctx context.Context, apiKey string, payload map[string]any) (map[string]any, error)
	ImagesGenerations(ctx context.Context, apiKey string, payload map[string]any) (map[string]any, error)
	ImagesEdits(ctx context.Context, apiKey string, fields map[string]string, files []filePart) (map[string]any, error)
	GeminiGenerateContent(ctx context.Context, apiKey, model string, payload map[string]any) (map[string]any, error)
	GeminiPredict(ctx context.Context, apiKey, model string, payload map[string]any) (map[string]any, error)
	ModelProtocols(ctx context.Context, apiKey string) (map[string][]string, error)
	FetchBinary(ctx context.Context, rawURL string, maxBytes int64) ([]byte, string, error)
}

// imageLoader 读取一张输入图（data URI / 本地资产 / 远程 URL）为字节 + MIME 的回调。
type imageLoader func(ctx context.Context, imgURL string) (data []byte, mime string, err error)

// Worker 单 goroutine 轮询 studio_tasks 并执行图像生成任务。
//
// 领取语义由 TaskStore.ClaimNext 保证（FOR UPDATE SKIP LOCKED），
// 单实例部署下同时只有一个任务在执行；失败经 TaskStore.Fail 走重试状态机。
type Worker struct {
	tasks  TaskStore
	users  UserStore
	assets *AssetStore
	core   coreGateway
	logger *slog.Logger
	// catalog core 模型目录（model→protocols）TTL 缓存，供执行策略判定。
	catalog *protocolCatalog

	// pollInterval 空闲轮询间隔。
	pollInterval time.Duration
	// execTimeout 单次生成调用的超时上限（图像生成可能长达数分钟）。
	execTimeout time.Duration
	// maxDownloadBytes 远程图片落地时的单张大小上限。
	maxDownloadBytes int64
}

// NewWorker 创建任务执行器。
func NewWorker(tasks TaskStore, users UserStore, assets *AssetStore, core coreGateway, logger *slog.Logger) *Worker {
	return &Worker{
		tasks:            tasks,
		users:            users,
		assets:           assets,
		core:             core,
		logger:           logger,
		catalog:          newProtocolCatalog(core, defaultProtocolCatalogTTL),
		pollInterval:     2 * time.Second,
		execTimeout:      10 * time.Minute,
		maxDownloadBytes: 64 << 20,
	}
}

// Run 阻塞运行直到 ctx 取消。启动时先把遗留 processing 任务重置回 pending。
func (w *Worker) Run(ctx context.Context) {
	if n, err := w.tasks.ResetProcessing(ctx); err != nil {
		w.logger.Error("reset_processing_failed", "error", err)
	} else if n > 0 {
		w.logger.Info("reset_processing_tasks", "count", n)
	}

	for {
		claimed := w.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if claimed {
			// 队列非空时立刻继续领取，不等轮询间隔。
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.pollInterval):
		}
	}
}

// runOnce 领取并执行一个任务；返回是否领取到任务。
func (w *Worker) runOnce(ctx context.Context) bool {
	task, err := w.tasks.ClaimNext(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.Error("claim_task_failed", "error", err)
		}
		return false
	}
	if task == nil {
		return false
	}

	w.logger.Info("task_started", "task_id", task.ID, "task_type", task.TaskType, "attempt", task.Attempts+1)
	output, err := w.executeTask(ctx, task)
	if err != nil {
		w.logger.Warn("task_failed", "task_id", task.ID, "error", err)
		if failErr := w.tasks.Fail(ctx, task.ID, err.Error()); failErr != nil {
			w.logger.Error("mark_task_failed_error", "task_id", task.ID, "error", failErr)
		}
		return true
	}
	if err := w.tasks.Complete(ctx, task.ID, output); err != nil {
		w.logger.Error("mark_task_completed_error", "task_id", task.ID, "error", err)
		return true
	}
	w.logger.Info("task_completed", "task_id", task.ID)
	return true
}

// executeTask 执行单个生成任务：按任务分组取用户 key → 调生成 → 图片落地 → 组装 output。
func (w *Worker) executeTask(ctx context.Context, task *Task) (map[string]any, error) {
	user, err := w.users.GetByID(ctx, task.UserID)
	if err != nil {
		return nil, fmt.Errorf("查询任务所属用户失败: %w", err)
	}
	apiKey, err := w.resolveTaskKey(ctx, user, task)
	if err != nil {
		return nil, err
	}

	execCtx, cancel := context.WithTimeout(ctx, w.execTimeout)
	defer cancel()

	// 模型协议来自 core /v1/models 目录缓存（取不到按模型名启发式兜底）
	protocols := w.catalog.ProtocolsFor(execCtx, apiKey, stringFromAny(task.Input["model"]))
	result, err := generateImages(execCtx, w.core, apiKey, task.Input, protocols, w.loadInputImage)
	if err != nil {
		return nil, err
	}

	// 图片落地本地资产存储：data URI 解码写盘；远程 URL 下载后写盘（上游 URL 会过期）。
	// 单张落地失败不整体报废任务，保留原始 URL 兜底。
	urls := make([]string, 0, len(result.Images))
	for _, img := range result.Images {
		localURL, err := w.localizeImage(execCtx, img)
		if err != nil {
			w.logger.Warn("localize_image_failed", "task_id", task.ID, "error", err)
			localURL = img
		}
		urls = append(urls, localURL)
	}

	output := map[string]any{
		"images":  urls,
		"content": imagesMarkdown(urls),
	}
	if result.Model != "" {
		output["model"] = result.Model
	}
	if result.Usage != nil {
		output["usage"] = result.Usage
	}
	return output, nil
}

// resolveTaskKey 选定执行任务用的 sk- key：
// 任务带 group_id → 用该分组的按组 key（缺失提示重新登录）；
// 未带 → 默认 key（零配置兼容模式），兜底取用户任意一把按组 key。
func (w *Worker) resolveTaskKey(ctx context.Context, user *User, task *Task) (string, error) {
	groupID := int64(intFromAny(task.Input["group_id"]))
	if groupID > 0 {
		keys, err := w.users.KeysByUser(ctx, user.ID)
		if err != nil {
			return "", fmt.Errorf("查询用户分组 key 失败: %w", err)
		}
		if key := keys[groupID]; key != "" {
			return key, nil
		}
		return "", fmt.Errorf("尚未启用该分组（请重新登录以领取该分组的 API Key）")
	}
	if user.APIKey != "" {
		return user.APIKey, nil
	}
	keys, err := w.users.KeysByUser(ctx, user.ID)
	if err == nil {
		for _, key := range keys {
			if key != "" {
				return key, nil
			}
		}
	}
	return "", fmt.Errorf("没有可用的 API Key（可能在 AirGate 中被禁用），请重新登录")
}

// localizeImage 把一张生成图片（data URI 或远程 URL）落地到本地资产存储，返回公开 URL。
func (w *Worker) localizeImage(ctx context.Context, imgURL string) (string, error) {
	if strings.HasPrefix(imgURL, assetURLPrefix) {
		// 已经是本地资产（理论上不会发生），原样保留。
		return imgURL, nil
	}
	if strings.HasPrefix(imgURL, "data:") {
		mime, data, err := parseDataURI(imgURL)
		if err != nil {
			return "", err
		}
		return w.assets.SaveGenerated(data, extFromMIME(mime))
	}
	if strings.HasPrefix(imgURL, "http://") || strings.HasPrefix(imgURL, "https://") {
		data, contentType, err := w.core.FetchBinary(ctx, imgURL, w.maxDownloadBytes)
		if err != nil {
			return "", fmt.Errorf("下载远程图片失败: %w", err)
		}
		return w.assets.SaveGenerated(data, extFromMIME(contentType))
	}
	return "", fmt.Errorf("无法识别的图片 URL 形态: %.32s", imgURL)
}

// loadInputImage 读取一张任务输入图为字节 + MIME（imageLoader 实现）：
// data URI 直接解码；本地资产从磁盘读；远程 URL 下载（受大小上限约束）。
func (w *Worker) loadInputImage(ctx context.Context, imgURL string) ([]byte, string, error) {
	switch {
	case strings.HasPrefix(imgURL, "data:"):
		mime, data, err := parseDataURI(imgURL)
		return data, mime, err
	case strings.HasPrefix(imgURL, assetURLPrefix):
		return w.assets.ReadGenerated(imgURL)
	case strings.HasPrefix(imgURL, "http://"), strings.HasPrefix(imgURL, "https://"):
		return w.core.FetchBinary(ctx, imgURL, w.maxDownloadBytes)
	default:
		return nil, "", fmt.Errorf("无法识别的输入图片形态: %.32s", imgURL)
	}
}

// ==================== 生成调用（协议装配 + 响应提取） ====================

// generationResult 一次生成调用的解析结果。
type generationResult struct {
	Images []string // 提取出的图片（data URI 或远程 URL），已去重保序
	Model  string
	Usage  map[string]any
}

// generateImages 一次图像生成的统一切换点：按模型协议 + 模型名 + 是否携带输入图
// 选择执行路径（判定纯函数见 strategy.go，各路径实现见 genimage_*.go）。
//
//	imagen* × gemini      → /v1beta/models/{model}:predict
//	其他 gemini 图像模型   → /v1beta/models/{model}:generateContent
//	gpt-image*/dall-e* × openai → /v1/images/generations（无输入图）| /v1/images/edits（有输入图）
//	其余                  → /v1/chat/completions 多模态（原路径）
func generateImages(ctx context.Context, core coreGateway, apiKey string, input map[string]any, protocols []string, loadImage imageLoader) (*generationResult, error) {
	model, _ := input["model"].(string)
	prompt, _ := input["prompt"].(string)
	if model == "" {
		return nil, fmt.Errorf("任务输入缺少 model")
	}
	if prompt == "" {
		return nil, fmt.Errorf("任务输入缺少 prompt")
	}

	hasInputImages := len(stringSliceFromAny(input["images"])) > 0
	switch resolveStrategy(model, protocols, hasInputImages) {
	case strategyImagenPredict:
		return generateViaImagenPredict(ctx, core, apiKey, input)
	case strategyGeminiContent:
		return generateViaGeminiContent(ctx, core, apiKey, input, loadImage)
	case strategyImagesGenerations:
		return generateViaImagesGenerations(ctx, core, apiKey, input)
	case strategyImagesEdits:
		return generateViaImagesEdits(ctx, core, apiKey, input, loadImage)
	default:
		return generateViaChat(ctx, core, apiKey, input)
	}
}

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

// parseDataURI 解析 data:<mime>;base64,<payload> 形态的 data URI。
func parseDataURI(uri string) (mime string, data []byte, err error) {
	if !strings.HasPrefix(uri, "data:") {
		return "", nil, fmt.Errorf("非 data URI")
	}
	rest := strings.TrimPrefix(uri, "data:")
	comma := strings.Index(rest, ",")
	if comma < 0 {
		return "", nil, fmt.Errorf("data URI 缺少逗号分隔符")
	}
	meta, payload := rest[:comma], rest[comma+1:]
	if !strings.HasSuffix(meta, ";base64") {
		return "", nil, fmt.Errorf("仅支持 base64 编码的 data URI")
	}
	mime = strings.TrimSuffix(meta, ";base64")
	data, err = base64.StdEncoding.DecodeString(payload)
	if err != nil {
		// 兼容 URL-safe base64
		data, err = base64.URLEncoding.DecodeString(payload)
		if err != nil {
			return "", nil, fmt.Errorf("解码 data URI 失败: %w", err)
		}
	}
	return mime, data, nil
}

// imagesMarkdown 把图片 URL 列表渲染成 markdown（前端画廊按 markdown 图链解析）。
func imagesMarkdown(urls []string) string {
	lines := make([]string, 0, len(urls))
	for _, u := range urls {
		lines = append(lines, fmt.Sprintf("![generated image](%s)", u))
	}
	return strings.Join(lines, "\n\n")
}
