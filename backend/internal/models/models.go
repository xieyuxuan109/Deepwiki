// Package models 定义了 DeepWiki 系统的核心数据模型和结构体，
// 包括摄取任务状态和问答引用来源。
package models

import "time"

// IngestTask 表示一个仓库摄取任务的状态信息。
type IngestTask struct {
	ID             string    `json:"id"`              // 任务唯一 ID
	RepoURL        string    `json:"repo_url"`        // 目标仓库地址
	Status         string    `json:"status"`          // 当前状态（见下方常量）
	Progress       float64   `json:"progress"`        // 进度百分比 (0.0 ~ 1.0)
	Message        string    `json:"message"`         // 状态描述信息
	TotalFiles     int       `json:"total_files"`     // 总文件数
	ProcessedFiles int       `json:"processed_files"` // 已处理文件数
	Error          string    `json:"error,omitempty"` // 错误信息（如果有）
	CreatedAt      time.Time `json:"created_at"`      // 创建时间
	UpdatedAt      time.Time `json:"updated_at"`      // 最后更新时间
}

// 任务状态常量。
const (
	TaskStatusPending   = "pending"   // 等待执行
	TaskStatusRunning   = "running"   // 执行中
	TaskStatusCompleted = "completed" // 已完成
	TaskStatusFailed    = "failed"    // 失败
)

// SourceRef 表示回答中引用的一段代码来源。
type SourceRef struct {
	FilePath  string  `json:"file_path"`  // 文件路径
	StartLine int     `json:"start_line"` // 起始行号
	EndLine   int     `json:"end_line"`   // 结束行号
	Language  string  `json:"language"`   // 编程语言
	Score     float64 `json:"score"`      // 相似度得分
}
