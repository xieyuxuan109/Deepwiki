package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"deepwiki/internal/ingest"
	"deepwiki/internal/models"
	"deepwiki/internal/rag"
)

type Handler struct {
	ingestSvc *ingest.Service
	ragSvc    *rag.Service
}

func NewHandler(ingestSvc *ingest.Service, ragSvc *rag.Service) *Handler {
	return &Handler{
		ingestSvc: ingestSvc,
		ragSvc:    ragSvc,
	}
}

func (h *Handler) SetupRoutes(r *gin.Engine) {
	api := r.Group("/api")
	{
		api.POST("/ingest", h.Ingest)
		api.GET("/ingest/:id/status", h.IngestStatus)
		api.POST("/ask", h.Ask)
		api.POST("/ask/stream", h.AskStream)
	}
}

type ingestReq struct {
	RepoURL string   `json:"repo_url" binding:"required"`
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

type ingestResp struct {
	TaskID string `json:"task_id"`
}

func (h *Handler) Ingest(c *gin.Context) {
	var req ingestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	taskID, err := h.ingestSvc.StartIngest(req.RepoURL, req.Include, req.Exclude)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, ingestResp{TaskID: taskID})
}

func (h *Handler) IngestStatus(c *gin.Context) {
	taskID := c.Param("id")

	task, ok := h.ingestSvc.GetTaskStatus(taskID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}

	c.JSON(http.StatusOK, task)
}

type askReq struct {
	RepoURL  string `json:"repo_url" binding:"required"`
	Question string `json:"question" binding:"required"`
	TopK     int    `json:"top_k,omitempty"`
}

type askResp struct {
	Answer  string             `json:"answer"`
	Sources []models.SourceRef `json:"sources"`
}

func (h *Handler) Ask(c *gin.Context) {
	var req askReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	input := &rag.AskInput{
		RepoURL:  req.RepoURL,
		Question: req.Question,
		TopK:     req.TopK,
	}

	result, err := h.ragSvc.Ask(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, askResp{
		Answer:  result.Answer,
		Sources: result.Sources,
	})
}

// AskStream 处理流式问答请求，使用 Server-Sent Events (SSE) 逐字返回回答。
func (h *Handler) AskStream(c *gin.Context) {
	var req askReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 设置 SSE 响应头
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")

	ctx := c.Request.Context()

	// 检查是否支持流式刷新
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	input := &rag.AskInput{
		RepoURL:  req.RepoURL,
		Question: req.Question,
		TopK:     req.TopK,
	}

	// 调用流式问答服务，逐 token 回调写入 SSE
	sources, err := h.ragSvc.AskStream(ctx, input, func(token string) {
		c.SSEvent("message", gin.H{"type": "token", "content": token})
		flusher.Flush()
	})

	if err != nil {
		c.SSEvent("error", gin.H{"message": err.Error()})
		flusher.Flush()
		return
	}

	c.SSEvent("sources", gin.H{"sources": sources})
	c.SSEvent("done", gin.H{"message": "completed"})
	flusher.Flush()
}
