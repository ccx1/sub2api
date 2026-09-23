package service

import (
	"context"
	"sync"
	"time"
)

// 同一账号串行执行；手动任务插到尚未开始的自动任务之前。
type codexTicketProbeTask struct {
	ctx     context.Context
	account *Account
	model   string
	manual  bool
	done    chan struct{}
}

type codexTicketAccountQueue struct {
	service *OpenAIGatewayService

	mu      sync.Mutex
	tasks   []*codexTicketProbeTask
	running bool
}

func (s *OpenAIGatewayService) enqueueCodexTicketProbe(ctx context.Context, account *Account, model string, manual bool) {
	tasks := s.submitCodexTicketProbes(ctx, account, []string{model}, manual)
	if len(tasks) == 0 {
		return
	}
	select {
	case <-tasks[0].done:
	case <-tasks[0].ctx.Done():
		if !s.codexTicketAccountQueue(account.ID).cancelPending(tasks[0]) {
			<-tasks[0].done
		}
	}
}

func (q *codexTicketAccountQueue) cancelPending(task *codexTicketProbeTask) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, pending := range q.tasks {
		if pending != task {
			continue
		}
		q.tasks = append(q.tasks[:i], q.tasks[i+1:]...)
		if task.manual {
			q.service.openaiCodexTicketManualPending.Add(-1)
		}
		close(task.done)
		return true
	}
	return false
}

func (s *OpenAIGatewayService) submitCodexTicketProbes(ctx context.Context, account *Account, models []string, manual bool) []*codexTicketProbeTask {
	if s == nil || account == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	queue := s.codexTicketAccountQueue(account.ID)
	tasks := make([]*codexTicketProbeTask, 0, len(models))
	for _, model := range models {
		tasks = append(tasks, &codexTicketProbeTask{ctx: ctx, account: cloneOpenAICodexTicketAccount(account), model: model, manual: manual, done: make(chan struct{})})
	}
	queue.mu.Lock()
	if manual {
		index := len(queue.tasks)
		for i, queued := range queue.tasks {
			if !queued.manual {
				index = i
				break
			}
		}
		queue.tasks = append(queue.tasks, tasks...)
		copy(queue.tasks[index+len(tasks):], queue.tasks[index:len(queue.tasks)-len(tasks)])
		copy(queue.tasks[index:], tasks)
		s.openaiCodexTicketAdmissionMu.Lock()
		s.openaiCodexTicketManualPending.Add(int32(len(tasks)))
		s.openaiCodexTicketAdmissionMu.Unlock()
	} else {
		queue.tasks = append(queue.tasks, tasks...)
	}
	start := !queue.running
	queue.running = true
	queue.mu.Unlock()
	if start {
		go queue.run()
	}
	return tasks
}

func (s *OpenAIGatewayService) codexTicketAccountQueue(accountID int64) *codexTicketAccountQueue {
	if existing, ok := s.openaiCodexTicketQueues.Load(accountID); ok {
		return existing.(*codexTicketAccountQueue)
	}
	queue := &codexTicketAccountQueue{service: s}
	actual, _ := s.openaiCodexTicketQueues.LoadOrStore(accountID, queue)
	return actual.(*codexTicketAccountQueue)
}

func (q *codexTicketAccountQueue) next() *codexTicketProbeTask {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.tasks) == 0 {
		q.running = false
		return nil
	}
	task := q.tasks[0]
	q.tasks[0] = nil
	q.tasks = q.tasks[1:]
	return task
}

func (q *codexTicketAccountQueue) hasManualPending() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, task := range q.tasks {
		if task.manual {
			return true
		}
	}
	return false
}

func (q *codexTicketAccountQueue) yieldToManual(task *codexTicketProbeTask) {
	q.mu.Lock()
	index := 0
	for index < len(q.tasks) && q.tasks[index].manual {
		index++
	}
	q.tasks = append(q.tasks, nil)
	copy(q.tasks[index+1:], q.tasks[index:])
	q.tasks[index] = task
	q.mu.Unlock()
}

func (q *codexTicketAccountQueue) run() {
	for {
		task := q.next()
		if task == nil {
			return
		}
		if q.runTask(task) {
			if task.manual {
				q.service.openaiCodexTicketManualPending.Add(-1)
			}
			close(task.done)
		}
	}
}

func (q *codexTicketAccountQueue) runTask(task *codexTicketProbeTask) bool {
	for task.ctx.Err() == nil {
		// 等待期间到来的手动任务仍优先；已发出的请求完成后再让出账号。
		if !task.manual && q.hasManualPending() {
			q.yieldToManual(task)
			return false
		}
		acquired, manualWaiting := q.service.acquireCodexTicketProbeWorker(task)
		if acquired {
			defer q.service.openaiCodexTicketActive.Add(-1)
			q.service.harvestVerifiedOpenAICodexTicket(task.ctx, task.account, task.model)
			return true
		}
		if !task.manual && !manualWaiting {
			return true
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-task.ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return true
		case <-timer.C:
		}
	}
	return true
}

func (s *OpenAIGatewayService) acquireCodexTicketProbeWorker(task *codexTicketProbeTask) (bool, bool) {
	// 准入与手动登记共用锁，消除检查优先级后自动任务抢先占用 worker 的窗口。
	s.openaiCodexTicketAdmissionMu.Lock()
	defer s.openaiCodexTicketAdmissionMu.Unlock()
	if !task.manual && s.openaiCodexTicketManualPending.Load() > 0 {
		return false, true
	}
	return s.acquireCodexTicketWorker(task.ctx), false
}
