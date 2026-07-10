package studio

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	// Postgres 驱动注册。
	_ "github.com/lib/pq"
)

// OpenDB 建立 Postgres 连接并做连通性检查。
func OpenDB(ctx context.Context, databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("数据库连通性检查失败: %w", err)
	}
	return db, nil
}

// migrations 幂等建表语句，启动时顺序执行。
var migrations = []string{
	`CREATE TABLE IF NOT EXISTS studio_users (
		id BIGSERIAL PRIMARY KEY,
		airgate_user_id BIGINT NOT NULL UNIQUE,
		email TEXT NOT NULL DEFAULT '',
		username TEXT NOT NULL DEFAULT '',
		api_key TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
	`CREATE TABLE IF NOT EXISTS studio_tasks (
		id BIGSERIAL PRIMARY KEY,
		public_task_id TEXT NOT NULL UNIQUE,
		user_id BIGINT NOT NULL,
		task_type TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		input JSONB NOT NULL DEFAULT '{}'::jsonb,
		output JSONB,
		error_message TEXT NOT NULL DEFAULT '',
		progress INT NOT NULL DEFAULT 0,
		attempts INT NOT NULL DEFAULT 0,
		max_attempts INT NOT NULL DEFAULT 3,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		started_at TIMESTAMPTZ,
		completed_at TIMESTAMPTZ
	)`,
	`CREATE INDEX IF NOT EXISTS idx_studio_tasks_user_created ON studio_tasks (user_id, created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_studio_tasks_status_created ON studio_tasks (status, created_at)`,
	// 分组货架：core 分组镜像（管理员开关决定对用户开放哪些组）。
	`CREATE TABLE IF NOT EXISTS studio_groups (
		core_group_id BIGINT PRIMARY KEY,
		name TEXT NOT NULL DEFAULT '',
		rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1,
		note TEXT NOT NULL DEFAULT '',
		enabled BOOLEAN NOT NULL DEFAULT FALSE,
		synced_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
	// 模型货架：按分组同步自 core /v1/models，管理员挑选上架。
	`CREATE TABLE IF NOT EXISTS studio_models (
		id BIGSERIAL PRIMARY KEY,
		core_group_id BIGINT NOT NULL,
		model_name TEXT NOT NULL,
		display_name TEXT NOT NULL DEFAULT '',
		protocols JSONB NOT NULL DEFAULT '[]'::jsonb,
		modality TEXT NOT NULL DEFAULT 'image',
		enabled BOOLEAN NOT NULL DEFAULT FALSE,
		sort_order INT NOT NULL DEFAULT 0,
		missing_at_core BOOLEAN NOT NULL DEFAULT FALSE,
		synced_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		UNIQUE (core_group_id, model_name)
	)`,
	// 存量部署补列：模型模态（image/video/music），同步时按模型名启发式预填。
	`ALTER TABLE studio_models ADD COLUMN IF NOT EXISTS modality TEXT NOT NULL DEFAULT 'image'`,
	// 用户按分组领取的 sk- key（登录回调自动 provision；仅后端持有）。
	`CREATE TABLE IF NOT EXISTS studio_user_keys (
		user_id BIGINT NOT NULL,
		core_group_id BIGINT NOT NULL,
		api_key TEXT NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		PRIMARY KEY (user_id, core_group_id)
	)`,
}

// Migrate 执行幂等迁移（CREATE IF NOT EXISTS），失败即返回错误让启动中止。
func Migrate(ctx context.Context, db *sql.DB) error {
	for _, stmt := range migrations {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("迁移失败: %w\nSQL: %s", err, stmt)
		}
	}
	return nil
}
