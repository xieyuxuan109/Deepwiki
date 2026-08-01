package llm

import (
	"context"
	"os"

	embedding_openai "github.com/cloudwego/eino-ext/components/embedding/openai"
	model_openai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/model"
)

const (
	defaultBaseURL    = "https://dashscope.aliyuncs.com/compatible-mode/v1" // 通义千问默认 API 地址
	defaultChatModel  = "qwen3.7-max"                                       // 默认聊天模型
	defaultEmbedModel = "qwen3.7-text-embedding"                            // 默认 Embedding 模型
)

// NewChatModel 创建一个新的聊天模型客户端，用于生成回答。
func NewChatModel(ctx context.Context) (model.ToolCallingChatModel, error) {
	apiKey := os.Getenv("TONGYI_API_KEY")
	baseURL := os.Getenv("TONGYI_BASE_URL")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	config := &model_openai.ChatModelConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   defaultChatModel,
	}

	return model_openai.NewChatModel(ctx, config)
}

func NewEmbeddingModel(ctx context.Context) (embedding.Embedder, error) {
	apiKey := os.Getenv("TONGYI_API_KEY")
	baseURL := os.Getenv("TONGYI_BASE_URL")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	config := &embedding_openai.EmbeddingConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   defaultEmbedModel,
	}

	return embedding_openai.NewEmbedder(ctx, config)
}
