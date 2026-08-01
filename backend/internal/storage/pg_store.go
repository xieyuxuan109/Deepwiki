package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgVectorStore struct {
	pool *pgxpool.Pool // PostgreSQL 连接池
	dim  int           // 向量维度
}

var _ VectorStore = (*PgVectorStore)(nil)

// NewPgVectorStore 创建一个新的 PostgreSQL 向量存储实例。
func NewPgVectorStore(pool *pgxpool.Pool, dim int) (*PgVectorStore, error) {
	vs := &PgVectorStore{
		pool: pool,
		dim:  dim,
	}

	if err := vs.ensureSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("ensure schema: %w", err)
	}

	return vs, nil
}

// ensureSchema 初始化数据库表结构和索引
func (vs *PgVectorStore) ensureSchema(ctx context.Context) error {
	if _, err := vs.pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		return fmt.Errorf("create extension: %w", err)
	}

	createTableSQL := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS deepwiki_chunks (
		id          VARCHAR(100) PRIMARY KEY,
		content     TEXT NOT NULL,
		embedding   vector(%d),
		repo_url    VARCHAR(500) NOT NULL,
		file_path   VARCHAR(500) NOT NULL,
		language    VARCHAR(50) NOT NULL,
		start_line  INT NOT NULL,
		end_line    INT NOT NULL,
		chunk_index INT NOT NULL,
		metadata    JSONB,
		created_at  TIMESTAMP DEFAULT NOW()
	)`, vs.dim)

	if _, err := vs.pool.Exec(ctx, createTableSQL); err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	if _, err := vs.pool.Exec(ctx,
		`CREATE INDEX IF NOT EXISTS idx_chunks_embedding ON deepwiki_chunks USING hnsw (embedding vector_cosine_ops)`); err != nil {
		return fmt.Errorf("create hnsw index: %w", err)
	}
	if _, err := vs.pool.Exec(ctx,
		`CREATE INDEX IF NOT EXISTS idx_chunks_repo_url ON deepwiki_chunks (repo_url)`); err != nil {
		return fmt.Errorf("create repo_url index: %w", err)
	}

	return nil
}

// Store 将文档批量插入 PostgreSQL。
func (vs *PgVectorStore) Store(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) ([]string, error) {
	options := indexer.GetCommonOptions(nil, opts...)

	// 生成向量嵌入（如果提供了 embedder）
	var embeddings [][]float64
	if options.Embedding != nil {
		var texts []string
		for _, doc := range docs {
			texts = append(texts, doc.Content)
		}

		var err error
		embeddings, err = options.Embedding.EmbedStrings(ctx, texts)
		if err != nil {
			return nil, fmt.Errorf("embed: %w", err)
		}
	}

	// 使用事务批量插入
	tx, err := vs.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	ids := make([]string, len(docs))
	for i, doc := range docs {
		id := doc.ID
		if id == "" {
			id = uuid.New().String()
		}
		ids[i] = id

		// 从元数据中提取字段
		repoURL, _ := doc.MetaData["repo_url"].(string)
		filePath, _ := doc.MetaData["file_path"].(string)
		language, _ := doc.MetaData["language"].(string)
		startLine := getIntFromMetaData(doc.MetaData, "start_line")
		endLine := getIntFromMetaData(doc.MetaData, "end_line")
		chunkIndex := getIntFromMetaData(doc.MetaData, "chunk_index")

		metadataJSON, _ := json.Marshal(doc.MetaData)

		// 构建向量字符串（pgvector 格式：[0.1,0.2,...]）
		var embeddingStr interface{}
		if embeddings != nil && i < len(embeddings) {
			embeddingStr = floatSliceToPgVector(embeddings[i])
		} else {
			embeddingStr = nil
		}

		_, err := tx.Exec(ctx,
			`INSERT INTO deepwiki_chunks (id, content, embedding, repo_url, file_path, language, start_line, end_line, chunk_index, metadata)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			 ON CONFLICT (id) DO UPDATE SET
			   content = EXCLUDED.content,
			   embedding = EXCLUDED.embedding,
			   repo_url = EXCLUDED.repo_url,
			   file_path = EXCLUDED.file_path,
			   language = EXCLUDED.language,
			   start_line = EXCLUDED.start_line,
			   end_line = EXCLUDED.end_line,
			   chunk_index = EXCLUDED.chunk_index,
			   metadata = EXCLUDED.metadata`,
			id, doc.Content, embeddingStr, repoURL, filePath, language,
			startLine, endLine, chunkIndex, metadataJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("insert chunk %s: %w", id, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return ids, nil
}

// Retrieve 在 PostgreSQL 中执行向量相似度检索。
// 如果提供了 Embedder，使用向量检索；否则降级为关键词匹配。
func (vs *PgVectorStore) Retrieve(ctx context.Context, query string, opts ...retriever.Option) ([]*schema.Document, error) {
	options := retriever.GetCommonOptions(&retriever.Options{
		TopK: intPtr(5),
	}, opts...)

	// 提取仓库过滤条件
	var repoFilter string
	if options.DSLInfo != nil {
		if v, ok := options.DSLInfo["repo_url"].(string); ok {
			repoFilter = v
		}
	}

	topK := 5
	if options.TopK != nil {
		topK = *options.TopK
	}

	// 判断是否使用向量检索
	if options.Embedding == nil {
		return vs.keywordRetrieve(ctx, query, repoFilter, topK)
	}

	// 生成查询向量
	queryEmbeddings, err := options.Embedding.EmbedStrings(ctx, []string{query})
	if err != nil {
		// 向量生成失败，降级为关键词匹配
		return vs.keywordRetrieve(ctx, query, repoFilter, topK)
	}

	queryVec := floatSliceToPgVector(queryEmbeddings[0])

	// 执行向量相似度检索（余弦距离）
	querySQL := `
		SELECT id, content, repo_url, file_path, language, start_line, end_line, metadata,
		       1 - (embedding <=> $1) AS score
		FROM deepwiki_chunks`

	args := []interface{}{queryVec}

	if repoFilter != "" {
		querySQL += ` WHERE repo_url = $2`
		args = append(args, repoFilter)
	}

	querySQL += ` ORDER BY embedding <=> $1 LIMIT $` + strconv.Itoa(len(args)+1)
	args = append(args, topK)

	rows, err := vs.pool.Query(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	return scanRowsToDocs(rows)
}

// keywordRetrieve 使用关键词匹配进行检索（向量检索的降级方案）。
func (vs *PgVectorStore) keywordRetrieve(ctx context.Context, query, repoFilter string, topK int) ([]*schema.Document, error) {
	querySQL := `
		SELECT id, content, repo_url, file_path, language, start_line, end_line, metadata,
		       ts_rank(to_tsvector('simple', content), plainto_tsquery('simple', $1)) AS score
		FROM deepwiki_chunks
		WHERE to_tsvector('simple', content) @@ plainto_tsquery('simple', $1)`

	args := []interface{}{query}
	argIdx := 2

	if repoFilter != "" {
		querySQL += fmt.Sprintf(` AND repo_url = $%d`, argIdx)
		args = append(args, repoFilter)
		argIdx++
	}

	querySQL += fmt.Sprintf(` ORDER BY score DESC LIMIT $%d`, argIdx)
	args = append(args, topK)

	rows, err := vs.pool.Query(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("keyword query: %w", err)
	}
	defer rows.Close()

	return scanRowsToDocs(rows)
}

// HasRepo 检查数据库中是否包含指定仓库的数据。
func (vs *PgVectorStore) HasRepo(repoURL string) bool {
	var exists bool
	err := vs.pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM deepwiki_chunks WHERE repo_url = $1)`, repoURL).Scan(&exists)
	if err != nil {
		return false
	}
	return exists
}

// scanRowsToDocs 将查询结果行扫描为 Document 列表。
func scanRowsToDocs(rows pgx.Rows) ([]*schema.Document, error) {
	var docs []*schema.Document
	for rows.Next() {
		var (
			id        string
			content   string
			repoURL   string
			filePath  string
			language  string
			startLine int
			endLine   int
			metadata  []byte
			score     float64
		)

		if err := rows.Scan(&id, &content, &repoURL, &filePath, &language,
			&startLine, &endLine, &metadata, &score); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		meta := make(map[string]any)
		if metadata != nil {
			_ = json.Unmarshal(metadata, &meta)
		}
		meta["repo_url"] = repoURL
		meta["file_path"] = filePath
		meta["language"] = language
		meta["start_line"] = startLine
		meta["end_line"] = endLine
		meta["score"] = score

		docs = append(docs, &schema.Document{
			ID:       id,
			Content:  content,
			MetaData: meta,
		})
	}

	return docs, rows.Err()
}

// floatSliceToPgVector 将 float64 切片转换为 pgvector 字符串格式。
// 例如：[0.1, 0.2, 0.3] -> "[0.1,0.2,0.3]"
func floatSliceToPgVector(vec []float64) string {
	parts := make([]string, len(vec))
	for i, v := range vec {
		parts[i] = strconv.FormatFloat(v, 'f', -1, 64)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// getIntFromMetaData 从元数据中安全地提取整数值。
func getIntFromMetaData(meta map[string]any, key string) int {
	v, ok := meta[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case float32:
		return int(val)
	default:
		return 0
	}
}

// intPtr 返回 int 的指针，用于 retriever.Options 的默认值。
func intPtr(i int) *int {
	return &i
}
