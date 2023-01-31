package ratelimit_test

// func TestAdaptiveRateLimit(t *testing.T) {
// 	limiter := ratelimit.NewUnlimited(context.Background())
// 	start := time.Now()

// 	for i := 0; i < 132; i++ {
// 		limiter.Take()
// 		// got 429 / hit ratelimit after 100
// 		if i == 100 {
// 			// Retry-After and new limiter (calibrate using different statergies)
// 			// new expected ratelimit 30req every 5 sec
// 			limiter.SleepandReset(time.Duration(5)*time.Second, 30, time.Duration(5)*time.Second)
// 		}
// 	}
// 	require.Equal(t, time.Since(start).Round(time.Second), time.Duration(10)*time.Second)
// }
