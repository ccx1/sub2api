package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	modelAvailabilityRecheckInterval      = time.Minute
	modelAvailabilityRecheckLeaderLockKey = "jobs:model-availability-recheck"
	modelAvailabilityRecheckLeaderLockTTL = 15 * time.Minute
	modelAvailabilityRecheckScanLimit     = 200
	// 单轮最多真实探测的 (账号, 模型) 数，避免大量模型同时失效时一轮扫描过长；
	// 剩余条目下一分钟继续处理。
	modelAvailabilityRecheckMaxProbes    = 10
	modelAvailabilityRecheckProbeTimeout = 60 * time.Second
	// 复检无法得出"模型确实不可用"的结论（网络错误、429、映射为空无法删除等）时，
	// 继续暂停该模型并延后下一次复检。
	modelAvailabilityRecheckRetryCooldown = 30 * time.Minute
)

var modelAvailabilityStatusCodePattern = regexp.MustCompile(`(?:returned|HTTP) (\d{3})`)

type modelAvailabilityTester interface {
	RunTestBackground(ctx context.Context, accountID int64, modelID string) (*ScheduledTestResult, error)
}

// ModelAvailabilityRecheckService 复检因单模型 503 被暂停调度的 (API Key 账号, 模型)：
// 冷却到期后用该模型做一次真实测试，成功则恢复调度，仍失败则从账号的
// credentials.model_mapping 中删除该模型，避免请求持续打到不可用的模型上。
type ModelAvailabilityRecheckService struct {
	accountRepo  AccountRepository
	availability ModelAvailabilityRepository
	tester       modelAvailabilityTester
	lockCache    LeaderLockCache
	db           *sql.DB
	now          func() time.Time

	ctx    context.Context
	cancel context.CancelFunc
	owner  string
	start  sync.Once
	stop   sync.Once
	wg     sync.WaitGroup
}

func NewModelAvailabilityRecheckService(
	accountRepo AccountRepository,
	availability ModelAvailabilityRepository,
	tester modelAvailabilityTester,
	lockCache LeaderLockCache,
	db *sql.DB,
) *ModelAvailabilityRecheckService {
	ctx, cancel := context.WithCancel(context.Background())
	return &ModelAvailabilityRecheckService{
		accountRepo:  accountRepo,
		availability: availability,
		tester:       tester,
		lockCache:    lockCache,
		db:           db,
		now:          time.Now,
		ctx:          ctx,
		cancel:       cancel,
		owner:        uuid.NewString(),
	}
}

func (s *ModelAvailabilityRecheckService) Start() {
	if s == nil || s.accountRepo == nil || s.availability == nil || s.tester == nil {
		return
	}
	s.start.Do(func() {
		s.wg.Add(1)
		go s.run()
	})
}

func (s *ModelAvailabilityRecheckService) Stop() {
	if s == nil {
		return
	}
	s.stop.Do(func() {
		s.cancel()
		s.wg.Wait()
	})
}

func (s *ModelAvailabilityRecheckService) run() {
	defer s.wg.Done()
	ticker := time.NewTicker(modelAvailabilityRecheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.scan(s.ctx)
		}
	}
}

func (s *ModelAvailabilityRecheckService) scan(ctx context.Context) {
	release, ok := tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, modelAvailabilityRecheckLeaderLockKey, s.owner, modelAvailabilityRecheckLeaderLockTTL)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}
	s.runOnce(ctx)
}

// runOnce 处理一轮到期条目，返回本轮实际探测的次数。
func (s *ModelAvailabilityRecheckService) runOnce(ctx context.Context) int {
	ids, err := s.availability.ListModelRateLimitedAccountIDsByReason(ctx, upstreamModelUnavailableReason, modelAvailabilityRecheckScanLimit)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Warn("model_availability_recheck_list_failed", "error", err)
		}
		return 0
	}
	probes := 0
	for _, id := range ids {
		if ctx.Err() != nil || probes >= modelAvailabilityRecheckMaxProbes {
			break
		}
		probes += s.recheckAccount(ctx, id, modelAvailabilityRecheckMaxProbes-probes)
	}
	return probes
}

func (s *ModelAvailabilityRecheckService) recheckAccount(ctx context.Context, accountID int64, budget int) int {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return 0
	}
	// 列表查询与本次读取之间账号可能已被暂停/停用：不对暂停账号发起真实探测。
	if account.Type != AccountTypeAPIKey || !account.IsActive() || !account.Schedulable {
		return 0
	}
	probes := 0
	for _, scope := range dueModelAvailabilityScopes(account, s.now()) {
		if ctx.Err() != nil || probes >= budget {
			break
		}
		probes++
		s.recheckModel(ctx, account, scope)
	}
	return probes
}

func (s *ModelAvailabilityRecheckService) recheckModel(ctx context.Context, account *Account, scope string) {
	mappingKeys := modelAvailabilityMappingKeys(account, scope)
	testModel := scope
	if len(mappingKeys) > 0 && !slices.Contains(mappingKeys, scope) {
		// scope 是映射后的上游模型名；测试会再做一次映射，因此用映射键发起测试。
		testModel = mappingKeys[0]
	}

	probeCtx, cancel := context.WithTimeout(ctx, modelAvailabilityRecheckProbeTimeout)
	result, err := s.tester.RunTestBackground(probeCtx, account.ID, testModel)
	cancel()
	if ctx.Err() != nil {
		return
	}
	if err == nil && result != nil && result.Status == "success" {
		if _, clearErr := s.availability.ClearModelRateLimitIfReason(ctx, account.ID, scope, upstreamModelUnavailableReason); clearErr != nil {
			slog.Warn("model_availability_recheck_clear_failed", "account_id", account.ID, "model", scope, "error", clearErr)
			return
		}
		slog.Info("model_availability_recheck_recovered", "account_id", account.ID, "model", scope)
		return
	}

	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	} else if result != nil {
		errMsg = result.ErrorMessage
	}
	statusCode := modelAvailabilityProbeStatusCode(errMsg)
	if !isModelAvailabilityRemovalStatus(statusCode) || isModelAvailabilityCapacityShed(errMsg) {
		// 网络错误、429、鉴权失败等不能证明是该模型本身不可用，不删模型，延后再测。
		s.extendCooldown(ctx, account, scope, "probe_inconclusive", statusCode, errMsg)
		return
	}
	if len(mappingKeys) == 0 {
		// 未配置 model_mapping（允许所有模型）或只经通配符命中：无法精确删除，保持暂停。
		s.extendCooldown(ctx, account, scope, "mapping_not_removable", statusCode, errMsg)
		return
	}
	removed, removeErr := s.availability.RemoveModelMappingKeys(ctx, account.ID, mappingKeys)
	if removeErr != nil {
		slog.Warn("model_availability_recheck_remove_failed", "account_id", account.ID, "model", scope, "error", removeErr)
		s.extendCooldown(ctx, account, scope, "remove_failed", statusCode, errMsg)
		return
	}
	if !removed {
		// 删除后映射会变空（语义变为允许所有模型）或映射已被并发修改：保持暂停。
		s.extendCooldown(ctx, account, scope, "mapping_not_removable", statusCode, errMsg)
		return
	}
	if _, clearErr := s.availability.ClearModelRateLimitIfReason(ctx, account.ID, scope, upstreamModelUnavailableReason); clearErr != nil {
		slog.Warn("model_availability_recheck_clear_after_remove_failed", "account_id", account.ID, "model", scope, "error", clearErr)
	}
	slog.Warn("model_availability_recheck_model_removed",
		"account_id", account.ID,
		"account_name", account.Name,
		"model", scope,
		"removed_mapping_keys", mappingKeys,
		"probe_status", statusCode,
		"probe_error", truncateForLog([]byte(errMsg), 256),
	)
}

func (s *ModelAvailabilityRecheckService) extendCooldown(ctx context.Context, account *Account, scope, cause string, statusCode int, errMsg string) {
	resetAt := s.now().Add(modelAvailabilityRecheckRetryCooldown)
	if err := s.accountRepo.SetModelRateLimit(ctx, account.ID, scope, resetAt, upstreamModelUnavailableReason); err != nil {
		slog.Warn("model_availability_recheck_extend_failed", "account_id", account.ID, "model", scope, "error", err)
		return
	}
	slog.Warn("model_availability_recheck_still_unavailable",
		"account_id", account.ID,
		"model", scope,
		"cause", cause,
		"probe_status", statusCode,
		"probe_error", truncateForLog([]byte(errMsg), 256),
		"next_check_at", resetAt,
	)
}

// dueModelAvailabilityScopes 返回 reason 为单模型 503 且冷却已到期的模型 scope。
func dueModelAvailabilityScopes(account *Account, now time.Time) []string {
	if account == nil || account.Extra == nil {
		return nil
	}
	limits, ok := account.Extra[modelRateLimitsKey].(map[string]any)
	if !ok {
		return nil
	}
	scopes := make([]string, 0, len(limits))
	for scope, raw := range limits {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if reason, _ := entry["reason"].(string); strings.TrimSpace(reason) != upstreamModelUnavailableReason {
			continue
		}
		resetAt := account.modelRateLimitResetAt(scope)
		if resetAt != nil && now.Before(*resetAt) {
			continue
		}
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	return scopes
}

// modelAvailabilityMappingKeys 返回 credentials.model_mapping 中指向该模型的精确键
// （键本身等于该模型，或映射目标等于该模型）。通配符键会同时覆盖其它模型，不在删除范围内。
func modelAvailabilityMappingKeys(account *Account, scope string) []string {
	if account == nil || account.Credentials == nil {
		return nil
	}
	raw, ok := account.Credentials["model_mapping"].(map[string]any)
	if !ok {
		return nil
	}
	var keys []string
	for key, value := range raw {
		if strings.Contains(key, "*") {
			continue
		}
		target, _ := value.(string)
		if key == scope || strings.TrimSpace(target) == scope {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func modelAvailabilityProbeStatusCode(errMsg string) int {
	match := modelAvailabilityStatusCodePattern.FindStringSubmatch(errMsg)
	if len(match) < 2 {
		return 0
	}
	code, err := strconv.Atoi(match[1])
	if err != nil {
		return 0
	}
	return code
}

// isModelAvailabilityRemovalStatus 判断复检失败的状态码是否足以认定该模型在此账号上不可用。
func isModelAvailabilityRemovalStatus(statusCode int) bool {
	return statusCode == http.StatusNotFound || statusCode >= http.StatusInternalServerError
}

// isModelAvailabilityCapacityShed 排除上游请求级容量降载（server is overloaded / slow_down），
// 这类 503 代表瞬时拥塞而不是模型失效。
func isModelAvailabilityCapacityShed(errMsg string) bool {
	body := errMsg
	if idx := strings.Index(errMsg, ": "); idx >= 0 {
		body = errMsg[idx+2:]
	}
	return isOpenAIRequestScopedCapacityShed("", []byte(body))
}
