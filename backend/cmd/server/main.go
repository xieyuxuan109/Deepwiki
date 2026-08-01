// Package main 是 DeepWiki 服务的入口点。
// DeepWiki 是一个基于 RAG（检索增强生成）的代码知识库问答系统，
// 支持从 Git 仓库摄取代码，并通过 LLM 提供智能问答能力。
package main

import (
	"context"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"github.com/cloudwego/eino/components/embedding"

	"deepwiki/internal/api"
	"deepwiki/internal/ingest"
	"deepwiki/internal/llm"
	"deepwiki/internal/rag"
	"deepwiki/internal/storage"
)

// 初始化流程：
//  0. 从 .env 文件加载环境变量
//  1. 从环境变量读取 API 密钥
//  2. 初始化 LLM 客户端
//  3. 初始化 PostgreSQL 连接池 + 向量存储 + 任务存储
//  4. 初始化摄取服务和 RAG 服务
//  5. 设置 Gin HTTP 路由和中间件
//  6. 启动服务器监听 :8080
func main() {
	if err := godotenv.Load(".env"); err != nil {
		if err := godotenv.Load("../../.env"); err != nil {
			log.Println("未找到 .env 文件，将使用系统环境变量")
		}
	}
	ctx := context.Background()

	apiKey := os.Getenv("TONGYI_API_KEY")
	if apiKey == "" {
		log.Fatal("TONGYI_API_KEY 环境变量未设置")
	}

	log.Println("正在初始化 LLM 客户端...")
	chatModel, err := llm.NewChatModel(ctx)
	if err != nil {
		log.Fatalf("初始化 ChatModel 失败: %v", err)
	}

	var embedder embedding.Embedder
	if os.Getenv("ENABLE_EMBEDDING") != "false" {
		embedder, err = llm.NewEmbeddingModel(ctx)
		if err != nil {
			log.Printf("警告: 初始化 Embedding 模型失败，将使用关键词匹配: %v", err)
			embedder = nil
		}
	} else {
		log.Println("已禁用 Embedding（ENABLE_EMBEDDING=false），使用关键词检索模式")
	}
	log.Println("LLM 客户端初始化成功")
	log.Println("正在初始化 PostgreSQL 存储...")
	pgCfg := &storage.PgConfig{
		Host:     getEnvOrDefault("POSTGRES_HOST", "localhost"),
		Port:     getEnvOrDefault("POSTGRES_PORT", "5432"),
		Database: getEnvOrDefault("POSTGRES_DB", "deepwiki"),
		Username: getEnvOrDefault("POSTGRES_USER", "postgres"),
		Password: os.Getenv("POSTGRES_PASSWORD"),
	}

	pool, err := storage.NewPgPool(pgCfg)
	if err != nil {
		log.Fatalf("连接 PostgreSQL 失败: %v", err)
	}
	defer pool.Close()

	vectorStore, err := storage.NewPgVectorStore(pool, 1024)
	if err != nil {
		log.Fatalf("初始化向量存储失败: %v", err)
	}

	taskStore, err := storage.NewPgTaskStore(pool)
	if err != nil {
		log.Fatalf("初始化任务存储失败: %v", err)
	}
	log.Println("PostgreSQL 存储初始化成功")

	log.Println("正在初始化服务...")
	ingestSvc := ingest.NewService(&ingest.Config{
		TaskStore:   taskStore,
		VectorStore: vectorStore,
		Embedder:    embedder,
		ReposPath:   "../../data/repos",
	})

	ragSvc, err := rag.NewService(&rag.Config{
		VectorStore: vectorStore,
		ChatModel:   chatModel,
		Embedder:    embedder,
	})
	if err != nil {
		log.Fatalf("初始化 RAG 服务失败: %v", err)
	}
	log.Println("服务初始化成功")

	r := gin.Default()

	// CORS 跨域中间件，允许前端访问
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	handler := api.NewHandler(ingestSvc, ragSvc)
	handler.SetupRoutes(r)

	// 健康检查端点
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
		})
	})

	addr := ":8080"
	log.Printf("服务启动成功，监听地址: %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("启动服务失败: %v", err)
	}
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
