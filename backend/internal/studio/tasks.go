package studio

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// 任务状态机：pending → processing → completed / failed（重试时 processing → pending）。
// cancelled 预留给未来的用户主动取消。
const (
	TaskStatusPending    = "pending"
	TaskStatusProcessing = "processing"
	TaskStatusCompleted  = "completed"
	TaskStatusFailed     = "failed"
	TaskStatusCancelled  = "cancelled"
)

// ErrTaskNotFound 任务不存在（或不属于该用户）。
var ErrTaskNotFound = errors.New("任务不存在")

// Task 本地生成任务，替代原插件架构下 core 托管的任务。
type Task struct {
	ID           int64
	PublicID     string // public_task_id，uuid
	UserID       int64  // studio_users.id
	TaskType     string // image.generate / image.edit
	Status       string
	Input        map[string]any
	Output       map[string]any
	ErrorMessage string
	Progress     int
	Attempts     int
	MaxAttempts  int
	CreatedAt    time.Time
	StartedAt    *time.Time
	CompletedAt  *time.Time
}

// TaskStore 任务仓储接口；HTTP 层与 worker 共用，单测用内存实现替换。
type TaskStore interface {
	// Create 落库新任务（status=pending）；回填 ID/PublicID/CreatedAt。
	Create(ctx context.Context, t *Task) error
	// GetByID 按 (userID, id) 查询；不存在返回 ErrTaskNotFound。
	GetByID(ctx context.Context, userID, id int64) (*Task, error)
	// List 按用户倒序分页；status 为空表示不过滤。返回当前页与总数。
	List(ctx context.Context, userID int64, status string, limit, offset int) ([]*Task, int, error)
	// Delete 删除任务并返回被删任务（供调用方清理产物文件）；不存在返回 ErrTaskNotFound。
	Delete(ctx context.Context, userID, id int64) (*Task, error)
	// ClaimNext 事务领取最早的 pending 任务并置 processing（FOR UPDATE SKIP LOCKED）；
	// 无可领任务返回 (nil, nil)。
	ClaimNext(ctx context.Context) (*Task, error)
	// Complete 写入 output 并置 completed。
	Complete(ctx context.Context, id int64, output map[string]any) error
	// Fail 记录失败：attempts+1；达到 max_attempts 置 failed，否则回 pending 等待重试。
	Fail(ctx context.Context, id int64, errMsg string) error
	// ResetProcessing 把遗留的 processing 任务重置回 pending（进程重启恢复，单实例假设）。
	ResetProcessing(ctx context.Context) (int64, error)
}

// pgTaskStore TaskStore 的 Postgres 实现。
type pgTaskStore struct {
	db *sql.DB
}

// NewPGTaskStore 创建 Postgres 任务仓储。
func NewPGTaskStore(db *sql.DB) TaskStore {
	return &pgTaskStore{db: db}
}

const taskColumns = `id, public_task_id, user_id, task_type, status, input, output,
	error_message, progress, attempts, max_attempts, created_at, started_at, completed_at`

func (s *pgTaskStore) Create(ctx context.Context, t *Task) error {
	if t.PublicID == "" {
		t.PublicID = uuid.NewString()
	}
	if t.MaxAttempts <= 0 {
		t.MaxAttempts = 3
	}
	inputJSON, err := marshalJSONMap(t.Input)
	if err != nil {
		return fmt.Errorf("序列化任务输入失败: %w", err)
	}
	const q = `
		INSERT INTO studio_tasks (public_task_id, user_id, task_type, status, input, max_attempts)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`
	t.Status = TaskStatusPending
	if err := s.db.QueryRowContext(ctx, q, t.PublicID, t.UserID, t.TaskType, t.Status, inputJSON, t.MaxAttempts).
		Scan(&t.ID, &t.CreatedAt); err != nil {
		return fmt.Errorf("创建任务失败: %w", err)
	}
	return nil
}

func (s *pgTaskStore) GetByID(ctx context.Context, userID, id int64) (*Task, error) {
	q := `SELECT ` + taskColumns + ` FROM studio_tasks WHERE id = $1 AND user_id = $2`
	t, err := scanTask(s.db.QueryRowContext(ctx, q, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询任务失败: %w", err)
	}
	return t, nil
}

func (s *pgTaskStore) List(ctx context.Context, userID int64, status string, limit, offset int) ([]*Task, int, error) {
	where := `WHERE user_id = $1`
	args := []any{userID}
	if status != "" {
		where += ` AND status = $2`
		args = append(args, status)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM studio_tasks `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计任务失败: %w", err)
	}

	q := fmt.Sprintf(`SELECT %s FROM studio_tasks %s ORDER BY created_at DESC, id DESC LIMIT %d OFFSET %d`,
		taskColumns, where, limit, offset)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("查询任务列表失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tasks []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("解析任务行失败: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("遍历任务列表失败: %w", err)
	}
	return tasks, total, nil
}

func (s *pgTaskStore) Delete(ctx context.Context, userID, id int64) (*Task, error) {
	q := `DELETE FROM studio_tasks WHERE id = $1 AND user_id = $2 RETURNING ` + taskColumns
	t, err := scanTask(s.db.QueryRowContext(ctx, q, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("删除任务失败: %w", err)
	}
	return t, nil
}

func (s *pgTaskStore) ClaimNext(ctx context.Context) (*Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("开启领取事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	q := `SELECT ` + taskColumns + ` FROM studio_tasks
		WHERE status = '` + TaskStatusPending + `'
		ORDER BY id
		FOR UPDATE SKIP LOCKED
		LIMIT 1`
	t, err := scanTask(tx.QueryRowContext(ctx, q))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("领取任务失败: %w", err)
	}

	const upd = `UPDATE studio_tasks
		SET status = $2, started_at = now(), progress = 50
		WHERE id = $1
		RETURNING started_at`
	if err := tx.QueryRowContext(ctx, upd, t.ID, TaskStatusProcessing).Scan(&t.StartedAt); err != nil {
		return nil, fmt.Errorf("标记任务执行中失败: %w", err)
	}
	t.Status = TaskStatusProcessing
	t.Progress = 50

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交领取事务失败: %w", err)
	}
	return t, nil
}

func (s *pgTaskStore) Complete(ctx context.Context, id int64, output map[string]any) error {
	outputJSON, err := marshalJSONMap(output)
	if err != nil {
		return fmt.Errorf("序列化任务输出失败: %w", err)
	}
	const q = `UPDATE studio_tasks
		SET status = $2, output = $3, progress = 100, error_message = '', completed_at = now()
		WHERE id = $1`
	if _, err := s.db.ExecContext(ctx, q, id, TaskStatusCompleted, outputJSON); err != nil {
		return fmt.Errorf("完成任务失败: %w", err)
	}
	return nil
}

func (s *pgTaskStore) Fail(ctx context.Context, id int64, errMsg string) error {
	// attempts+1 后达到 max_attempts 则终态 failed，否则回 pending 等下一轮重试。
	const q = `UPDATE studio_tasks
		SET attempts = attempts + 1,
		    error_message = $2,
		    status = CASE WHEN attempts + 1 >= max_attempts THEN 'failed' ELSE 'pending' END,
		    progress = CASE WHEN attempts + 1 >= max_attempts THEN 100 ELSE 0 END,
		    completed_at = CASE WHEN attempts + 1 >= max_attempts THEN now() ELSE NULL END
		WHERE id = $1`
	if _, err := s.db.ExecContext(ctx, q, id, errMsg); err != nil {
		return fmt.Errorf("记录任务失败状态失败: %w", err)
	}
	return nil
}

func (s *pgTaskStore) ResetProcessing(ctx context.Context) (int64, error) {
	const q = `UPDATE studio_tasks SET status = 'pending', started_at = NULL, progress = 0
		WHERE status = 'processing'`
	res, err := s.db.ExecContext(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("重置遗留任务失败: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// rowScanner 兼容 *sql.Row 与 *sql.Rows 的扫描接口。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(row rowScanner) (*Task, error) {
	t := &Task{}
	var (
		inputJSON   []byte
		outputJSON  []byte
		startedAt   sql.NullTime
		completedAt sql.NullTime
	)
	if err := row.Scan(
		&t.ID, &t.PublicID, &t.UserID, &t.TaskType, &t.Status, &inputJSON, &outputJSON,
		&t.ErrorMessage, &t.Progress, &t.Attempts, &t.MaxAttempts, &t.CreatedAt, &startedAt, &completedAt,
	); err != nil {
		return nil, err
	}
	if len(inputJSON) > 0 {
		if err := json.Unmarshal(inputJSON, &t.Input); err != nil {
			return nil, fmt.Errorf("解析任务输入失败: %w", err)
		}
	}
	if len(outputJSON) > 0 {
		if err := json.Unmarshal(outputJSON, &t.Output); err != nil {
			return nil, fmt.Errorf("解析任务输出失败: %w", err)
		}
	}
	if startedAt.Valid {
		t.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		t.CompletedAt = &completedAt.Time
	}
	return t, nil
}

func marshalJSONMap(m map[string]any) ([]byte, error) {
	if m == nil {
		m = map[string]any{}
	}
	return json.Marshal(m)
}
