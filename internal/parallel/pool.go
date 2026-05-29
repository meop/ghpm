package parallel

import (
	"context"
	"sync"
)

type Task[T any] struct {
	Name string
	Run  func() (T, error)
}

type Result[T any] struct {
	Name  string
	Value T
	Err   error
}

func Run[T any](ctx context.Context, tasks []Task[T], workers int) []Result[T] {
	if workers <= 0 {
		workers = 5
	}

	taskCh := make(chan Task[T], len(tasks))
	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)

	resultCh := make(chan Result[T], len(tasks))

	var wg sync.WaitGroup
	for range min(workers, len(tasks)) {
		wg.Go(func() {
			for task := range taskCh {
				if ctx.Err() != nil {
					resultCh <- Result[T]{Name: task.Name, Err: ctx.Err()}
					continue
				}
				val, err := task.Run()
				resultCh <- Result[T]{Name: task.Name, Value: val, Err: err}
			}
		})
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	results := make([]Result[T], 0, len(tasks))
	for r := range resultCh {
		results = append(results, r)
	}
	return results
}
