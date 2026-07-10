package studio

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// shelf.go：分组×模型货架。
//
// 管理员从 core 侧同步「分组」（登录时自动收集自 userinfo.groups）与「分组下的模型」
//（手动 sync，用管理员本人在该组的 key 拉 /v1/models），再决定开放哪些分组、上架哪些模型。
// 用户只看到已开放分组 × 已上架模型；渠道调度与计费仍全在 core（key 属组）。

// ErrShelfNotFound 货架条目不存在。
var ErrShelfNotFound = errors.New("货架条目不存在")

// ShelfGroup core 分组在 studio 的镜像行。
type ShelfGroup struct {
	CoreGroupID    int64     `json:"core_group_id"`
	Name           string    `json:"name"`
	RateMultiplier float64   `json:"rate_multiplier"`
	Note           string    `json:"note"`
	Enabled        bool      `json:"enabled"`
	SyncedAt       time.Time `json:"synced_at"`
}

// ShelfModel 分组下的货架模型。
type ShelfModel struct {
	ID            int64     `json:"id"`
	CoreGroupID   int64     `json:"core_group_id"`
	ModelName     string    `json:"model_name"`
	DisplayName   string    `json:"display_name"`
	Protocols     []string  `json:"protocols"`
	Enabled       bool      `json:"enabled"`
	SortOrder     int       `json:"sort_order"`
	MissingAtCore bool      `json:"missing_at_core"`
	SyncedAt      time.Time `json:"synced_at"`
}

// ShelfModelPatch 模型编辑的可写字段（nil = 不改）。
type ShelfModelPatch struct {
	DisplayName *string
	Enabled     *bool
	SortOrder   *int
}

// ShelfStore 货架仓储；HTTP 层与 worker 共用，单测用内存实现替换。
type ShelfStore interface {
	// UpsertGroups 按 core_group_id 幂等写入分组镜像（保留既有 enabled 开关）。
	UpsertGroups(ctx context.Context, groups []ShelfGroup) error
	// ListGroups 列出分组；onlyEnabled 为 true 时只返回已开放的。
	ListGroups(ctx context.Context, onlyEnabled bool) ([]ShelfGroup, error)
	// SetGroupEnabled 开/关一个分组；分组不存在返回 ErrShelfNotFound。
	SetGroupEnabled(ctx context.Context, coreGroupID int64, enabled bool) error
	// SyncModels 按分组全量同步模型：新模型插入（enabled=false），既有模型刷新
	// protocols/synced_at 并清除漂移标记，core 已不存在的标记 missing_at_core。
	SyncModels(ctx context.Context, coreGroupID int64, models []ShelfModel) error
	// ListModels 列出分组下模型；onlyEnabled 为 true 时只返回已上架的。
	ListModels(ctx context.Context, coreGroupID int64, onlyEnabled bool) ([]ShelfModel, error)
	// GetModel 按 (coreGroupID, modelName) 查询；不存在返回 ErrShelfNotFound。
	GetModel(ctx context.Context, coreGroupID int64, modelName string) (*ShelfModel, error)
	// UpdateModel 局部更新模型；不存在返回 ErrShelfNotFound。
	UpdateModel(ctx context.Context, id int64, patch ShelfModelPatch) (*ShelfModel, error)
}

// pgShelfStore ShelfStore 的 Postgres 实现。
type pgShelfStore struct {
	db *sql.DB
}

// NewPGShelfStore 创建 Postgres 货架仓储。
func NewPGShelfStore(db *sql.DB) ShelfStore {
	return &pgShelfStore{db: db}
}

func (s *pgShelfStore) UpsertGroups(ctx context.Context, groups []ShelfGroup) error {
	const q = `
		INSERT INTO studio_groups (core_group_id, name, rate_multiplier, note, synced_at, updated_at)
		VALUES ($1, $2, $3, $4, now(), now())
		ON CONFLICT (core_group_id) DO UPDATE
		SET name = EXCLUDED.name,
		    rate_multiplier = EXCLUDED.rate_multiplier,
		    note = EXCLUDED.note,
		    synced_at = now(),
		    updated_at = now()`
	for _, g := range groups {
		if _, err := s.db.ExecContext(ctx, q, g.CoreGroupID, g.Name, g.RateMultiplier, g.Note); err != nil {
			return fmt.Errorf("写入分组镜像失败: %w", err)
		}
	}
	return nil
}

func (s *pgShelfStore) ListGroups(ctx context.Context, onlyEnabled bool) ([]ShelfGroup, error) {
	q := `SELECT core_group_id, name, rate_multiplier, note, enabled, synced_at
		FROM studio_groups`
	if onlyEnabled {
		q += ` WHERE enabled`
	}
	q += ` ORDER BY core_group_id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("查询分组列表失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ShelfGroup
	for rows.Next() {
		var g ShelfGroup
		if err := rows.Scan(&g.CoreGroupID, &g.Name, &g.RateMultiplier, &g.Note, &g.Enabled, &g.SyncedAt); err != nil {
			return nil, fmt.Errorf("解析分组行失败: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *pgShelfStore) SetGroupEnabled(ctx context.Context, coreGroupID int64, enabled bool) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE studio_groups SET enabled = $2, updated_at = now() WHERE core_group_id = $1`,
		coreGroupID, enabled)
	if err != nil {
		return fmt.Errorf("更新分组开关失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrShelfNotFound
	}
	return nil
}

func (s *pgShelfStore) SyncModels(ctx context.Context, coreGroupID int64, models []ShelfModel) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启模型同步事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 先全部标记漂移，同步命中的再清除——core 已下线的模型保留行但亮 missing 提示，
	// 是否下架由管理员决定（不自动改 enabled）。
	if _, err := tx.ExecContext(ctx,
		`UPDATE studio_models SET missing_at_core = TRUE, updated_at = now() WHERE core_group_id = $1`,
		coreGroupID); err != nil {
		return fmt.Errorf("标记漂移失败: %w", err)
	}
	const upsert = `
		INSERT INTO studio_models (core_group_id, model_name, protocols, synced_at, updated_at)
		VALUES ($1, $2, $3, now(), now())
		ON CONFLICT (core_group_id, model_name) DO UPDATE
		SET protocols = EXCLUDED.protocols,
		    missing_at_core = FALSE,
		    synced_at = now(),
		    updated_at = now()`
	for _, m := range models {
		protocolsJSON, err := json.Marshal(m.Protocols)
		if err != nil {
			return fmt.Errorf("序列化 protocols 失败: %w", err)
		}
		if _, err := tx.ExecContext(ctx, upsert, coreGroupID, m.ModelName, protocolsJSON); err != nil {
			return fmt.Errorf("同步模型 %s 失败: %w", m.ModelName, err)
		}
	}
	return tx.Commit()
}

const shelfModelColumns = `id, core_group_id, model_name, display_name, protocols, enabled, sort_order, missing_at_core, synced_at`

func (s *pgShelfStore) ListModels(ctx context.Context, coreGroupID int64, onlyEnabled bool) ([]ShelfModel, error) {
	q := `SELECT ` + shelfModelColumns + ` FROM studio_models WHERE core_group_id = $1`
	if onlyEnabled {
		q += ` AND enabled`
	}
	q += ` ORDER BY sort_order, model_name`
	rows, err := s.db.QueryContext(ctx, q, coreGroupID)
	if err != nil {
		return nil, fmt.Errorf("查询模型列表失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ShelfModel
	for rows.Next() {
		m, err := scanShelfModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (s *pgShelfStore) GetModel(ctx context.Context, coreGroupID int64, modelName string) (*ShelfModel, error) {
	q := `SELECT ` + shelfModelColumns + ` FROM studio_models WHERE core_group_id = $1 AND model_name = $2`
	m, err := scanShelfModel(s.db.QueryRowContext(ctx, q, coreGroupID, modelName))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrShelfNotFound
	}
	return m, err
}

func (s *pgShelfStore) UpdateModel(ctx context.Context, id int64, patch ShelfModelPatch) (*ShelfModel, error) {
	q := `UPDATE studio_models
		SET display_name = COALESCE($2, display_name),
		    enabled = COALESCE($3, enabled),
		    sort_order = COALESCE($4, sort_order),
		    updated_at = now()
		WHERE id = $1
		RETURNING ` + shelfModelColumns
	m, err := scanShelfModel(s.db.QueryRowContext(ctx, q, id, patch.DisplayName, patch.Enabled, patch.SortOrder))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrShelfNotFound
	}
	return m, err
}

func scanShelfModel(row rowScanner) (*ShelfModel, error) {
	m := &ShelfModel{}
	var protocolsJSON []byte
	if err := row.Scan(&m.ID, &m.CoreGroupID, &m.ModelName, &m.DisplayName, &protocolsJSON,
		&m.Enabled, &m.SortOrder, &m.MissingAtCore, &m.SyncedAt); err != nil {
		return nil, err
	}
	if len(protocolsJSON) > 0 {
		if err := json.Unmarshal(protocolsJSON, &m.Protocols); err != nil {
			return nil, fmt.Errorf("解析 protocols 失败: %w", err)
		}
	}
	return m, nil
}
