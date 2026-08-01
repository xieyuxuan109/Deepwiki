# DeepWiki


一个代码仓库 → 可问答知识库的服务，基于 Go 语言、Eino 框架和通义千问大模型构建。
使用 PostgreSQL + pgvector 作为统一存储后端，支持向量相似度检索和关键词检索双模式。

## 功能特性

- **仓库摄取**: 从任意 Git 仓库拉取代码并自动建立向量索引
- **智能过滤**: 自动跳过 `.git/`、`vendor/`、`node_modules/`、二进制文件、超大文件（>1MB）
- **自定义规则**: 支持用户自定义 include/exclude 文件过滤规则
- **代码追溯**: 每段代码块记录来源文件、行号范围、编程语言
- **智能问答**: 基于代码上下文做 RAG 问答，答案中引用相关文件和行号
- **异步摄取**: 提交仓库立即返回任务 ID，支持轮询查询进度
- **流式输出**: 支持 SSE 流式问答，逐 token 返回
- **双检索模式**: 向量检索（pgvector）为主，Embedding 不可用时自动降级为关键词检索
- **持久化存储**: 全部数据存储在 PostgreSQL 中，重启不丢失
- **自动建表**: 服务启动时自动创建 pgvector 扩展、表结构和索引

## 技术栈

| 组件 | 选型 |
|------|------|
| 语言 | Go 1.25+ |
| Web 框架 | Gin |
| AI 框架 | Eino (CloudWeGo) |
| LLM | 通义千问（DashScope API，兼容 OpenAI 接口） |
| Git | go-git |
| 数据库 | PostgreSQL + pgvector |
| 环境变量 | godotenv（支持 .env 文件） |

## 快速开始

### 环境要求

- Go 1.25+
- PostgreSQL 14+（需安装 pgvector 扩展）

### 1. 准备 PostgreSQL

```bash
# 创建数据库
psql -U postgres -c "CREATE DATABASE deepwiki;"

# 安装 pgvector 扩展（如尚未安装）
psql -U postgres -d deepwiki -c "CREATE EXTENSION IF NOT EXISTS vector;"
```

> 服务启动时会自动检查并创建扩展和表结构，此步骤可选。

### 2. 配置环境变量

复制 `.env.example` 为 `.env` 并填入实际值：

```bash
cp .env.example .env
```

```env
# 通义千问 API 密钥（必填）
TONGYI_API_KEY=your_api_key_here

# 通义千问 API 地址（可选，默认 https://dashscope.aliyuncs.com/compatible-mode/v1）
# TONGYI_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1

# PostgreSQL 配置
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_DB=deepwiki
POSTGRES_USER=postgres
POSTGRES_PASSWORD=your_password_here
```

### 3. 安装依赖并启动

```bash
go mod tidy
go run ./cmd/server
```

启动后服务监听 `:8080`，日志输出：

```
正在初始化 LLM 客户端...
LLM 客户端初始化成功
正在初始化 PostgreSQL 存储...
PostgreSQL 存储初始化成功
服务初始化成功
服务启动成功，监听地址: :8080
```

## API 接口

### 1. 提交仓库摄取任务

```bash
POST /api/ingest
Content-Type: application/json

{
  "repo_url": "https://github.com/gin-gonic/gin",// https://gitee.com/mirrors/gin.git
  "include": ["*.go"],
  "exclude": ["testdata/*"]
}
```

**响应**:

```json
{
  "task_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

### 2. 查询摄取进度

```bash
GET /api/ingest/:id/status
```

**响应**:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "repo_url": "https://github.com/gin-gonic/gin",
  "status": "running",
  "progress": 0.5,
  "message": "已处理 10/20 个文件",
  "total_files": 20,
  "processed_files": 10
}
```

任务状态：`pending` → `running` → `completed` / `failed`

### 3. 问答（非流式）

```bash
POST /api/ask
Content-Type: application/json

{
  "repo_url": "https://github.com/gin-gonic/gin",
  "question": "如何使用 gin 创建一个 GET 路由？",
  "top_k": 5
}
```

**响应**:

```json
{
  "answer": "在 Gin 中创建 GET 路由非常简单...",
  "sources": [
    {
      "file_path": "gin.go",
      "start_line": 100,
      "end_line": 150,
      "language": "go",
      "score": 0.95
    }
  ]
}
```

### 4. 问答（流式 SSE）

```bash
POST /api/ask/stream
Content-Type: application/json

{
  "repo_url": "https://github.com/gin-gonic/gin",
  "question": "如何使用 gin 创建一个 GET 路由？",
  "top_k": 5
}
```

**响应**: SSE 流格式，依次发送 `message`（token）、`sources`（来源）、`done`（完成）事件。

### 5. 健康检查

```bash
GET /health
```

## 整体架构

```
┌─────────────────────────────────────────────────────────┐
│                    API Layer (Gin)                       │
│  POST /api/ingest  GET /api/ingest/:id/status           │
│  POST /api/ask      POST /api/ask/stream                 │
└──────────┬──────────────────┬───────────────────────────┘
           │                  │
           ▼                  ▼
┌─────────────────────────────────────────────────────────┐
│                  Service Layer                          │
│  ┌─────────────────┐    ┌─────────────────┐            │
│  │  Ingest Service  │    │   RAG Service    │            │
│  │  · Git 克隆      │    │  · 向量检索      │            │
│  │  · 文件扫描      │    │  · 上下文构建    │            │
│  │  · 代码分块      │    │  · LLM 生成      │            │
│  └────────┬────────┘    └────────┬────────┘            │
└───────────┼──────────────────────┼────────────────────┘
            │                      │
            ▼                      ▼
┌─────────────────────────────────────────────────────────┐
│               Storage Layer (PostgreSQL)                 │
│  ┌──────────────────────┐  ┌──────────────────────┐     │
│  │  deepwiki_chunks     │  │   ingest_tasks        │     │
│  │  · pgvector 向量检索  │  │   · 任务状态 CRUD     │     │
│  │  · HNSW 索引          │  │                      │     │
│  │  · 关键词降级检索      │  │                      │     │
│  └──────────────────────┘  └──────────────────────┘     │
│               LLM (通义千问 via Eino)                    │
└─────────────────────────────────────────────────────────┘
```

## 数据库结构

### deepwiki_chunks 表

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | VARCHAR(100) PK | 代码块唯一 ID |
| `content` | TEXT | 代码块内容 |
| `embedding` | vector(1536) | 向量嵌入（pgvector） |
| `repo_url` | VARCHAR(500) | 所属仓库地址 |
| `file_path` | VARCHAR(500) | 文件相对路径 |
| `language` | VARCHAR(50) | 编程语言 |
| `start_line` | INT | 起始行号 |
| `end_line` | INT | 结束行号 |
| `chunk_index` | INT | 块序号 |
| `metadata` | JSONB | 额外元数据 |
| `created_at` | TIMESTAMP | 创建时间 |

索引：
- `HNSW` 索引 on `embedding`（余弦距离，加速向量检索）
- `B-tree` 索引 on `repo_url`（加速仓库过滤）

### ingest_tasks 表

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | VARCHAR(100) PK | 任务 ID |
| `repo_url` | VARCHAR(500) | 目标仓库地址 |
| `status` | VARCHAR(20) | 任务状态 |
| `progress` | DOUBLE | 进度 0~1 |
| `message` | TEXT | 状态描述 |
| `total_files` | INT | 总文件数 |
| `processed_files` | INT | 已处理文件数 |
| `error` | TEXT | 错误信息 |
| `created_at` | TIMESTAMP | 创建时间 |
| `updated_at` | TIMESTAMP | 更新时间 |

## 设计说明

### 检索方式：双模式自动降级

- **向量检索**（默认）：当 Embedding 模型可用时，使用 pgvector 的余弦相似度检索，精度高
- **关键词检索**（降级）：当 Embedding 初始化失败时，使用 PostgreSQL 全文检索（`ts_rank`），保证基本可用

两种模式对外接口完全一致，业务层无需感知。

### 代码切分策略

- 固定 80 行/块，相邻块重叠 20 行，保证上下文连续性
- 每块包含文件路径和行号前缀，便于 LLM 引用

### 环境变量优先级

系统环境变量 > `.env` 文件。生产环境通过系统环境变量注入密钥，本地开发使用 `.env` 文件。

## 项目结构

```
deepwiki/
├── cmd/
│   └── server/
│       └── main.go              # 主入口
├── internal/
│   ├── api/
│   │   └── handler.go           # API 控制器
│   ├── ingest/
│   │   └── service.go           # 摄取服务（Git克隆、文件扫描、切分、索引）
│   ├── llm/
│   │   └── llm.go               # LLM 客户端（基于 Eino + 通义千问）
│   ├── models/
│   │   └── models.go            # 数据模型
│   ├── rag/
│   │   └── service.go           # RAG 服务（检索、提示词、生成）
│   └── storage/
│       ├── store.go             # VectorStore 接口定义 + PG 配置
│       ├── pg_store.go          # PostgreSQL 向量存储（pgvector）
│       └── pg_task_store.go     # PostgreSQL 任务存储
├── .env.example                 # 环境变量模板
├── .gitignore
├── go.mod
├── go.sum
└── README.md
```

## 许可证

MIT License
