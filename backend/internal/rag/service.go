// Package rag 提供检索增强生成（RAG）服务，负责将用户问题与代码库向量索引结合，
// 通过 LLM 生成基于代码上下文的智能回答。
// 核心流程：检索相关代码片段 -> 构建上下文 -> 调用 LLM 生成回答。
package rag

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"

	"deepwiki/internal/models"
	"deepwiki/internal/storage"
)

type Service struct {
	vectorStore storage.VectorStore
	chatModel   model.ToolCallingChatModel
	embedder    embedding.Embedder
	retriever   retriever.Retriever
}

type Config struct {
	VectorStore storage.VectorStore
	ChatModel   model.ToolCallingChatModel
	Embedder    embedding.Embedder
}

type AskInput struct {
	RepoURL  string
	Question string
	TopK     int
}

type AskOutput struct {
	Answer  string
	Sources []models.SourceRef
}

func NewService(cfg *Config) (*Service, error) {
	s := &Service{
		vectorStore: cfg.VectorStore,
		chatModel:   cfg.ChatModel,
		embedder:    cfg.Embedder,
		retriever:   cfg.VectorStore, // VectorStore 同时实现了 Retriever 接口
	}

	return s, nil
}

// Ask 执行同步问答，返回完整回答。
// 流程：检查仓库是否存在 -> 检索相关代码 -> 构建上下文 -> 调用 LLM。
func (s *Service) Ask(ctx context.Context, input *AskInput) (*AskOutput, error) {
	if !s.vectorStore.HasRepo(input.RepoURL) {
		return nil, fmt.Errorf("仓库尚未摄取，请先调用 /api/ingest")
	}

	topK := input.TopK
	if topK <= 0 {
		topK = 5
	}
	var retrieveOpts []retriever.Option
	if s.embedder != nil {
		retrieveOpts = append(retrieveOpts, retriever.WithEmbedding(s.embedder))
	}
	retrieveOpts = append(retrieveOpts,
		retriever.WithTopK(topK),
		retriever.WithDSLInfo(map[string]any{
			"repo_url": input.RepoURL,
		}),
	)

	docs, err := s.retriever.Retrieve(ctx, input.Question, retrieveOpts...)
	if err != nil {
		return nil, fmt.Errorf("检索失败: %w", err)
	}

	if len(docs) == 0 {
		return &AskOutput{
			Answer:  "未找到相关的代码片段，请尝试换一种问法。",
			Sources: nil,
		}, nil
	}

	context := buildContext(docs)
	sources := docsToSources(docs)

	messages := []*schema.Message{
		schema.SystemMessage("你是一个代码知识库助手，基于提供的代码上下文回答用户的问题。" +
			"请仔细阅读上下文，准确回答问题，并引用相关的代码文件和行号。" +
			"如果上下文中没有相关信息，请明确告知用户。"),
		schema.UserMessage(fmt.Sprintf("代码上下文:\n\n%s\n\n用户问题: %s", context, input.Question)),
	}

	resp, err := s.chatModel.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("生成回答失败: %w", err)
	}

	return &AskOutput{
		Answer:  resp.Content,
		Sources: sources,
	}, nil
}

// AskStream 执行流式问答，通过回调函数逐 token 返回内容。
// 适用于 SSE（Server-Sent Events）实时推送场景。
func (s *Service) AskStream(ctx context.Context, input *AskInput, onToken func(string)) ([]models.SourceRef, error) {
	if !s.vectorStore.HasRepo(input.RepoURL) {
		return nil, fmt.Errorf("仓库尚未摄取，请先调用 /api/ingest")
	}

	// 设置默认 TopK
	topK := input.TopK
	if topK <= 0 {
		topK = 5
	}

	// 构建检索选项
	var retrieveOpts []retriever.Option
	if s.embedder != nil {
		retrieveOpts = append(retrieveOpts, retriever.WithEmbedding(s.embedder))
	}
	retrieveOpts = append(retrieveOpts,
		retriever.WithTopK(topK),
		retriever.WithDSLInfo(map[string]any{
			"repo_url": input.RepoURL,
		}),
	)

	// 执行向量检索
	docs, err := s.retriever.Retrieve(ctx, input.Question, retrieveOpts...)
	if err != nil {
		return nil, fmt.Errorf("检索失败: %w", err)
	}

	// 无相关结果
	if len(docs) == 0 {
		onToken("未找到相关的代码片段，请尝试换一种问法。")
		return nil, nil
	}

	// 构建上下文和来源
	context := buildContext(docs)
	sources := docsToSources(docs)

	// 构建对话消息
	messages := []*schema.Message{
		schema.SystemMessage("你是一个代码知识库助手，基于提供的代码上下文回答用户的问题。" +
			"请仔细阅读上下文，准确回答问题，并引用相关的代码文件和行号。" +
			"如果上下文中没有相关信息，请明确告知用户。"),
		schema.UserMessage(fmt.Sprintf("代码上下文:\n\n%s\n\n用户问题: %s", context, input.Question)),
	}

	// 调用流式生成接口
	stream, err := s.chatModel.Stream(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("生成流式回答失败: %w", err)
	}
	defer stream.Close()

	// 逐 token 读取并回调
	for {
		msg, err := stream.Recv()
		if err != nil {
			if err.Error() == "EOF" || strings.Contains(err.Error(), "EOF") {
				break
			}
			return nil, fmt.Errorf("读取流失败: %w", err)
		}
		if msg == nil {
			continue
		}
		if msg.Content != "" {
			onToken(msg.Content)
		}
	}

	return sources, nil
}

// buildContext 将检索到的文档列表拼接成 LLM 可用的上下文字符串。
func buildContext(docs []*schema.Document) string {
	var parts []string
	for i, doc := range docs {
		score := 0.0
		if doc.MetaData != nil {
			if s, ok := doc.MetaData["score"].(float64); ok {
				score = s
			}
		}
		parts = append(parts, fmt.Sprintf("--- 文档 %d (相似度: %.4f) ---\n%s", i+1, score, doc.Content))
	}
	return strings.Join(parts, "\n\n")
}

// docsToSources 将检索结果转换为引用来源列表。
func docsToSources(docs []*schema.Document) []models.SourceRef {
	var sources []models.SourceRef
	for _, doc := range docs {
		ref := models.SourceRef{}
		if doc.MetaData != nil {
			if v, ok := doc.MetaData["file_path"].(string); ok {
				ref.FilePath = v
			}
			if v, ok := doc.MetaData["language"].(string); ok {
				ref.Language = v
			}
			ref.StartLine = getIntFromMeta(doc.MetaData, "start_line")
			ref.EndLine = getIntFromMeta(doc.MetaData, "end_line")
			if v, ok := doc.MetaData["score"].(float64); ok {
				ref.Score = v
			}
		}
		sources = append(sources, ref)
	}
	return sources
}

// getIntFromMeta 从元数据中安全地提取整数值，支持多种数值类型。
func getIntFromMeta(meta map[string]any, key string) int {
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
