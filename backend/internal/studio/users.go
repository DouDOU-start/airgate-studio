package studio

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrUserNotFound 用户不存在。
var ErrUserNotFound = errors.New("用户不存在")

// User 本地用户，映射 core 用户（airgate_user_id 唯一）并持有其 sk- key。
type User struct {
	ID            int64
	AirgateUserID int64
	Email         string
	Username      string
	// APIKey 默认分组的 sk- key 明文（登录回调 provision group_id=0 所得），仅本服务
	// 后端持有，永不通过任何 API 响应下发到浏览器。按组的 key 另存 studio_user_keys。
	APIKey    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UserStore 用户仓储接口；worker 与 HTTP 层都经此取数，便于单测替换。
type UserStore interface {
	// Upsert 按 airgate_user_id 幂等插入/更新（登录回调时调用）。
	Upsert(ctx context.Context, airgateUserID int64, email, username, apiKey string) (*User, error)
	// GetByID 按本地主键查询；不存在返回 ErrUserNotFound。
	GetByID(ctx context.Context, id int64) (*User, error)
	// UpsertKey 写入用户在某 core 分组的 sk- key（登录回调按组 provision 后调用）。
	UpsertKey(ctx context.Context, userID, coreGroupID int64, apiKey string) error
	// KeysByUser 返回用户全部按组 key（core_group_id → key 明文）。
	KeysByUser(ctx context.Context, userID int64) (map[int64]string, error)
}

// pgUserStore UserStore 的 Postgres 实现。
type pgUserStore struct {
	db *sql.DB
}

// NewPGUserStore 创建 Postgres 用户仓储。
func NewPGUserStore(db *sql.DB) UserStore {
	return &pgUserStore{db: db}
}

func (s *pgUserStore) Upsert(ctx context.Context, airgateUserID int64, email, username, apiKey string) (*User, error) {
	const q = `
		INSERT INTO studio_users (airgate_user_id, email, username, api_key, created_at, updated_at)
		VALUES ($1, $2, $3, $4, now(), now())
		ON CONFLICT (airgate_user_id) DO UPDATE
		SET email = EXCLUDED.email,
		    username = EXCLUDED.username,
		    api_key = EXCLUDED.api_key,
		    updated_at = now()
		RETURNING id, airgate_user_id, email, username, api_key, created_at, updated_at`
	u := &User{}
	err := s.db.QueryRowContext(ctx, q, airgateUserID, email, username, apiKey).
		Scan(&u.ID, &u.AirgateUserID, &u.Email, &u.Username, &u.APIKey, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert 用户失败: %w", err)
	}
	return u, nil
}

func (s *pgUserStore) UpsertKey(ctx context.Context, userID, coreGroupID int64, apiKey string) error {
	const q = `
		INSERT INTO studio_user_keys (user_id, core_group_id, api_key, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (user_id, core_group_id) DO UPDATE
		SET api_key = EXCLUDED.api_key, updated_at = now()`
	if _, err := s.db.ExecContext(ctx, q, userID, coreGroupID, apiKey); err != nil {
		return fmt.Errorf("写入用户分组 key 失败: %w", err)
	}
	return nil
}

func (s *pgUserStore) KeysByUser(ctx context.Context, userID int64) (map[int64]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT core_group_id, api_key FROM studio_user_keys WHERE user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("查询用户分组 key 失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[int64]string)
	for rows.Next() {
		var groupID int64
		var apiKey string
		if err := rows.Scan(&groupID, &apiKey); err != nil {
			return nil, fmt.Errorf("解析用户分组 key 失败: %w", err)
		}
		out[groupID] = apiKey
	}
	return out, rows.Err()
}

func (s *pgUserStore) GetByID(ctx context.Context, id int64) (*User, error) {
	const q = `
		SELECT id, airgate_user_id, email, username, api_key, created_at, updated_at
		FROM studio_users WHERE id = $1`
	u := &User{}
	err := s.db.QueryRowContext(ctx, q, id).
		Scan(&u.ID, &u.AirgateUserID, &u.Email, &u.Username, &u.APIKey, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}
	return u, nil
}
