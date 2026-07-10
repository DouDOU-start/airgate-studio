package studio

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// shelf_test.go：分组货架的 HTTP 级测试（admin 开关/同步/上架 + 用户侧可见性 + 任务校验）。

// newShelfTestServer 装配带货架的测试服务：fake core 提供 /v1/models。
func newShelfTestServer(t *testing.T, modelsJSON string) (*Server, *memUserStore, *memShelfStore) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(modelsJSON))
	})
	core := httptest.NewServer(mux)
	t.Cleanup(core.Close)

	users := newMemUserStore()
	shelf := newMemShelfStore()
	cfg := &Config{
		AirgateBaseURL:      core.URL,
		AirgatePublicURL:    core.URL,
		OAuthClientID:       "ac_test",
		OAuthClientSecret:   "acs_test",
		PublicBaseURL:       "http://app.example",
		SessionSecret:       "test-secret",
		AdminAirgateUserIDs: []int64{42},
	}
	return NewServer(cfg, newMemTaskStore(), users, shelf, nil, NewCoreClient(core.URL), slog.New(slog.DiscardHandler)), users, shelf
}

// seedUser 造一个已登录用户并返回其会话 cookie。
func seedUser(t *testing.T, s *Server, users *memUserStore, airgateID int64, keys map[int64]string) (*User, *http.Cookie) {
	t.Helper()
	user, err := users.Upsert(context.Background(), airgateID, "u@example.com", "u", "")
	if err != nil {
		t.Fatalf("造用户失败: %v", err)
	}
	for gid, key := range keys {
		_ = users.UpsertKey(context.Background(), user.ID, gid, key)
	}
	return user, &http.Cookie{Name: sessionCookieName, Value: s.signSession(user.ID, time.Now().Add(time.Hour))}
}

func doJSON(t *testing.T, s *Server, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// TestAdminShelfFlow 管理端全链路：非管理员 403 → 同步模型 → 上架 → 开放分组 →
// 用户侧 /api/groups 与 /api/models?group_id 只见已开放/已上架。
func TestAdminShelfFlow(t *testing.T) {
	t.Parallel()

	s, users, shelf := newShelfTestServer(t,
		`{"data":[{"id":"gpt-image-1","protocols":["openai"]},{"id":"gemini-img","protocols":["gemini"]}]}`)

	_, adminCookie := seedUser(t, s, users, 42, map[int64]string{2: "sk-admin-g2"})
	_, userCookie := seedUser(t, s, users, 7, map[int64]string{2: "sk-user-g2"})
	_ = shelf.UpsertGroups(context.Background(), []ShelfGroup{{CoreGroupID: 2, Name: "vip", RateMultiplier: 2}})

	// 非管理员访问管理端 → 403。
	if rec := doJSON(t, s, http.MethodGet, "/api/admin/groups", "", userCookie); rec.Code != http.StatusForbidden {
		t.Fatalf("非管理员 status = %d, want 403", rec.Code)
	}

	// 同步分组 2 的模型（用管理员在该组的 key 拉 fake core）。
	if rec := doJSON(t, s, http.MethodPost, "/api/admin/groups/2/sync-models", "", adminCookie); rec.Code != http.StatusOK {
		t.Fatalf("sync-models = (%d, %s)", rec.Code, rec.Body.String())
	}
	models, _ := shelf.ListModels(context.Background(), 2, false)
	if len(models) != 2 || models[0].Enabled {
		t.Fatalf("同步结果 = %+v, want 2 个默认下架", models)
	}

	// 上架 gpt-image-1 并改展示名。
	body := `{"enabled":true,"display_name":"图像生成"}`
	var target ShelfModel
	for _, m := range models {
		if m.ModelName == "gpt-image-1" {
			target = m
		}
	}
	if rec := doJSON(t, s, http.MethodPut, "/api/admin/models/"+itoa64(target.ID), body, adminCookie); rec.Code != http.StatusOK {
		t.Fatalf("上架 = (%d, %s)", rec.Code, rec.Body.String())
	}

	// 未开放分组前用户不可见。
	rec := doJSON(t, s, http.MethodGet, "/api/groups", "", userCookie)
	var groupsResp struct {
		Groups []map[string]any `json:"groups"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &groupsResp)
	if len(groupsResp.Groups) != 0 {
		t.Fatalf("未开放时用户可见分组 = %v, want 空", groupsResp.Groups)
	}

	// 开放分组 2。
	if rec := doJSON(t, s, http.MethodPut, "/api/admin/groups/2", `{"enabled":true}`, adminCookie); rec.Code != http.StatusOK {
		t.Fatalf("开放分组 = (%d, %s)", rec.Code, rec.Body.String())
	}

	// 用户可见分组（key_ready=true）与已上架模型。
	rec = doJSON(t, s, http.MethodGet, "/api/groups", "", userCookie)
	_ = json.Unmarshal(rec.Body.Bytes(), &groupsResp)
	if len(groupsResp.Groups) != 1 || groupsResp.Groups[0]["key_ready"] != true {
		t.Fatalf("用户分组 = %v, want vip key_ready", groupsResp.Groups)
	}
	rec = doJSON(t, s, http.MethodGet, "/api/models?group_id=2", "", userCookie)
	var modelsResp struct {
		Data []map[string]any `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &modelsResp)
	if len(modelsResp.Data) != 1 || modelsResp.Data[0]["id"] != "gpt-image-1" || modelsResp.Data[0]["display_name"] != "图像生成" {
		t.Fatalf("用户模型 = %v, want 仅上架的 gpt-image-1", modelsResp.Data)
	}
}

// TestCreateTaskShelfValidation 任务提交校验：分组未开放 / 模型未上架 / 无该组 key。
func TestCreateTaskShelfValidation(t *testing.T) {
	t.Parallel()

	s, users, shelf := newShelfTestServer(t, `{"data":[]}`)
	_, userCookie := seedUser(t, s, users, 7, map[int64]string{2: "sk-user-g2"})
	_, noKeyCookie := seedUser(t, s, users, 8, nil)

	_ = shelf.UpsertGroups(context.Background(), []ShelfGroup{{CoreGroupID: 2, Name: "vip"}})
	_ = shelf.SyncModels(context.Background(), 2, []ShelfModel{{ModelName: "gpt-image-1", Protocols: []string{"openai"}}})

	taskBody := func(groupID int64) string {
		return `{"kind":"image","operation":"generate","model":"gpt-image-1","prompt":"a cat","group_id":` + itoa64(groupID) + `}`
	}

	// 分组未开放。
	if rec := doJSON(t, s, http.MethodPost, "/api/generation-tasks", taskBody(2), userCookie); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "未开放") {
		t.Fatalf("未开放分组 = (%d, %s)", rec.Code, rec.Body.String())
	}
	_ = shelf.SetGroupEnabled(context.Background(), 2, true)

	// 模型未上架。
	if rec := doJSON(t, s, http.MethodPost, "/api/generation-tasks", taskBody(2), userCookie); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "未上架") {
		t.Fatalf("未上架模型 = (%d, %s)", rec.Code, rec.Body.String())
	}
	m, _ := shelf.GetModel(context.Background(), 2, "gpt-image-1")
	enabled := true
	_, _ = shelf.UpdateModel(context.Background(), m.ID, ShelfModelPatch{Enabled: &enabled})

	// 无该组 key（分组在用户登录后才开放）。
	if rec := doJSON(t, s, http.MethodPost, "/api/generation-tasks", taskBody(2), noKeyCookie); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "重新登录") {
		t.Fatalf("缺 key = (%d, %s)", rec.Code, rec.Body.String())
	}

	// 全部就绪 → 202，任务 input 落 group_id。
	rec := doJSON(t, s, http.MethodPost, "/api/generation-tasks", taskBody(2), userCookie)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("创建任务 = (%d, %s)", rec.Code, rec.Body.String())
	}
}

func itoa64(v int64) string {
	return strconv.FormatInt(v, 10)
}
