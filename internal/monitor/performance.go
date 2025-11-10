package monitor

import (
	"fmt"
	"sync/atomic"
	"time"
)

var (
	totalMessages      uint64
	lastSecondMessages uint64
	maxPerSecond       uint64
	started            int32
)

// Called from mqtt or anywhere that sends messages
func IncMessageCounter() {
	atomic.AddUint64(&totalMessages, 1)
	atomic.AddUint64(&lastSecondMessages, 1)
}

func StartPerformanceMonitor() {
	if !atomic.CompareAndSwapInt32(&started, 0, 1) {
		return
	}

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		start := time.Now()
		defer ticker.Stop()

		for range ticker.C {
			elapsed := time.Since(start).Seconds()
			total := atomic.LoadUint64(&totalMessages)
			curr := atomic.SwapUint64(&lastSecondMessages, 0)

			if curr > atomic.LoadUint64(&maxPerSecond) {
				atomic.StoreUint64(&maxPerSecond, curr)
			}

			fmt.Printf("[PERF] Total: %d msgs | Avg: %.2f msg/s | This sec: %d msg/s | Max/sec: %d\n",
				total,
				float64(total)/elapsed,
				curr,
				atomic.LoadUint64(&maxPerSecond),
			)
		}
	}()
}
