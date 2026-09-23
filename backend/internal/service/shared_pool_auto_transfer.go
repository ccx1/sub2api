package service

import (
	"context"
	"database/sql"
	"math"
	"sync"
	"sync/atomic"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var ErrSharedPoolAutoTransferInvalid = infraerrors.BadRequest("SHARED_POOL_AUTO_TRANSFER_INVALID", "请输入有效的收益门槛（0.00000001 至 1000000000，最多八位小数）和每日时间（HH:mm）")

type SharedPoolAutoTransferSettings struct {
	Enabled     bool    `json:"enabled"`
	Threshold   float64 `json:"threshold"`
	DailyTime   string  `json:"daily_time"`
	Timezone    string  `json:"timezone"`
	LastRunDate string  `json:"last_run_date,omitempty"`
}

type SharedPoolAutoTransferUpdate struct {
	Enabled   bool    `json:"enabled"`
	Threshold float64 `json:"threshold"`
	DailyTime string  `json:"daily_time"`
}

type SharedPoolAutoTransferRepository interface {
	Get(context.Context, int64) (*SharedPoolAutoTransferSettings, error)
	Save(context.Context, int64, SharedPoolAutoTransferUpdate) (*SharedPoolAutoTransferSettings, error)
	DueUserIDs(context.Context, time.Time, int64, int) ([]int64, error)
	// RunDue 在同一事务内重检设置、门槛、转账并记录当天执行，失败时全部回滚。
	RunDue(context.Context, int64, time.Time) (*SharedPoolEarningsTransfer, error)
}

type sharedPoolBalanceInvalidator interface {
	InvalidateUserBalance(context.Context, int64) error
}

type SharedPoolAutoTransferService struct {
	settings   SharedPoolAutoTransferRepository
	authCache  APIKeyAuthCacheInvalidator
	billing    sharedPoolBalanceInvalidator
	leader     LeaderLockCache
	db         *sql.DB
	instanceID string
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	startOnce  sync.Once
	started    atomic.Bool
}

func NewSharedPoolAutoTransferService(repo SharedPoolAutoTransferRepository, auth APIKeyAuthCacheInvalidator,
	billing sharedPoolBalanceInvalidator, leader LeaderLockCache, db *sql.DB) *SharedPoolAutoTransferService {
	ctx, cancel := context.WithCancel(context.Background())
	return &SharedPoolAutoTransferService{
		settings: repo, authCache: auth, billing: billing, leader: leader, db: db,
		instanceID: uuid.NewString(), ctx: ctx, cancel: cancel, done: make(chan struct{}),
	}
}

func (s *SharedPoolAutoTransferService) Get(ctx context.Context, userID int64) (*SharedPoolAutoTransferSettings, error) {
	if userID <= 0 {
		return nil, ErrSharedPoolAutoTransferInvalid
	}
	return s.settings.Get(ctx, userID)
}

func (s *SharedPoolAutoTransferService) Save(ctx context.Context, userID int64, input SharedPoolAutoTransferUpdate) (*SharedPoolAutoTransferSettings, error) {
	if userID <= 0 || ValidateSharedPoolAutoTransfer(input) != nil {
		return nil, ErrSharedPoolAutoTransferInvalid
	}
	return s.settings.Save(ctx, userID, input)
}

func ValidateSharedPoolAutoTransfer(input SharedPoolAutoTransferUpdate) error {
	if math.IsNaN(input.Threshold) || math.IsInf(input.Threshold, 0) || input.Threshold < 0.00000001 || input.Threshold > 1000000000 {
		return ErrSharedPoolAutoTransferInvalid
	}
	amount := decimal.NewFromFloat(input.Threshold)
	if !amount.Equal(amount.Round(8)) {
		return ErrSharedPoolAutoTransferInvalid
	}
	clock, err := time.Parse("15:04", input.DailyTime)
	if err != nil || clock.Format("15:04") != input.DailyTime {
		return ErrSharedPoolAutoTransferInvalid
	}
	return nil
}
