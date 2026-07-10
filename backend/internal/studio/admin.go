package studio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// admin.go：管理端接口——分组开关 + 按组同步/上架模型。
//
// 管理员由 config 的 admin_airgate_user_ids 白名单判定；分组镜像在管理员登录时
// 自动收集（见 auth.go），模型同步用管理员本人在该组的 sk- key 拉 core /v1/models。

// requireAdmin 会话鉴权 + 管理员白名单校验。
func (s *Server) requireAdmin(next func(http.ResponseWriter, *http.Request, *User)) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request, user *User) {
		if !s.cfg.IsAdmin(user.AirgateUserID) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next(w, r, user)
	})
}

// handleAdminListGroups GET /api/admin/groups：全部分组镜像（含未开放）。
func (s *Server) handleAdminListGroups(w http.ResponseWriter, r *http.Request, _ *User) {
	groups, err := s.shelf.ListGroups(r.Context(), false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询分组失败: "+err.Error())
		return
	}
	if groups == nil {
		groups = []ShelfGroup{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

// handleAdminSetGroupEnabled PUT /api/admin/groups/{id}：开/关分组。
// 关组只拦新任务，不影响在途任务与已领 key（重新开放无需用户重新登录）。
func (s *Server) handleAdminSetGroupEnabled(w http.ResponseWriter, r *http.Request, _ *User) {
	groupID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || groupID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid group id")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.shelf.SetGroupEnabled(r.Context(), groupID, req.Enabled); err != nil {
		if errors.Is(err, ErrShelfNotFound) {
			writeError(w, http.StatusNotFound, "分组不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "更新分组失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAdminSyncModels POST /api/admin/groups/{id}/sync-models：
// 用管理员本人在该组的 key 拉 core /v1/models，全量同步该组货架
// （新模型默认下架；core 已下线的标记 missing_at_core，由管理员决定去留）。
func (s *Server) handleAdminSyncModels(w http.ResponseWriter, r *http.Request, admin *User) {
	groupID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || groupID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid group id")
		return
	}
	keys, err := s.users.KeysByUser(r.Context(), admin.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询分组 key 失败: "+err.Error())
		return
	}
	apiKey := keys[groupID]
	if apiKey == "" {
		writeError(w, http.StatusBadRequest, "你在该分组下还没有 key（需要在 core 侧可用该分组），请重新登录后重试")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	protocols, err := s.core.ModelProtocols(ctx, apiKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "拉取 core 模型目录失败: "+err.Error())
		return
	}

	// modality 启发式预填在此统一做（store 只存取）；管理员改过的值同步不覆盖。
	models := make([]ShelfModel, 0, len(protocols))
	for name, ps := range protocols {
		models = append(models, ShelfModel{
			ModelName: name,
			Protocols: ps,
			Modality:  guessModalityByModelName(name),
		})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ModelName < models[j].ModelName })
	if err := s.shelf.SyncModels(r.Context(), groupID, models); err != nil {
		writeError(w, http.StatusInternalServerError, "同步货架失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "synced": len(models)})
}

// handleAdminListModels GET /api/admin/models?group_id=N：分组下全部货架模型（含未上架）。
func (s *Server) handleAdminListModels(w http.ResponseWriter, r *http.Request, _ *User) {
	groupID, err := strconv.ParseInt(r.URL.Query().Get("group_id"), 10, 64)
	if err != nil || groupID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid group_id")
		return
	}
	models, err := s.shelf.ListModels(r.Context(), groupID, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询货架失败: "+err.Error())
		return
	}
	if models == nil {
		models = []ShelfModel{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// handleAdminUpdateModel PUT /api/admin/models/{id}：改展示名/上架状态/排序。
func (s *Server) handleAdminUpdateModel(w http.ResponseWriter, r *http.Request, _ *User) {
	modelID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || modelID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid model id")
		return
	}
	var req struct {
		DisplayName *string `json:"display_name"`
		Modality    *string `json:"modality"`
		Enabled     *bool   `json:"enabled"`
		SortOrder   *int    `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Modality != nil && !knownModalities[*req.Modality] {
		writeError(w, http.StatusBadRequest, "modality 仅支持 image/video/music")
		return
	}
	model, err := s.shelf.UpdateModel(r.Context(), modelID, ShelfModelPatch{
		DisplayName: req.DisplayName,
		Modality:    req.Modality,
		Enabled:     req.Enabled,
		SortOrder:   req.SortOrder,
	})
	if err != nil {
		if errors.Is(err, ErrShelfNotFound) {
			writeError(w, http.StatusNotFound, "模型不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "更新模型失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model)
}
