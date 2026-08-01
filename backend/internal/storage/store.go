// Package storage 提供数据存储层，统一使用 PostgreSQL + pgvector。
// 包含两种存储：
//   - 向量存储（PgVectorStore）：存储代码块及其向量嵌入，支持相似度检索
//   - 任务存储（PgTaskStore）：存储摄取任务的状态信息
package storage

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

type VectorStore interface {
	Store(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) ([]string, error)
	Retrieve(ctx context.Context, query string, opts ...retriever.Option) ([]*schema.Document, error)
	HasRepo(repoURL string) bool
}

type PgConfig struct {
	Host     string
	Port     string
	Database string
	Username string
	Password string
}

func (c *PgConfig) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.Username, c.Password, c.Host, c.Port, c.Database)
}

func NewPgPool(cfg *PgConfig) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(context.Background(), cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("create pg connection pool: %w", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}
