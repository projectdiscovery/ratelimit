package ratelimit

import (
	"context"
	"fmt"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// Run separately from other benchmarks: CPU usage includes the entire process.
func BenchmarkUnlimitedIdle(b *testing.B) {
	for _, n := range []int{0, 30, 1000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			before := runtime.NumGoroutine()
			limiters := make([]*Limiter, n)
			for i := range limiters {
				limiters[i] = NewUnlimited(context.Background())
			}
			defer func() {
				for _, limiter := range limiters {
					limiter.Stop()
				}
			}()
			workers := runtime.NumGoroutine() - before
			cpuTime := func() time.Duration {
				var usage syscall.Rusage
				if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
					b.Fatal(err)
				}
				return time.Duration(usage.Utime.Nano() + usage.Stime.Nano())
			}
			b.ResetTimer()
			startCPU, start := cpuTime(), time.Now()
			for i := 0; i < b.N; i++ {
				time.Sleep(100 * time.Millisecond)
			}
			elapsed, cpu := time.Since(start), cpuTime()-startCPU
			b.StopTimer()
			b.ReportMetric(float64(cpu.Nanoseconds())/elapsed.Seconds(), "cpu-ns/s")
			b.ReportMetric(float64(workers), "goroutines")
		})
	}
}
