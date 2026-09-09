package ratelimit

import (
	"context"
	"testing"
)

func BenchmarkUnlimitedTake(b *testing.B) {
	limiter := NewUnlimited(context.Background())
	defer limiter.Stop()
	b.ReportAllocs()
	for b.Loop() {
		limiter.Take()
	}
}

func BenchmarkUnlimitedTakeParallel(b *testing.B) {
	limiter := NewUnlimited(context.Background())
	defer limiter.Stop()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			limiter.Take()
		}
	})
}

func BenchmarkUnlimitedLifecycle(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		limiter := NewUnlimited(context.Background())
		limiter.Stop()
		// Wait for the old implementation to exit so workers do not accumulate.
		if limiter.tokens != nil {
			for range limiter.tokens {
			}
		}
	}
}

func BenchmarkMultiLimiterUnlimitedTake(b *testing.B) {
	limiter, err := NewMultiLimiter(context.Background(), &Options{Key: "unlimited", IsUnlimited: true})
	if err != nil {
		b.Fatal(err)
	}
	defer limiter.Stop()
	b.ReportAllocs()
	for b.Loop() {
		if err := limiter.Take("unlimited"); err != nil {
			b.Fatal(err)
		}
	}
}
