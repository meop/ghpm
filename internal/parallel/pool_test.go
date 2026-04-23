package parallel

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
	"testing"
)

func TestRunAllComplete(t *testing.T) {
	tasks := make([]Task, 10)
	for i := range tasks {
		i := i
		tasks[i] = Task{
			Name: fmt.Sprintf("task-%d", i),
			Run:  func() (any, error) { return i, nil },
		}
	}

	results := Run(context.Background(), tasks, 3)
	if len(results) != 10 {
		t.Fatalf("expected 10 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("unexpected error for %s: %v", r.Name, r.Err)
		}
	}
}

func TestRunErrorPropagation(t *testing.T) {
	tasks := []Task{
		{Name: "ok", Run: func() (any, error) { return "yes", nil }},
		{Name: "fail", Run: func() (any, error) { return nil, fmt.Errorf("boom") }},
	}

	results := Run(context.Background(), tasks, 2)
	errs := 0
	for _, r := range results {
		if r.Err != nil {
			errs++
		}
	}
	if errs != 1 {
		t.Errorf("expected 1 error, got %d", errs)
	}
}

func TestRunBoundedConcurrency(t *testing.T) {
	var concurrent int64
	var maxSeen int64

	tasks := make([]Task, 20)
	for i := range tasks {
		tasks[i] = Task{
			Name: fmt.Sprintf("t%d", i),
			Run: func() (any, error) {
				cur := atomic.AddInt64(&concurrent, 1)
				for {
					old := atomic.LoadInt64(&maxSeen)
					if cur <= old || atomic.CompareAndSwapInt64(&maxSeen, old, cur) {
						break
					}
				}
				atomic.AddInt64(&concurrent, -1)
				return nil, nil
			},
		}
	}

	Run(context.Background(), tasks, 4)

	if maxSeen > 4 {
		t.Errorf("concurrency exceeded workers: max observed %d", maxSeen)
	}
}

func TestRunCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	tasks := make([]Task, 5)
	for i := range tasks {
		i := i
		tasks[i] = Task{
			Name: fmt.Sprintf("t%d", i),
			Run:  func() (any, error) { return i, nil },
		}
	}

	results := Run(ctx, tasks, 2)
	if len(results) != 5 {
		t.Fatalf("expected 5 results (all reported), got %d", len(results))
	}
}

func TestRunResultNames(t *testing.T) {
	names := []string{"alpha", "beta", "gamma"}
	tasks := make([]Task, len(names))
	for i, n := range names {
		n := n
		tasks[i] = Task{Name: n, Run: func() (any, error) { return n, nil }}
	}

	results := Run(context.Background(), tasks, 3)
	got := make([]string, len(results))
	for i, r := range results {
		got[i] = r.Name
	}
	sort.Strings(got)
	sort.Strings(names)
	for i := range names {
		if got[i] != names[i] {
			t.Errorf("result names mismatch: got %v, want %v", got, names)
			break
		}
	}
}
