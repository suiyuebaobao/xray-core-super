// Package scheduler 提供定时任务调度能力。
//
// 使用 robfig/cron/v3 管理后台定时任务，例如：
// - 订阅过期扫描
// - 订单过期扫描（v1 保留骨架）
// - 支付对账扫描（v1 保留骨架）
//
// 注意：v1 不引入 Redis，所有任务状态基于 MySQL 管理。
package scheduler

import (
	"log"
	"sync"

	"github.com/robfig/cron/v3"
)

// Scheduler 封装 cron 调度器。
type Scheduler struct {
	cron *cron.Cron
	wg   sync.WaitGroup
}

// New 创建调度器。
func New() *Scheduler {
	return &Scheduler{
		cron: cron.New(),
	}
}

// Start 启动调度器。
func (s *Scheduler) Start() {
	s.cron.Start()
	log.Println("[scheduler] scheduler started")
}

// Stop 停止调度器并等待所有任务完成。
func (s *Scheduler) Stop() {
	log.Println("[scheduler] stopping scheduler")
	s.cron.Stop()
	s.wg.Wait()
	log.Println("[scheduler] scheduler stopped")
}

// AddJob 添加一个定时任务。
//
// spec 为 cron 表达式，例如 "@every 1m" 或 "0 */6 * * *"。
func (s *Scheduler) AddJob(spec string, name string, fn func()) {
	_, err := s.cron.AddFunc(spec, func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[scheduler] job %s panicked: %v", name, r)
			}
		}()
		fn()
	})
	if err != nil {
		log.Printf("[scheduler] failed to add job %s: %v", name, err)
		return
	}
	log.Printf("[scheduler] job added: %s (%s)", name, spec)
}
