// Package requestcapture records opt-in diagnostic traffic without changing it.
package requestcapture

import (
	"context"
	"errors"
	"time"
)

const (
	MaxTasks           = 16
	MaxSessions        = 256
	BufferLimit  int64 = 64 << 20
	RecordLimit  int64 = 128 << 20
	ChunkSize          = 32 << 10
	PreviewLimit       = 256 << 10
)

var (
	ErrDisabled   = errors.New("request capture is disabled")
	ErrNotFound   = errors.New("capture not found on this instance")
	ErrFinalizing = errors.New("capture task must be stopped and finalized before exporting")
	ErrCapacity   = errors.New("request capture capacity reached")
)

type Config struct {
	Enabled       bool
	QuotaMiB      int64
	RetentionDays int
}

func (c Config) Validate() error {
	if c.QuotaMiB < 1 || c.QuotaMiB > (1<<63-1)/(1<<20) {
		return errors.New("capture quota must be a positive integer MiB within int64 range")
	}
	if c.RetentionDays < 1 || c.RetentionDays > 30 {
		return errors.New("capture retention must be between 1 and 30 days")
	}
	return nil
}

type Task struct {
	ID         string `json:"id"`
	InstanceID string `json:"instance_id"`
	TargetType string `json:"target_type"`
	TargetID   int64  `json:"target_id"`
	TargetName string `json:"target_name"`
	SaveMedia  bool   `json:"save_media"`
	// Raw 原文模式：不脱敏、不过滤报文，保留完整请求/响应头与 URL，成功请求
	// 同样入库。仅用于管理员主动排障，数据按原样落盘。
	Raw       bool       `json:"raw,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Status    string     `json:"status"`
	Reason    string     `json:"reason,omitempty"`
	Requests  int64      `json:"requests"`
	Partial   int64      `json:"partial"`
	Skipped   int64      `json:"skipped"`
	Bytes     int64      `json:"bytes"`
}

type CreateTask struct {
	TargetType      string `json:"target_type"`
	TargetID        int64  `json:"target_id"`
	DurationMinutes int    `json:"duration_minutes"`
	SaveMedia       bool   `json:"save_media"`
	Raw             bool   `json:"raw"`
}

type Meta struct {
	RequestID       string `json:"request_id"`
	ClientRequestID string `json:"client_request_id,omitempty"`
	UserID          int64  `json:"user_id"`
	UserEmail       string `json:"user_email,omitempty"`
	APIKeyID        int64  `json:"api_key_id,omitempty"`
	APIKeyName      string `json:"api_key_name,omitempty"`
	GroupID         int64  `json:"group_id"`
	RoutedGroupID   int64  `json:"routed_group_id,omitempty"`
	Method          string `json:"method"`
	Path            string `json:"path"`
	RawPath         string `json:"raw_path,omitempty"`
	Model           string `json:"model,omitempty"`
	Protocol        string `json:"protocol"`
}

type Attempt struct {
	Number            int       `json:"number"`
	AccountID         int64     `json:"account_id"`
	AccountName       string    `json:"account_name,omitempty"`
	StartedAt         time.Time `json:"started_at"`
	Status            int       `json:"status,omitempty"`
	UpstreamRequestID string    `json:"upstream_request_id,omitempty"`
	Error             string    `json:"error,omitempty"`
	ErrorDetail       string    `json:"error_detail,omitempty"`
	ErrorStage        string    `json:"error_stage,omitempty"`
	ReadError         string    `json:"read_error,omitempty"`
	ReadErrorDetail   string    `json:"read_error_detail,omitempty"`
	ResponseTerminal  string    `json:"response_terminal,omitempty"`
	LocalClose        bool      `json:"local_close,omitempty"`
}

type Part struct {
	URL         string            `json:"url,omitempty"`
	RawURL      string            `json:"raw_url,omitempty"`
	Name        string            `json:"name"`
	Stage       string            `json:"stage"`
	Attempt     int               `json:"attempt"`
	Turn        int               `json:"turn"`
	ContentType string            `json:"content_type"`
	Headers     map[string]string `json:"headers,omitempty"`
	// RawHeaders 仅原文模式任务保留：完整、未截断、未脱敏的全部头。
	RawHeaders map[string][]string `json:"raw_headers,omitempty"`
	Bytes      int64               `json:"bytes"`
	Omitted    string              `json:"omitted,omitempty"`
}

type Record struct {
	ID         string `json:"id"`
	TaskID     string `json:"task_id"`
	InstanceID string `json:"instance_id"`
	Turn       int    `json:"turn,omitempty"`
	Meta
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Status     int        `json:"status"`
	IsError    bool       `json:"is_error"`
	Partial    bool       `json:"partial"`
	Reason     string     `json:"reason,omitempty"`
	Bytes      int64      `json:"bytes"`
	Attempts   []Attempt  `json:"attempts"`
	Parts      []Part     `json:"parts"`
	Raw        bool       `json:"raw,omitempty"`
	AccountIDs []int64    `json:"account_ids,omitempty"`
	// AccountNames 由管理接口读取时按 AccountIDs 填充，不入库。
	AccountNames  map[int64]string    `json:"account_names,omitempty"`
	ClientHeaders map[string][]string `json:"client_headers,omitempty"`
	Handshakes    []Handshake         `json:"handshakes,omitempty"`
	ClientOutcome string              `json:"client_outcome,omitempty"`
	ErrorCode     string              `json:"error_code,omitempty"`
	Usage         map[string]int64    `json:"usage,omitempty"`
}

// Handshake 记录一次上游 WebSocket 握手（仅原文模式）：WS 帧本身没有头，
// 实际发往上游的鉴权与指纹头都在握手请求里。
type Handshake struct {
	AccountID       int64               `json:"account_id"`
	AccountName     string              `json:"account_name,omitempty"`
	At              time.Time           `json:"at"`
	URL             string              `json:"url"`
	RequestHeaders  map[string][]string `json:"request_headers,omitempty"`
	Status          int                 `json:"status,omitempty"`
	ResponseHeaders map[string][]string `json:"response_headers,omitempty"`
	ResponseBody    string              `json:"response_body,omitempty"`
	Error           string              `json:"error,omitempty"`
}

// Store accepts metadata only. Payload bytes must never enter this interface.
type Store interface {
	SaveTask(context.Context, *Task) error
	Task(context.Context, string, string) (*Task, error)
	Tasks(context.Context, string, int, int) ([]Task, error)
	SaveRecord(context.Context, *Record) error
	Records(context.Context, string, string, bool, int, int) ([]Record, error)
	Record(context.Context, string, string) (*Record, error)
	DeleteRecord(context.Context, string, string) error
	DeleteTask(context.Context, string, string) error
}
