package service

import "sync"

var openAICodexSnapshotWriteLocks [256]sync.Mutex

// 在启动后台写入前取得锁，避免已排队的旧后台快照晚于后续同步快照落库。
// 锁覆盖 UpdateExtra 内的同步调度缓存刷新；固定分片避免账号锁表无限增长。
func lockOpenAICodexSnapshotWrite(accountID int64) func() {
	mu := &openAICodexSnapshotWriteLocks[uint64(accountID)%uint64(len(openAICodexSnapshotWriteLocks))]
	mu.Lock()
	return mu.Unlock
}
