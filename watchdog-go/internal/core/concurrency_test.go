// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func receiveStart(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not enter inference")
	}
}

func TestModelConcurrencyLimit(t *testing.T) {
	for _, tc := range []struct {
		backend string
		limit   int
	}{
		{"apple-api", 1}, {"apple", 1}, {"gguf", 4},
		{"mlx", 4}, {"omlx", 4}, {"plamo-mlx", 4},
	} {
		t.Run(tc.backend, func(t *testing.T) {
			m := testManager(t)
			m.current.Type, m.models[0].Type = tc.backend, tc.backend
			var launches atomic.Int32
			start := m.start
			m.start = func(ctx context.Context, model Model) (*child, error) { launches.Add(1); return start(ctx, model) }
			started, finish := make(chan struct{}, tc.limit+1), make(chan struct{})
			defer close(finish)
			results := make(chan error, tc.limit+1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			run := func(ctx context.Context, alias string) {
				results <- m.WithModel(ctx, alias, func(ctx context.Context, _ Model) error {
					started <- struct{}{}
					select {
					case <-finish:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
			}
			go run(ctx, "default")
			receiveStart(t, started)
			for i := 1; i < tc.limit; i++ {
				go run(context.Background(), m.models[0].Path)
				receiveStart(t, started)
			}
			if got := m.Snapshot().ActiveRequests; got != tc.limit {
				t.Fatalf("active=%d, want %d", got, tc.limit)
			}
			if got := launches.Load(); got != 1 {
				t.Fatalf("launched %d model processes", got)
			}
			if err := m.Sleep(context.Background()); !errors.Is(err, ErrBusy) {
				t.Fatalf("sleep interrupted inference: %v", err)
			}
			if err := m.Switch(context.Background(), "fixture"); !errors.Is(err, ErrBusy) {
				t.Fatalf("switch interrupted inference: %v", err)
			}
			go run(context.Background(), "")
			select {
			case <-started:
				t.Fatal("request bypassed concurrency limit")
			case <-time.After(30 * time.Millisecond):
			}
			cancel()
			if err := <-results; !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			receiveStart(t, started)
			if m.Snapshot().ActiveRequests != tc.limit {
				t.Fatal("cancel did not release exactly one slot")
			}
			if err := m.Stop(); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < tc.limit; i++ {
				if err := <-results; !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			}
			if m.Snapshot().ActiveRequests != 0 {
				t.Fatal("shutdown left active leases")
			}
		})
	}
}

func TestQueuedCancellationAndModelSwitchWaitForAllResponses(t *testing.T) {
	m := testManager(t)
	fixture, ok := FindModel(m.models, "fixture")
	if !ok {
		t.Fatal("GGUF fixture is missing")
	}
	other := fixture
	other.ID, other.Path = "other", "/fixture/other.gguf"
	m.models = append(m.models, other)
	started := make(chan struct{}, 2)
	finish := []chan struct{}{make(chan struct{}), make(chan struct{})}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			results <- m.WithModel(context.Background(), fixture.ID, func(ctx context.Context, _ Model) error {
				started <- struct{}{}
				select {
				case <-finish[i]:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}(i)
	}
	receiveStart(t, started)
	receiveStart(t, started)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := m.WithModel(ctx, other.ID, func(context.Context, Model) error { t.Error("cancelled queued request entered inference"); return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	switched := make(chan error, 1)
	go func() {
		switched <- m.WithModel(context.Background(), other.ID, func(_ context.Context, model Model) error {
			if model.ID != other.ID {
				return errors.New("wrong model")
			}
			return nil
		})
	}()
	close(finish[0])
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-switched:
		t.Fatalf("switched with one active response: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(finish[1])
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-switched:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("model switch stuck")
	}
	if m.Snapshot().RequestCount != 3 {
		t.Fatal("queued cancellation counted as inference")
	}
}
