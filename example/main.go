package main

import (
	"context"
	"fmt"
	"time"

	"github.com/projectdiscovery/ratelimit"
)

func main() {
	// create a rate limiter by passing context, max tasks/tokens , time interval
	limiter := ratelimit.New(context.Background(), 5, 10*time.Second)

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
