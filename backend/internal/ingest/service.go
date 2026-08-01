package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	"github.com/go-git/go-git/v5"
	"github.com/google/uuid"

	"deepwiki/internal/models"
	"deepwiki/internal/storage"
)

type Service struct {
	taskStore   *storage.PgTaskStore
	vectorStore storage.VectorStore // 向量存储接口
	embedder    embedding.Embedder  // Embedding 模型，用于生成向量（可能为 nil）
	reposPath   string              // 仓库临时克隆目录

	mu    sync.Mutex                    // 保护 tasks map 的互斥锁
	tasks map[string]context.CancelFunc // 运行中任务的取消函数映射
}

// Config 是创建 Service 所需的配置参数。
type Config struct {
	TaskStore   *storage.PgTaskStore
	VectorStore storage.VectorStore
	Embedder    embedding.Embedder
	ReposPath   string
}

// NewService 创建一个新的摄取服务实例。
func NewService(cfg *Config) *Service {
	if cfg.ReposPath == "" {
		cfg.ReposPath = "../../data/repos"
	}
	os.MkdirAll(cfg.ReposPath, 0755)

	return &Service{
		taskStore:   cfg.TaskStore,
		vectorStore: cfg.VectorStore,
		embedder:    cfg.Embedder,
		reposPath:   cfg.ReposPath,
		tasks:       make(map[string]context.CancelFunc),
	}
}

func (s *Service) StartIngest(repoURL string, include, exclude []string) (string, error) {
	taskID := uuid.New().String()

	task := &models.IngestTask{
		ID:       taskID,
		RepoURL:  repoURL,
		Status:   models.TaskStatusPending,
		Progress: 0,
		Message:  "任务已创建，等待执行",
	}

	if err := s.taskStore.Create(task); err != nil {
		return "", fmt.Errorf("create task: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.tasks[taskID] = cancel
	s.mu.Unlock()

	go s.runIngest(ctx, task, include, exclude)

	return taskID, nil
}

// GetTaskStatus 查询指定任务 ID 的当前状态。
func (s *Service) GetTaskStatus(taskID string) (*models.IngestTask, bool) {
	return s.taskStore.Get(taskID)
}

// 流程：克隆仓库 -> 扫描文件 -> 分块 -> 生成向量索引。
func (s *Service) runIngest(ctx context.Context, task *models.IngestTask, include, exclude []string) {
	defer func() {
		s.mu.Lock()
		delete(s.tasks, task.ID)
		s.mu.Unlock()
	}()

	task.Status = models.TaskStatusRunning
	task.Message = "正在克隆仓库..."
	s.taskStore.Update(task)

	repoDir, err := s.cloneRepo(ctx, task.RepoURL)
	if err != nil {
		task.Status = models.TaskStatusFailed
		task.Error = fmt.Sprintf("克隆仓库失败: %v", err)
		task.Message = "克隆仓库失败"
		s.taskStore.Update(task)
		return
	}
	defer os.RemoveAll(repoDir) // 清理临时目录

	// 步骤 2: 扫描符合条件的文件
	task.Message = "正在扫描文件..."
	s.taskStore.Update(task)

	files, err := s.scanFiles(repoDir, include, exclude)
	if err != nil {
		task.Status = models.TaskStatusFailed
		task.Error = fmt.Sprintf("扫描文件失败: %v", err)
		task.Message = "扫描文件失败"
		s.taskStore.Update(task)
		return
	}

	task.TotalFiles = len(files)
	task.Message = fmt.Sprintf("找到 %d 个文件，开始处理...", len(files))
	s.taskStore.Update(task)

	// 步骤 3: 读取文件并分块
	var docs []*schema.Document
	processed := 0

	for _, file := range files {
		// 检查任务是否被取消
		select {
		case <-ctx.Done():
			task.Status = models.TaskStatusFailed
			task.Error = "任务被取消"
			s.taskStore.Update(task)
			return
		default:
		}

		relPath, _ := filepath.Rel(repoDir, file)
		lang := detectLanguage(file)

		content, err := os.ReadFile(file)
		if err != nil {
			processed++
			continue
		}

		// 将文件内容分块（80 行/块，20 行重叠）
		chunks := splitCode(string(content), relPath, lang)

		for i, chunk := range chunks {
			doc := &schema.Document{
				ID:      uuid.New().String(),
				Content: chunk.content,
				MetaData: map[string]any{
					"repo_url":    task.RepoURL,
					"file_path":   relPath,
					"language":    lang,
					"start_line":  chunk.startLine,
					"end_line":    chunk.endLine,
					"chunk_index": i,
				},
			}
			docs = append(docs, doc)
		}

		processed++
		task.ProcessedFiles = processed
		task.Progress = float64(processed) / float64(len(files)) * 0.7 // 分块占 70% 进度
		task.Message = fmt.Sprintf("已处理 %d/%d 个文件", processed, len(files))
		s.taskStore.Update(task)
	}

	// 步骤 4: 批量存储到向量数据库
	task.Message = "正在生成向量索引..."
	task.Progress = 0.8
	s.taskStore.Update(task)

	batchSize := 20
	for i := 0; i < len(docs); i += batchSize {
		end := i + batchSize
		if end > len(docs) {
			end = len(docs)
		}

		// 检查取消
		select {
		case <-ctx.Done():
			task.Status = models.TaskStatusFailed
			task.Error = "任务被取消"
			s.taskStore.Update(task)
			return
		default:
		}

		// 如果有 embedder，则使用 Embedding 存储
		var storeOpts []indexer.Option
		if s.embedder != nil {
			storeOpts = append(storeOpts, indexer.WithEmbedding(s.embedder))
		}
		_, err := s.vectorStore.Store(ctx, docs[i:end], storeOpts...)
		if err != nil {
			task.Status = models.TaskStatusFailed
			task.Error = fmt.Sprintf("生成索引失败: %v", err)
			s.taskStore.Update(task)
			return
		}

		task.Progress = 0.8 + 0.2*float64(end)/float64(len(docs))
		s.taskStore.Update(task)
	}

	// 任务完成
	task.Status = models.TaskStatusCompleted
	task.Progress = 1.0
	task.Message = fmt.Sprintf("摄取完成，共处理 %d 个文件，%d 个代码块", len(files), len(docs))
	s.taskStore.Update(task)
}

func (s *Service) cloneRepo(ctx context.Context, repoURL string) (string, error) {
	repoName := filepath.Base(strings.TrimSuffix(repoURL, ".git"))
	repoDir := filepath.Join(s.reposPath, repoName+"_"+uuid.New().String()[:8])

	_, err := git.PlainCloneContext(ctx, repoDir, false, &git.CloneOptions{
		URL:      repoURL,
		Depth:    1,
		Progress: os.Stdout,
	})
	if err != nil {
		return "", fmt.Errorf("clone: %w", err)
	}

	return repoDir, nil
}

var defaultExclude = []string{
	".git",
	"vendor",
	"node_modules",
	".idea",
	".vscode",
	"__pycache__",
	".next",
	".nuxt",
	"dist",
	"build",
	"target",
}

var defaultExcludeExts = map[string]bool{
	".png":    true,
	".jpg":    true,
	".jpeg":   true,
	".gif":    true,
	".ico":    true,
	".svg":    true,
	".woff":   true,
	".woff2":  true,
	".ttf":    true,
	".eot":    true,
	".pdf":    true,
	".zip":    true,
	".tar":    true,
	".gz":     true,
	".rar":    true,
	".exe":    true,
	".dll":    true,
	".so":     true,
	".dylib":  true,
	".bin":    true,
	".dat":    true,
	".db":     true,
	".sqlite": true,
	".mp3":    true,
	".mp4":    true,
	".avi":    true,
	".mov":    true,
}

// scanFiles 递归扫描目录，返回符合过滤条件的文件列表。
func (s *Service) scanFiles(root string, include, exclude []string) ([]string, error) {
	excludePatterns := append(defaultExclude, exclude...)

	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			name := info.Name()
			for _, ex := range excludePatterns {
				if name == ex {
					return filepath.SkipDir
				}
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if defaultExcludeExts[ext] {
			return nil
		}

		if info.Size() > 1024 {
			return nil
		}

		relPath, _ := filepath.Rel(root, path)

		if len(include) > 0 {
			matched := false
			for _, inc := range include {
				if ok, _ := filepath.Match(inc, relPath); ok {
					matched = true
					break
				}
				if strings.HasPrefix(relPath, inc) {
					matched = true
					break
				}
			}
			if !matched {
				return nil
			}
		}

		if len(exclude) > 0 {
			for _, ex := range exclude {
				if ok, _ := filepath.Match(ex, relPath); ok {
					return nil
				}
				if strings.HasPrefix(relPath, ex) {
					return nil
				}
			}
		}

		files = append(files, path)
		return nil
	})

	return files, err
}

// codeChunk 表示一个代码分块，包含内容和行号范围。
type codeChunk struct {
	content   string // 分块内容（包含文件路径和行号前缀）
	startLine int    // 起始行号（从 1 开始）
	endLine   int    // 结束行号
}

// splitCode 将文件内容按固定行数分块，块之间有重叠以保证上下文连续性。
// 参数:
//   - content: 文件完整内容
//   - filePath: 文件相对路径（用于标注）
//   - language: 编程语言（用于标注）
//
// 返回: 代码块列表
func splitCode(content, filePath, language string) []codeChunk {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return nil
	}

	chunkSize := 80 // 每块最大行数
	overlap := 20   // 相邻块之间的重叠行数

	var chunks []codeChunk
	for i := 0; i < len(lines); i += chunkSize - overlap {
		end := i + chunkSize
		if end > len(lines) {
			end = len(lines)
		}

		chunkLines := lines[i:end]
		chunk := codeChunk{
			content:   fmt.Sprintf("File: %s\nLines %d-%d\n\n%s", filePath, i+1, end, strings.Join(chunkLines, "\n")),
			startLine: i + 1,
			endLine:   end,
		}
		chunks = append(chunks, chunk)

		if end >= len(lines) {
			break
		}
	}

	return chunks
}

// detectLanguage 根据文件扩展名检测编程语言
func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	langMap := map[string]string{
		".go":    "go",
		".py":    "python",
		".js":    "javascript",
		".ts":    "typescript",
		".jsx":   "javascript",
		".tsx":   "typescript",
		".java":  "java",
		".cpp":   "cpp",
		".c":     "c",
		".h":     "c",
		".hpp":   "cpp",
		".rs":    "rust",
		".rb":    "ruby",
		".php":   "php",
		".swift": "swift",
		".kt":    "kotlin",
		".scala": "scala",
		".sh":    "shell",
		".bash":  "shell",
		".md":    "markdown",
		".yml":   "yaml",
		".yaml":  "yaml",
		".json":  "json",
		".xml":   "xml",
		".html":  "html",
		".css":   "css",
		".sql":   "sql",
	}

	if lang, ok := langMap[ext]; ok {
		return lang
	}
	return "plaintext"
}
