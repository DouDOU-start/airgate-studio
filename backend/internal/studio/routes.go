package studio

import (
	"context"
	"encoding/json"
	"errors"
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
	assets *AssetStore
	core   *CoreClient
	logger *slog.Logger
}

// NewServer 创建 HTTP 服务装配器。
func NewServer(cfg *Config, tasks TaskStore, users UserStore, assets *AssetStore, core *CoreClient, logger *slog.Logger) *Server {
	return &Server{cfg: cfg, tasks: tasks, users: users, assets: assets, core: core, logger: logger}
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

// ==================== 模型列表 ====================

// handleListModels GET /api/models：拿当前用户的 sk- key 透传 core 的 /v1/models。
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request, user *User) {
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
