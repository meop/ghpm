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

// Run executes tasks across at most workers goroutines and returns their
// results in input order (results[i] corresponds to tasks[i]), regardless of
// completion order, so callers get deterministic, stable output.
func Run[T any](ctx context.Context, tasks []Task[T], workers int) []Result[T] {
	if workers <= 0 {
		workers = 5
	}

	type indexedTask struct {
		idx  int
		task Task[T]
	}
	taskCh := make(chan indexedTask, len(tasks))
	for i, t := range tasks {
		taskCh <- indexedTask{idx: i, task: t}
	}
	close(taskCh)

	// Each worker writes only to results[it.idx]; distinct indices are
	// independent memory, so no per-element synchronization is needed.
	results := make([]Result[T], len(tasks))

	var wg sync.WaitGroup
	for range min(workers, len(tasks)) {
		wg.Go(func() {
			for it := range taskCh {
				if ctx.Err() != nil {
					results[it.idx] = Result[T]{Name: it.task.Name, Err: ctx.Err()}
					continue
				}
				val, err := it.task.Run()
				results[it.idx] = Result[T]{Name: it.task.Name, Value: val, Err: err}
			}
		})
	}
	wg.Wait()

	return results
}
