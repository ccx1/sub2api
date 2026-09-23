package service

import (
	"context"
	"log"
	"time"
)

const sharedPoolAutoTransferLockKey = "shared-pool:auto-transfer"

func (s *SharedPoolAutoTransferService) Start() {
	s.startOnce.Do(func() {
		s.started.Store(true)
		go s.loop()
	})
}

func (s *SharedPoolAutoTransferService) Stop() {
	s.cancel()
	if !s.started.Load() {
		return
	}
	<-s.done
}

func (s *SharedPoolAutoTransferService) loop() {
	defer close(s.done)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for s.ctx.Err() == nil {
		if err := s.runOnce(s.ctx, time.Now()); err != nil && s.ctx.Err() == nil {
			log.Printf("[shared-pool-auto-transfer] scan: %v", err)
		}
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *SharedPoolAutoTransferService) runOnce(parent context.Context, now time.Time) error {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	release, acquired := tryAcquireSingletonLeaderLock(ctx, s.leader, s.db, sharedPoolAutoTransferLockKey, s.instanceID, time.Minute)
	if !acquired {
		return nil
	}
	defer release()
	var afterID int64
	for ctx.Err() == nil {
		ids, err := s.settings.DueUserIDs(ctx, now, afterID, 100)
		if err != nil {
			return err
		}
		for _, userID := range ids {
			s.runUser(ctx, userID, now)
			afterID = userID
		}
		if len(ids) < 100 {
			return nil
		}
	}
	return ctx.Err()
}

func (s *SharedPoolAutoTransferService) runUser(ctx context.Context, userID int64, now time.Time) {
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	result, err := s.settings.RunDue(runCtx, userID, now)
	cancel()
	if err != nil {
		log.Printf("[shared-pool-auto-transfer] user %d: %v", userID, err)
		return
	}
	if result == nil {
		return
	}
	// 提交成功后即便 worker 正在停止，也需要完成余额和认证缓存失效。
	cacheCtx, cacheCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cacheCancel()
	if s.authCache != nil {
		s.authCache.InvalidateAuthCacheByUserID(cacheCtx, userID)
	}
	if s.billing != nil {
		if err := s.billing.InvalidateUserBalance(cacheCtx, userID); err != nil {
			log.Printf("[shared-pool-auto-transfer] invalidate balance user %d: %v", userID, err)
		}
	}
}
