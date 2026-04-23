package parallel

import (
	"context"
	"sync"
)

type Task struct {
	Name string
	Run  func() (any, error)
}

type Result struct {
	Name  string
	Value any
	Err   error
}

// Run executes tasks using a bounded worker pool and returns all results.
func Run(ctx context.Context, tasks []Task, workers int) []Result {
	if workers <= 0 {
		workers = 5
	}

	taskCh := make(chan Task, len(tasks))
	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)

	resultCh := make(chan Result, len(tasks))

	var wg sync.WaitGroup
	for i := 0; i < workers && i < len(tasks); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskCh {
				select {
				case <-ctx.Done():
					resultCh <- Result{Name: task.Name, Err: ctx.Err()}
				default:
					val, err := task.Run()
					resultCh <- Result{Name: task.Name, Value: val, Err: err}
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	results := make([]Result, 0, len(tasks))
	for r := range resultCh {
		results = append(results, r)
	}
	return results
}
