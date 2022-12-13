package main

import (
	"context"
	"fmt"
	"time"

	"github.com/projectdiscovery/ratelimit"
)

func main() {

	fmt.Printf("[+] Complete Tasks Using MulitLimiter with unique key\n")

	multiLimiter := ratelimit.NewMultiLimiter(context.Background())
	multiLimiter.Add("default", 10)
	save1 := time.Now()

	for i := 0; i < 11; i++ {
		multiLimiter.Take("default")
		fmt.Printf("MulitKey Task %v completed after %v\n", i, time.Since(save1))
	}

	fmt.Printf("\n[+] Complete Tasks Using Limiter\n")

	// create a rate limiter by passing context, max tasks/tokens , time interval
	limiter := ratelimit.New(context.Background(), 5, time.Duration(10*time.Second))

	save := time.Now()

	for i := 0; i < 10; i++ {
		// run limiter.Take() method before each task
		limiter.Take()
		fmt.Printf("Task %v completed after %v\n", i, time.Since(save))
	}

	/*
		Output:
		Task 0 completed after 4.083µs
		Task 1 completed after 111.416µs
		Task 2 completed after 118µs
		Task 3 completed after 121.083µs
		Task 4 completed after 124.583µs
		Task 5 completed after 10.001356375s
		Task 6 completed after 10.001524791s
		Task 7 completed after 10.001537583s
		Task 8 completed after 10.001542708s
		Task 9 completed after 10.001548666s
	*/
}
