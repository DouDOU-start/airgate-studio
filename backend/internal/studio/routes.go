package studio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// Server 聚合独立服务的全部依赖，负责路由装配与 HTTP 处理。
type Server struct {
	cfg    *Config
	tasks  TaskStore
	users  UserStore
	shelf  ShelfStore
	assets *AssetStore
	core   *CoreClient
	logger *slog.Logger
}

// NewServer 创建 HTTP 服务装配器。
func NewServer(cfg *Config, tasks TaskStore, users UserStore, shelf ShelfStore, assets *AssetStore, core *CoreClient, logger *slog.Logger) *Server {
	return &Server{cfg: cfg, tasks: tasks, users: users, shelf: shelf, assets: assets, core: core, logger: logger}
}

// Handler 装配全部路由：
//
//	/auth/*            OAuth 登录/回调/登出
//	/api/*             业务 API（会话鉴权）
//	/assets-runtime/*  生成产物静态文件
//	/                  SPA 前端（fallback index.html）
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /auth/login", s.handleLogin)
	mux.HandleFunc("GET /auth/callback", s.handleCallback)
	mux.HandleFunc("POST /auth/logout", s.handleLogout)

	mux.HandleFunc("GET /api/user/info", s.requireUser(s.handleUserInfo))
	mux.HandleFunc("POST /api/generation-tasks", s.requireUser(s.handleCreateGenerationTask))
	mux.HandleFunc("GET /api/generation-tasks", s.requireUser(s.handleListGenerationTasks))
	mux.HandleFunc("GET /api/generation-tasks/{id}", s.requireUser(s.handleGetGenerationTask))
	mux.HandleFunc("DELETE /api/generation-tasks/{id}", s.requireUser(s.handleDeleteGenerationTask))
	mux.HandleFunc("GET /api/models", s.requireUser(s.handleListModels))
	mux.HandleFunc("GET /api/groups", s.requireUser(s.handleListGroups))

	// 管理端：分组开关 + 按组同步/上架模型（config 白名单判定管理员）。
	mux.HandleFunc("GET /api/admin/groups", s.requireAdmin(s.handleAdminListGroups))
	mux.HandleFunc("PUT /api/admin/groups/{id}", s.requireAdmin(s.handleAdminSetGroupEnabled))
	mux.HandleFunc("POST /api/admin/groups/{id}/sync-models", s.requireAdmin(s.handleAdminSyncModels))
	mux.HandleFunc("GET /api/admin/models", s.requireAdmin(s.handleAdminListModels))
	mux.HandleFunc("PUT /api/admin/models/{id}", s.requireAdmin(s.handleAdminUpdateModel))

	mux.HandleFunc("GET /assets-runtime/generated/{file}", func(w http.ResponseWriter, r *http.Request) {
		s.assets.ServeFile(w, r, r.PathValue("file"))
	})

	mux.Handle("/", s.spaHandler())
	return mux
}

// ==================== 用户信息 ====================

// handleUserInfo GET /api/user/info：会话用户 + core 余额。
// api_key 明文绝不下发，仅返回是否已领取。
func (s *Server) handleUserInfo(w http.ResponseWriter, r *http.Request, user *User) {
	resp := map[string]interface{}{
		"user_id":         user.ID,
		"airgate_user_id": user.AirgateUserID,
		"username":        user.Username,
		"email":           user.Email,
		"api_key_ready":   user.APIKey != "",
		"is_admin":        s.cfg.IsAdmin(user.AirgateUserID),
		"balance":         nil,
	}
	if user.APIKey != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if bal, err := s.core.UserBalance(ctx, user.APIKey); err == nil {
			resp["balance"] = bal["balance"]
			resp["balance_detail"] = bal
		} else {
			s.logger.Warn("fetch_balance_failed", "user_id", user.ID, "error", err)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// ==================== 生成任务 ====================

func (s *Server) handleCreateGenerationTask(w http.ResponseWriter, r *http.Request, user *User) {
	var req createGenerationTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	normalizeGenerationRequest(&req)

	if req.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt is required"})
		return
	}
	if req.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model is required"})
		return
	}

	// 分组模式：校验分组已开放、模型已上架、用户持有该组 key（体验优化；
	// 硬约束在 core——即使绕过这里，key 权限与计费也由 core 兜底）。
	if req.GroupID > 0 {
		if err := s.validateShelfSelection(r.Context(), user, req.GroupID, req.Model); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}

	task := &Task{
		UserID:      user.ID,
		TaskType:    resolveTaskType(req.Kind, req.Operation),
		Input:       buildTaskInput(req),
		MaxAttempts: 3,
	}
	if err := s.tasks.Create(r.Context(), task); err != nil {
		s.logger.Error("create_generation_task_failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "创建任务失败: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, buildGenerationTaskResponse(task))
}

func (s *Server) handleGetGenerationTask(w http.ResponseWriter, r *http.Request, user *User) {
	taskID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || taskID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task_id"})
		return
	}

	task, err := s.tasks.GetByID(r.Context(), user.ID, taskID)
	if errors.Is(err, ErrTaskNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "任务不存在"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "查询任务失败: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, buildGenerationTaskResponse(task))
}

func (s *Server) handleDeleteGenerationTask(w http.ResponseWriter, r *http.Request, user *User) {
	taskID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || taskID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task_id"})
		return
	}

	task, err := s.tasks.Delete(r.Context(), user.ID, taskID)
	if errors.Is(err, ErrTaskNotFound) {
		// 删除幂等：不存在也视为成功
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "删除任务失败: " + err.Error()})
		return
	}

	// 同步清理产物文件；失败仅记日志，不影响删除结果。
	if task.Output != nil {
		for _, u := range stringSliceFromAny(task.Output["images"]) {
			if err := s.assets.Delete(u); err != nil {
				s.logger.Warn("delete_asset_failed", "task_id", task.ID, "url", u, "error", err)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListGenerationTasks(w http.ResponseWriter, r *http.Request, user *User) {
	limit := 20
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 100 {
		limit = v
	}
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 {
		offset = v
	}
	status := r.URL.Query().Get("status")

	tasks, total, err := s.tasks.List(r.Context(), user.ID, status, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "查询任务列表失败: " + err.Error()})
		return
	}

	items := make([]map[string]interface{}, 0, len(tasks))
	for _, t := range tasks {
		items = append(items, buildGenerationTaskResponse(t))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"tasks": items, "total": total})
}

// ==================== 分组与模型 ====================

// validateShelfSelection 校验「分组已开放 + 模型已上架 + 用户持有该组 key」。
func (s *Server) validateShelfSelection(ctx context.Context, user *User, groupID int64, model string) error {
	groups, err := s.shelf.ListGroups(ctx, true)
	if err != nil {
		return fmt.Errorf("查询分组失败: %w", err)
	}
	enabled := false
	for _, g := range groups {
		if g.CoreGroupID == groupID {
			enabled = true
			break
		}
	}
	if !enabled {
		return fmt.Errorf("该分组未开放")
	}
	m, err := s.shelf.GetModel(ctx, groupID, model)
	if errors.Is(err, ErrShelfNotFound) {
		return fmt.Errorf("该分组下没有此模型")
	}
	if err != nil {
		return fmt.Errorf("查询模型失败: %w", err)
	}
	if !m.Enabled {
		return fmt.Errorf("该模型未上架")
	}
	keys, err := s.users.KeysByUser(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("查询用户分组 key 失败: %w", err)
	}
	if keys[groupID] == "" {
		return fmt.Errorf("尚未启用该分组，请重新登录后重试")
	}
	return nil
}

// handleListGroups GET /api/groups：已开放分组 × 当前用户 key 就绪状态。
// 返回空列表表示未启用分组模式（前端回退全量模型透传）。
func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request, user *User) {
	groups, err := s.shelf.ListGroups(r.Context(), true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "查询分组失败: " + err.Error()})
		return
	}
	keys, err := s.users.KeysByUser(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "查询分组 key 失败: " + err.Error()})
		return
	}
	items := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		items = append(items, map[string]any{
			"core_group_id":   g.CoreGroupID,
			"name":            g.Name,
			"rate_multiplier": g.RateMultiplier,
			"note":            g.Note,
			// key_ready=false 表示分组在用户上次登录后才开放，重新登录即可领取。
			"key_ready": keys[g.CoreGroupID] != "",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": items})
}

// handleListModels GET /api/models：
//   - 带 group_id 参数 → 返回该分组已上架的货架模型（分组模式）；
//   - 不带参数 → 拿当前用户默认 key 透传 core /v1/models（零配置兼容模式）。
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request, user *User) {
	if raw := r.URL.Query().Get("group_id"); raw != "" {
		groupID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || groupID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid group_id"})
			return
		}
		models, err := s.shelf.ListModels(r.Context(), groupID, true)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "查询货架模型失败: " + err.Error()})
			return
		}
		items := make([]map[string]any, 0, len(models))
		for _, m := range models {
			display := m.DisplayName
			if display == "" {
				display = m.ModelName
			}
			items = append(items, map[string]any{
				"id":           m.ModelName,
				"display_name": display,
				"protocols":    m.Protocols,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": items})
		return
	}

	if user.APIKey == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "没有可用的 API Key，请重新登录"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	body, status, err := s.core.ListModels(ctx, user.APIKey)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "查询模型列表失败: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
