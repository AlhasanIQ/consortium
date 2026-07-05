package jobs

import (
	"sync"
	"sync/atomic"
)

// RunConcurrently executes task functions using a fixed worker pool.
// Each task receives its index and runs to completion (including any
// retries the caller wants to embed). The optional progress callback
// fires after each task completes.
func RunConcurrently(
	tasks []func(),
	concurrency int,
	progress func(completed, total int),
) {
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(tasks) {
		concurrency = len(tasks)
	}

	work := make(chan func())
	var completed int64
	total := len(tasks)

	var wg sync.WaitGroup
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range work {
				task()
				if progress != nil {
					done := int(atomic.AddInt64(&completed, 1))
					progress(done, total)
				}
			}
		}()
	}

	for _, task := range tasks {
		work <- task
	}
	close(work)

	wg.Wait()
}
