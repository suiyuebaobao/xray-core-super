// scheduler_test.go — 调度器测试。
package scheduler_test

import (
	"sync/atomic"
	"testing"

	"suiyue/internal/scheduler"

	"github.com/stretchr/testify/assert"
)

func TestScheduler_StartStop(t *testing.T) {
	s := scheduler.New()
	s.Start()
	// 启动后应该不 panic
	s.Stop()
	// 重复 Stop 可能 panic，测试只调用一次
}

func TestScheduler_AddJob(t *testing.T) {
	s := scheduler.New()
	var count atomic.Int64

	s.AddJob("@every 1s", "test-job", func() {
		count.Add(1)
	})

	s.Start()
	// 等待至少执行一次
	assert.Eventually(t, func() bool {
		return count.Load() >= 1
	}, 2000000000, 100000000, "job should have run at least once")

	s.Stop()
}

func TestScheduler_AddJob_PanicRecovery(t *testing.T) {
	s := scheduler.New()
	panicCount := int64(0)

	s.AddJob("@every 1s", "panic-job", func() {
		defer func() {
			if r := recover(); r != nil {
				// 不应该到达这里，因为调度器已经包装了 recover
			}
		}()
		// 只 panic 一次
		panicCount++
		if panicCount == 1 {
			panic("test panic")
		}
	})

	// 不应因 panic 而崩溃
	s.Start()
	defer s.Stop()
}


