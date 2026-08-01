package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"deepwiki/internal/models"
)

// PgTaskStore 是基于 PostgreSQL 的摄取任务存储实现。
// 任务状态持久化在 PostgreSQL 的 ingest_tasks 表中。
type PgTaskStore struct {
	pool *pgxpool.Pool
}

// NewPgTaskStore 创建一个新的 PostgreSQL 任务存储实例。
// 会自动创建表结构
func NewPgTaskStore(pool *pgxpool.Pool) (*PgTaskStore, error) {
	ts := &PgTaskStore{pool: pool}

	if err := ts.ensureSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("ensure schema: %w", err)
	}

	return ts, nil
}

// ensureSchema 初始化任务表结构
func (ts *PgTaskStore) ensureSchema(ctx context.Context) error {
	_, err := ts.pool.Exec(ctx, `
	CREATE TABLE IF NOT EXISTS ingest_tasks (
		id              VARCHAR(100) PRIMARY KEY,
		repo_url        VARCHAR(500) NOT NULL,
		status          VARCHAR(20) NOT NULL,
		progress        DOUBLE PRECISION NOT NULL DEFAULT 0,
		message         TEXT,
		total_files     INT NOT NULL DEFAULT 0,
		processed_files INT NOT NULL DEFAULT 0,
		error           TEXT,
		created_at      TIMESTAMP NOT NULL,
		updated_at      TIMESTAMP NOT NULL
	)`)

	if err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	// 创建状态查询索引
	_, err = ts.pool.Exec(ctx,
		`CREATE INDEX IF NOT EXISTS idx_tasks_status ON ingest_tasks (status)`)
	if err != nil {
		return fmt.Errorf("create index: %w", err)
	}

	return nil
}

// Create 创建一个新任务并持久化到数据库。
func (ts *PgTaskStore) Create(task *models.IngestTask) error {
	task.CreatedAt = time.Now()
	task.UpdatedAt = time.Now()

	_, err := ts.pool.Exec(context.Background(),
		`INSERT INTO ingest_tasks (id, repo_url, status, progress, message, total_files, processed_files, error, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		task.ID, task.RepoURL, task.Status, task.Progress, task.Message,
		task.TotalFiles, task.ProcessedFiles, task.Error, task.CreatedAt, task.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}

	return nil
}

// Update 更新已有任务的状态并持久化到数据库。
func (ts *PgTaskStore) Update(task *models.IngestTask) error {
	task.UpdatedAt = time.Now()

	_, err := ts.pool.Exec(context.Background(),
		`UPDATE ingest_tasks SET
			status = $1, progress = $2, message = $3,
			total_files = $4, processed_files = $5, error = $6,
			updated_at = $7
		 WHERE id = $8`,
		task.Status, task.Progress, task.Message,
		task.TotalFiles, task.ProcessedFiles, task.Error,
		task.UpdatedAt, task.ID,
	)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}

	return nil
}

// Get 根据任务 ID 查询任务状态。
func (ts *PgTaskStore) Get(id string) (*models.IngestTask, bool) {
	var task models.IngestTask
	var errorStr *string

	err := ts.pool.QueryRow(context.Background(),
		`SELECT id, repo_url, status, progress, message, total_files, processed_files, error, created_at, updated_at
		 FROM ingest_tasks WHERE id = $1`, id).Scan(
		&task.ID, &task.RepoURL, &task.Status, &task.Progress, &task.Message,
		&task.TotalFiles, &task.ProcessedFiles, &errorStr,
		&task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		return nil, false
	}

	if errorStr != nil {
		task.Error = *errorStr
	}

	return &task, true
}
