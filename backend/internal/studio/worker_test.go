package studio

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// 1x1 透明 PNG（最小合法图片数据）。
const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func tinyPNGDataURI() string {
	return "data:image/png;base64," + tinyPNGBase64
}

// fakeCore coreGateway 的假实现：各端点按脚本返回响应或错误，并分别记录请求。
type fakeCore struct {
	// chat/completions
	resp     map[string]any
	err      error
	requests []map[string]any
	// /v1/images/generations
	imagesResp     map[string]any
	imagesErr      error
	imagesRequests []map[string]any
	// /v1/images/edits
	editsResp   map[string]any
	editsErr    error
	editsFields []map[string]string
	editsFiles  [][]filePart
	// :generateContent
	geminiResp     map[string]any
	geminiErr      error
	geminiModels   []string
	geminiRequests []map[string]any
	// :predict
	predictResp     map[string]any
	predictErr      error
	predictModels   []string
	predictRequests []map[string]any
	// /v1/models 目录
	protocols      map[string][]string
	protocolsErr   error
	protocolsCalls int
	// fetch 结果（远程 URL 落地用）
	fetchData []byte
	fetchMIME string
	fetchErr  error
}

func (f *fakeCore) ChatCompletions(_ context.Context, _ string, payload map[string]any) (map[string]any, error) {
	f.requests = append(f.requests, payload)
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func (f *fakeCore) ImagesGenerations(_ context.Context, _ string, payload map[string]any) (map[string]any, error) {
	f.imagesRequests = append(f.imagesRequests, payload)
	if f.imagesErr != nil {
		return nil, f.imagesErr
	}
	return f.imagesResp, nil
}

func (f *fakeCore) ImagesEdits(_ context.Context, _ string, fields map[string]string, files []filePart) (map[string]any, error) {
	f.editsFields = append(f.editsFields, fields)
	f.editsFiles = append(f.editsFiles, files)
	if f.editsErr != nil {
		return nil, f.editsErr
	}
	return f.editsResp, nil
}

func (f *fakeCore) GeminiGenerateContent(_ context.Context, _, model string, payload map[string]any) (map[string]any, error) {
	f.geminiModels = append(f.geminiModels, model)
	f.geminiRequests = append(f.geminiRequests, payload)
	if f.geminiErr != nil {
		return nil, f.geminiErr
	}
	return f.geminiResp, nil
}

func (f *fakeCore) GeminiPredict(_ context.Context, _, model string, payload map[string]any) (map[string]any, error) {
	f.predictModels = append(f.predictModels, model)
	f.predictRequests = append(f.predictRequests, payload)
	if f.predictErr != nil {
		return nil, f.predictErr
	}
	return f.predictResp, nil
}

func (f *fakeCore) ModelProtocols(_ context.Context, _ string) (map[string][]string, error) {
	f.protocolsCalls++
	if f.protocolsErr != nil {
		return nil, f.protocolsErr
	}
	return f.protocols, nil
}

func (f *fakeCore) FetchBinary(_ context.Context, _ string, _ int64) ([]byte, string, error) {
	if f.fetchErr != nil {
		return nil, "", f.fetchErr
	}
	return f.fetchData, f.fetchMIME, nil
}

// dataURILoader 直接解 data URI 的 imageLoader（纯函数测试用）。
func dataURILoader(_ context.Context, imgURL string) ([]byte, string, error) {
	mime, data, err := parseDataURI(imgURL)
	return data, mime, err
}

// chatResponse 构造 message.images 形态的响应。
func chatResponseWithImages(urls ...string) map[string]any {
	images := make([]any, 0, len(urls))
	for _, u := range urls {
		images = append(images, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": u},
		})
	}
	return map[string]any{
		"model": "gpt-image-2-0709",
		"choices": []any{
			map[string]any{
				"message": map[string]any{"role": "assistant", "content": "", "images": images},
			},
		},
		"usage": map[string]any{"total_tokens": float64(99)},
	}
}

func newTestWorker(t *testing.T, core coreGateway) (*Worker, *memTaskStore, *memUserStore, *AssetStore) {
	t.Helper()
	assets, err := NewAssetStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewAssetStore: %v", err)
	}
	tasks := newMemTaskStore()
	users := newMemUserStore()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewWorker(tasks, users, assets, core, logger), tasks, users, assets
}

func seedTask(t *testing.T, tasks TaskStore, userID int64, input map[string]any) *Task {
	t.Helper()
	task := &Task{UserID: userID, TaskType: "image.generate", Input: input, MaxAttempts: 3}
	if err := tasks.Create(context.Background(), task); err != nil {
		t.Fatalf("Create task: %v", err)
	}
	return task
}

// ==================== worker 领取 / 成功 / 重试 ====================

func TestWorkerRunOnceCompletesTask(t *testing.T) {
	core := &fakeCore{resp: chatResponseWithImages(tinyPNGDataURI())}
	w, tasks, users, _ := newTestWorker(t, core)

	// flux-dev：目录无声明 → 启发式 openai，但非 gpt-image/dall-e 系 → 回退 chat 路径
	user, _ := users.Upsert(context.Background(), 42, "u@example.com", "u", "sk-test")
	task := seedTask(t, tasks, user.ID, map[string]any{"model": "flux-dev", "prompt": "a cat"})

	if claimed := w.runOnce(context.Background()); !claimed {
		t.Fatalf("runOnce 应领取到任务")
	}

	got, err := tasks.GetByID(context.Background(), user.ID, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != TaskStatusCompleted {
		t.Fatalf("status = %s, want completed（error=%s）", got.Status, got.ErrorMessage)
	}
	images := stringSliceFromAny(got.Output["images"])
	if len(images) != 1 || !strings.HasPrefix(images[0], assetURLPrefix) {
		t.Fatalf("output.images = %#v，应为本地资产 URL", got.Output["images"])
	}
	if got.Output["model"] != "gpt-image-2-0709" {
		t.Fatalf("output.model = %v", got.Output["model"])
	}
	if content, _ := got.Output["content"].(string); !strings.Contains(content, images[0]) {
		t.Fatalf("output.content 应包含图片 markdown，got %q", content)
	}
	// 请求装配检查：单条 user 消息，parts = text + 无输入图
	if len(core.requests) != 1 {
		t.Fatalf("请求次数 = %d", len(core.requests))
	}
	if core.requests[0]["model"] != "flux-dev" {
		t.Fatalf("请求 model = %v", core.requests[0]["model"])
	}
}

func TestWorkerSavesGeneratedImageToDisk(t *testing.T) {
	core := &fakeCore{resp: chatResponseWithImages(tinyPNGDataURI())}
	w, tasks, users, assets := newTestWorker(t, core)

	user, _ := users.Upsert(context.Background(), 42, "", "u", "sk-test")
	task := seedTask(t, tasks, user.ID, map[string]any{"model": "m", "prompt": "p"})
	w.runOnce(context.Background())

	got, _ := tasks.GetByID(context.Background(), user.ID, task.ID)
	images := stringSliceFromAny(got.Output["images"])
	if len(images) != 1 {
		t.Fatalf("images = %#v", images)
	}
	name := strings.TrimPrefix(images[0], assetURLPrefix)
	data, err := os.ReadFile(filepath.Join(assets.dir, name))
	if err != nil {
		t.Fatalf("产物文件应已落盘: %v", err)
	}
	want, _ := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if !reflect.DeepEqual(data, want) {
		t.Fatalf("落盘内容与 data URI 解码不一致")
	}
}

func TestWorkerRetryThenFailAfterMaxAttempts(t *testing.T) {
	core := &fakeCore{err: fmt.Errorf("上游超时")}
	w, tasks, users, _ := newTestWorker(t, core)

	user, _ := users.Upsert(context.Background(), 42, "", "u", "sk-test")
	task := seedTask(t, tasks, user.ID, map[string]any{"model": "m", "prompt": "p"})

	// 前两次失败回 pending，第三次达到 max_attempts 置 failed
	for i := 1; i <= 3; i++ {
		if claimed := w.runOnce(context.Background()); !claimed {
			t.Fatalf("第 %d 次 runOnce 应领取到任务", i)
		}
		got, _ := tasks.GetByID(context.Background(), user.ID, task.ID)
		if got.Attempts != i {
			t.Fatalf("第 %d 次后 attempts = %d", i, got.Attempts)
		}
		wantStatus := TaskStatusPending
		if i == 3 {
			wantStatus = TaskStatusFailed
		}
		if got.Status != wantStatus {
			t.Fatalf("第 %d 次后 status = %s, want %s", i, got.Status, wantStatus)
		}
		if got.ErrorMessage == "" {
			t.Fatalf("第 %d 次后 error_message 为空", i)
		}
	}

	// 终态后不再被领取
	if claimed := w.runOnce(context.Background()); claimed {
		t.Fatalf("failed 终态任务不应再被领取")
	}
}

func TestWorkerFailsWhenUserHasNoAPIKey(t *testing.T) {
	core := &fakeCore{resp: chatResponseWithImages(tinyPNGDataURI())}
	w, tasks, users, _ := newTestWorker(t, core)

	user, _ := users.Upsert(context.Background(), 42, "", "u", "") // 无 key
	task := seedTask(t, tasks, user.ID, map[string]any{"model": "m", "prompt": "p"})
	w.runOnce(context.Background())

	got, _ := tasks.GetByID(context.Background(), user.ID, task.ID)
	if got.Status != TaskStatusPending || got.Attempts != 1 {
		t.Fatalf("status/attempts = %s/%d", got.Status, got.Attempts)
	}
	if !strings.Contains(got.ErrorMessage, "API Key") {
		t.Fatalf("error_message = %q", got.ErrorMessage)
	}
	if len(core.requests) != 0 {
		t.Fatalf("无 key 不应发起生成请求")
	}
}

func TestWorkerClaimOrderAndIdleReturn(t *testing.T) {
	core := &fakeCore{resp: chatResponseWithImages(tinyPNGDataURI())}
	w, tasks, users, _ := newTestWorker(t, core)

	if claimed := w.runOnce(context.Background()); claimed {
		t.Fatalf("空队列不应领取到任务")
	}

	user, _ := users.Upsert(context.Background(), 42, "", "u", "sk-test")
	first := seedTask(t, tasks, user.ID, map[string]any{"model": "m", "prompt": "first"})
	second := seedTask(t, tasks, user.ID, map[string]any{"model": "m", "prompt": "second"})

	w.runOnce(context.Background())
	gotFirst, _ := tasks.GetByID(context.Background(), user.ID, first.ID)
	gotSecond, _ := tasks.GetByID(context.Background(), user.ID, second.ID)
	if gotFirst.Status != TaskStatusCompleted {
		t.Fatalf("应先领取最早的任务，first = %s", gotFirst.Status)
	}
	if gotSecond.Status != TaskStatusPending {
		t.Fatalf("second 不应被同轮领取，got %s", gotSecond.Status)
	}
}

// ==================== 生成请求装配 ====================

func TestGenerateImagesBuildsMultimodalMessage(t *testing.T) {
	core := &fakeCore{resp: chatResponseWithImages(tinyPNGDataURI())}
	input := map[string]any{
		"model":  "flux-dev", // 非 images API 模型 → chat 回退路径
		"prompt": "repaint the sky",
		"images": []any{"data:image/png;base64,src1", "data:image/png;base64,src2"},
		"mask":   "data:image/png;base64,mask",
	}
	result, err := generateImages(context.Background(), core, "sk-x", input, []string{"openai"}, nil)
	if err != nil {
		t.Fatalf("generateImages: %v", err)
	}
	if len(result.Images) != 1 {
		t.Fatalf("images = %#v", result.Images)
	}

	payload := core.requests[0]
	messages := payload["messages"].([]map[string]any)
	if len(messages) != 1 || messages[0]["role"] != "user" {
		t.Fatalf("messages = %#v", messages)
	}
	parts := messages[0]["content"].([]map[string]any)
	// text + 2 张输入图 + 1 张 mask
	if len(parts) != 4 {
		t.Fatalf("parts 数 = %d, want 4", len(parts))
	}
	if parts[0]["type"] != "text" || parts[0]["text"] != "repaint the sky" {
		t.Fatalf("parts[0] = %#v", parts[0])
	}
	for i, wantURL := range []string{"data:image/png;base64,src1", "data:image/png;base64,src2", "data:image/png;base64,mask"} {
		part := parts[i+1]
		if part["type"] != "image_url" {
			t.Fatalf("parts[%d].type = %v", i+1, part["type"])
		}
		iu := part["image_url"].(map[string]any)
		if iu["url"] != wantURL {
			t.Fatalf("parts[%d].url = %v, want %s", i+1, iu["url"], wantURL)
		}
	}
}

func TestGenerateImagesValidatesInput(t *testing.T) {
	core := &fakeCore{resp: chatResponseWithImages(tinyPNGDataURI())}
	tests := []struct {
		name  string
		input map[string]any
	}{
		{"缺 model", map[string]any{"prompt": "p"}},
		{"缺 prompt", map[string]any{"model": "m"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := generateImages(context.Background(), core, "sk-x", tt.input, nil, nil); err == nil {
				t.Fatalf("应返回错误")
			}
		})
	}
}

func TestGenerateImagesNoImageInResponse(t *testing.T) {
	core := &fakeCore{resp: map[string]any{
		"choices": []any{
			map[string]any{"message": map[string]any{"content": "抱歉，我无法生成这张图。"}},
		},
	}}
	_, err := generateImages(context.Background(), core, "sk-x", map[string]any{"model": "m", "prompt": "p"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "未包含图片") {
		t.Fatalf("err = %v", err)
	}
}

// ==================== 执行策略分发 ====================

// imagesAPIResponse 构造 /v1/images/* 标准响应（b64_json 形态）。
func imagesAPIResponse(b64s ...string) map[string]any {
	data := make([]any, 0, len(b64s))
	for _, b := range b64s {
		data = append(data, map[string]any{"b64_json": b})
	}
	return map[string]any{
		"created": float64(1),
		"data":    data,
		"usage":   map[string]any{"total_tokens": float64(7)},
	}
}

// geminiContentResponse 构造 :generateContent inlineData 响应。
func geminiContentResponse(b64 string) map[string]any {
	return map[string]any{
		"modelVersion": "gemini-2.5-flash-image-001",
		"candidates": []any{map[string]any{
			"content": map[string]any{
				"parts": []any{map[string]any{
					"inlineData": map[string]any{"mimeType": "image/png", "data": b64},
				}},
			},
		}},
		"usageMetadata": map[string]any{"totalTokenCount": float64(11)},
	}
}

// predictResponse 构造 :predict predictions 响应。
func predictResponse(b64 string) map[string]any {
	return map[string]any{
		"predictions": []any{map[string]any{"bytesBase64Encoded": b64, "mimeType": "image/png"}},
	}
}

// TestGenerateImagesDispatch 协议 × 模型名 × 有无输入图 → 应打到的端点（含回退）。
func TestGenerateImagesDispatch(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		protocols []string
		images    []any
		// want*：各端点期望的请求次数
		wantChat, wantImages, wantEdits, wantGemini, wantPredict int
	}{
		{"imagen×gemini → predict", "imagen-4.0-generate-001", []string{"gemini"}, nil, 0, 0, 0, 0, 1},
		{"imagen 带输入图仍 predict", "imagen-3", []string{"gemini"}, []any{tinyPNGDataURI()}, 0, 0, 0, 0, 1},
		{"gemini 图像模型 → generateContent", "gemini-2.5-flash-image", []string{"gemini"}, nil, 0, 0, 0, 1, 0},
		{"gemini 带输入图 → generateContent", "gemini-2.5-flash-image", []string{"gemini"}, []any{tinyPNGDataURI()}, 0, 0, 0, 1, 0},
		{"gpt-image 无输入图 → generations", "gpt-image-1", []string{"openai"}, nil, 0, 1, 0, 0, 0},
		{"gpt-image 有输入图 → edits", "gpt-image-1", []string{"openai"}, []any{tinyPNGDataURI()}, 0, 0, 1, 0, 0},
		{"dall-e 无输入图 → generations", "dall-e-3", []string{"openai"}, nil, 0, 1, 0, 0, 0},
		{"厂商前缀不影响判定", "openai/dall-e-2", []string{"openai"}, []any{tinyPNGDataURI()}, 0, 0, 1, 0, 0},
		{"imagen 无 gemini 协议 → chat 回退", "imagen-4", []string{"openai"}, nil, 1, 0, 0, 0, 0},
		{"gpt-image 无 openai 协议 → chat 回退", "gpt-image-1", []string{"anthropic"}, nil, 1, 0, 0, 0, 0},
		{"普通模型 → chat 回退", "flux-dev", []string{"openai"}, nil, 1, 0, 0, 0, 0},
		{"协议为空 → chat 回退", "whatever", nil, nil, 1, 0, 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core := &fakeCore{
				resp:        chatResponseWithImages(tinyPNGDataURI()),
				imagesResp:  imagesAPIResponse(tinyPNGBase64),
				editsResp:   imagesAPIResponse(tinyPNGBase64),
				geminiResp:  geminiContentResponse(tinyPNGBase64),
				predictResp: predictResponse(tinyPNGBase64),
			}
			input := map[string]any{"model": tt.model, "prompt": "p"}
			if len(tt.images) > 0 {
				input["images"] = tt.images
			}
			result, err := generateImages(context.Background(), core, "sk-x", input, tt.protocols, dataURILoader)
			if err != nil {
				t.Fatalf("generateImages: %v", err)
			}
			if len(result.Images) == 0 {
				t.Fatalf("结果应包含图片")
			}
			got := []int{len(core.requests), len(core.imagesRequests), len(core.editsFields), len(core.geminiRequests), len(core.predictRequests)}
			want := []int{tt.wantChat, tt.wantImages, tt.wantEdits, tt.wantGemini, tt.wantPredict}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("端点命中 [chat images edits gemini predict] = %v, want %v", got, want)
			}
		})
	}
}

// TestWorkerDispatchUsesModelCatalog 端到端：worker 经 /v1/models 目录判定协议并走 images 路径落盘。
func TestWorkerDispatchUsesModelCatalog(t *testing.T) {
	core := &fakeCore{
		imagesResp: imagesAPIResponse(tinyPNGBase64),
		protocols:  map[string][]string{"gpt-image-1": {"openai"}},
	}
	w, tasks, users, _ := newTestWorker(t, core)

	user, _ := users.Upsert(context.Background(), 42, "", "u", "sk-test")
	task := seedTask(t, tasks, user.ID, map[string]any{
		"model": "gpt-image-1", "prompt": "a dog", "size": "1024x1024", "quality": "high",
	})
	if claimed := w.runOnce(context.Background()); !claimed {
		t.Fatalf("runOnce 应领取到任务")
	}

	got, _ := tasks.GetByID(context.Background(), user.ID, task.ID)
	if got.Status != TaskStatusCompleted {
		t.Fatalf("status = %s（error=%s）", got.Status, got.ErrorMessage)
	}
	images := stringSliceFromAny(got.Output["images"])
	if len(images) != 1 || !strings.HasPrefix(images[0], assetURLPrefix) {
		t.Fatalf("output.images = %#v，应为本地资产 URL", got.Output["images"])
	}
	if core.protocolsCalls != 1 {
		t.Fatalf("目录拉取次数 = %d, want 1", core.protocolsCalls)
	}
	if len(core.requests) != 0 || len(core.imagesRequests) != 1 {
		t.Fatalf("应走 images generations 路径（chat=%d images=%d）", len(core.requests), len(core.imagesRequests))
	}
	// size/quality 原生透传
	req := core.imagesRequests[0]
	if req["size"] != "1024x1024" || req["quality"] != "high" {
		t.Fatalf("generations 请求参数 = %#v", req)
	}
}

// TestWorkerEditsReadsLocalAssetInput 端到端：图生图输入为本地资产 URL 时读盘作为 multipart 文件。
func TestWorkerEditsReadsLocalAssetInput(t *testing.T) {
	core := &fakeCore{
		editsResp: imagesAPIResponse(tinyPNGBase64),
		protocols: map[string][]string{"gpt-image-1": {"openai"}},
	}
	w, tasks, users, assets := newTestWorker(t, core)

	// 预置一张历史产物作为输入图
	srcData, _ := base64.StdEncoding.DecodeString(tinyPNGBase64)
	srcURL, err := assets.SaveGenerated(srcData, ".png")
	if err != nil {
		t.Fatalf("SaveGenerated: %v", err)
	}

	user, _ := users.Upsert(context.Background(), 42, "", "u", "sk-test")
	task := seedTask(t, tasks, user.ID, map[string]any{
		"model": "gpt-image-1", "prompt": "restyle", "images": []any{srcURL}, "mask": tinyPNGDataURI(),
	})
	w.runOnce(context.Background())

	got, _ := tasks.GetByID(context.Background(), user.ID, task.ID)
	if got.Status != TaskStatusCompleted {
		t.Fatalf("status = %s（error=%s）", got.Status, got.ErrorMessage)
	}
	if len(core.editsFiles) != 1 {
		t.Fatalf("edits 请求次数 = %d", len(core.editsFiles))
	}
	files := core.editsFiles[0]
	if len(files) != 2 {
		t.Fatalf("文件 part 数 = %d, want 2（image+mask）", len(files))
	}
	if files[0].Field != "image" || !reflect.DeepEqual(files[0].Data, srcData) {
		t.Fatalf("image part = %+v", files[0].Field)
	}
	if files[1].Field != "mask" {
		t.Fatalf("mask part 字段 = %s", files[1].Field)
	}
}

// ==================== 响应图片提取 ====================

func TestExtractImagesFromResponse(t *testing.T) {
	dataURI := tinyPNGDataURI()
	tests := []struct {
		name string
		resp map[string]any
		want []string
	}{
		{
			name: "message.images 数组（image_url.url 形态）",
			resp: chatResponseWithImages("https://cdn.example.com/a.png", dataURI),
			want: []string{"https://cdn.example.com/a.png", dataURI},
		},
		{
			name: "content markdown 图链",
			resp: map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]any{
						"content": "结果：![cat](https://cdn.example.com/cat.png) 完成",
					},
				}},
			},
			want: []string{"https://cdn.example.com/cat.png"},
		},
		{
			name: "content 裸 data: base64 链",
			resp: map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]any{
						"content": "这是图片 " + dataURI + " 请查收",
					},
				}},
			},
			want: []string{dataURI},
		},
		{
			name: "两种形态并存去重",
			resp: map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]any{
						"images": []any{
							map[string]any{"image_url": map[string]any{"url": "https://x/1.png"}},
						},
						"content": "![img](https://x/1.png) ![img2](https://x/2.png)",
					},
				}},
			},
			want: []string{"https://x/1.png", "https://x/2.png"},
		},
		{
			name: "images 元素为纯字符串",
			resp: map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]any{
						"images": []any{"https://x/s.png"},
					},
				}},
			},
			want: []string{"https://x/s.png"},
		},
		{
			name: "无图片",
			resp: map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]any{"content": "plain text"},
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
			got := extractImagesFromResponse(tt.resp)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseDataURI(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		wantMIME string
		wantErr  bool
	}{
		{"合法 png", tinyPNGDataURI(), "image/png", false},
		{"合法 jpeg", "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("jpg")), "image/jpeg", false},
		{"非 data URI", "https://x/a.png", "", true},
		{"缺逗号", "data:image/png;base64", "", true},
		{"非 base64 编码声明", "data:image/png,rawdata", "", true},
		{"base64 内容非法", "data:image/png;base64,!!!!", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mime, data, err := parseDataURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("应返回错误")
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if mime != tt.wantMIME {
				t.Fatalf("mime = %s, want %s", mime, tt.wantMIME)
			}
			if len(data) == 0 {
				t.Fatalf("data 为空")
			}
		})
	}
}

func TestLocalizeImageDownloadsRemoteURL(t *testing.T) {
	core := &fakeCore{fetchData: []byte("fake-image-bytes"), fetchMIME: "image/webp"}
	w, _, _, assets := newTestWorker(t, core)

	url, err := w.localizeImage(context.Background(), "https://cdn.example.com/gen.webp")
	if err != nil {
		t.Fatalf("localizeImage: %v", err)
	}
	if !strings.HasPrefix(url, assetURLPrefix) || !strings.HasSuffix(url, ".webp") {
		t.Fatalf("url = %s", url)
	}
	name := strings.TrimPrefix(url, assetURLPrefix)
	if _, err := os.Stat(filepath.Join(assets.dir, name)); err != nil {
		t.Fatalf("下载产物应已落盘: %v", err)
	}
}

func TestLocalizeImageKeepsOriginalOnDownloadFailure(t *testing.T) {
	core := &fakeCore{
		resp:     chatResponseWithImages("https://cdn.example.com/gone.png"),
		fetchErr: fmt.Errorf("404"),
	}
	w, tasks, users, _ := newTestWorker(t, core)

	user, _ := users.Upsert(context.Background(), 42, "", "u", "sk-test")
	task := seedTask(t, tasks, user.ID, map[string]any{"model": "m", "prompt": "p"})
	w.runOnce(context.Background())

	got, _ := tasks.GetByID(context.Background(), user.ID, task.ID)
	if got.Status != TaskStatusCompleted {
		t.Fatalf("下载失败不应报废任务, status = %s (%s)", got.Status, got.ErrorMessage)
	}
	images := stringSliceFromAny(got.Output["images"])
	if len(images) != 1 || images[0] != "https://cdn.example.com/gone.png" {
		t.Fatalf("应保留原始 URL 兜底, images = %#v", images)
	}
}

// ==================== 资产存储 ====================

func TestAssetStoreDeleteRejectsForeignURL(t *testing.T) {
	assets, err := NewAssetStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewAssetStore: %v", err)
	}
	// 非本存储 URL / 穿越尝试都应安全忽略
	for _, u := range []string{
		"https://x/a.png",
		"/assets-runtime/generated/../../../etc/passwd",
		"/assets-runtime/generated/not-a-uuid.png",
	} {
		if err := assets.Delete(u); err != nil {
			t.Fatalf("Delete(%q) = %v, want nil", u, err)
		}
	}
}

func TestAssetStoreSaveAndDelete(t *testing.T) {
	assets, err := NewAssetStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewAssetStore: %v", err)
	}
	url, err := assets.SaveGenerated([]byte("img"), ".png")
	if err != nil {
		t.Fatalf("SaveGenerated: %v", err)
	}
	name := strings.TrimPrefix(url, assetURLPrefix)
	path := filepath.Join(assets.dir, name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("文件应存在: %v", err)
	}
	if err := assets.Delete(url); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("文件应已删除")
	}
}
