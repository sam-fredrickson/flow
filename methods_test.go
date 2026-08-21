// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestFluentPipeline exercises the full Extract.Via/To chain, including an
// uninstantiated generic function (Flatten) as a Via argument.
func TestFluentPipeline(t *testing.T) {
	t.Parallel()

	getBatches := Extract[*CountingFlow, [][]int64](func(_ context.Context, _ *CountingFlow) ([][]int64, error) {
		return [][]int64{{1, 2}, {3, 4, 5}}, nil
	})

	step := getBatches.
		Via(Flatten).
		Via(Render(func(_ context.Context, _ *CountingFlow, n int64) (int64, error) {
			return n * 10, nil
		})).
		To(Apply(func(_ context.Context, c *CountingFlow, n int64) error {
			c.Counter += n
			return nil
		}))

	c := &CountingFlow{}
	if err := step(t.Context(), c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Counter != 150 {
		t.Errorf("expected counter 150, got %d", c.Counter)
	}
}

func TestTransformTo(t *testing.T) {
	t.Parallel()

	double := Transform[*CountingFlow, int64, int64](func(_ context.Context, _ *CountingFlow, n int64) (int64, error) {
		return n * 2, nil
	})
	consume := double.To(func(_ context.Context, c *CountingFlow, n int64) error {
		c.Counter = n
		return nil
	})

	c := &CountingFlow{}
	if err := consume(t.Context(), c, 21); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Counter != 42 {
		t.Errorf("expected counter 42, got %d", c.Counter)
	}
}

func TestStepThen(t *testing.T) {
	t.Parallel()

	add := func(n int64) Step[*CountingFlow] {
		return func(_ context.Context, c *CountingFlow) error {
			c.Counter += n
			return nil
		}
	}

	c := &CountingFlow{}
	step := add(1).Then(add(2)).Then(add(3))
	if err := step(t.Context(), c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Counter != 6 {
		t.Errorf("expected counter 6, got %d", c.Counter)
	}

	// Fail-fast: second step's error stops the chain.
	boom := errors.New("boom")
	c2 := &CountingFlow{}
	failing := add(1).Then(func(_ context.Context, _ *CountingFlow) error {
		return boom
	}).Then(add(100))
	if err := failing(t.Context(), c2); !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}
	if c2.Counter != 1 {
		t.Errorf("expected counter 1, got %d", c2.Counter)
	}
}

func TestStepDecoratorMethods(t *testing.T) {
	t.Parallel()

	t.Run("Named", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("boom")
		step := Step[*CountingFlow](func(_ context.Context, _ *CountingFlow) error {
			return boom
		}).Named("fail")
		err := step(t.Context(), &CountingFlow{})
		if err == nil || err.Error() != "fail: boom" {
			t.Errorf("expected 'fail: boom', got %v", err)
		}
	})

	t.Run("Retry", func(t *testing.T) {
		t.Parallel()
		attempts := 0
		step := Step[*CountingFlow](func(_ context.Context, _ *CountingFlow) error {
			attempts++
			if attempts < 3 {
				return fmt.Errorf("attempt %d", attempts)
			}
			return nil
		}).Retry(UpTo(5), FixedBackoff(time.Millisecond))
		if err := step(t.Context(), &CountingFlow{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if attempts != 3 {
			t.Errorf("expected 3 attempts, got %d", attempts)
		}
	})

	t.Run("WhenUnless", func(t *testing.T) {
		t.Parallel()
		yes := Predicate[*CountingFlow](func(_ context.Context, _ *CountingFlow) (bool, error) {
			return true, nil
		})
		incr := Step[*CountingFlow](func(_ context.Context, c *CountingFlow) error {
			c.Counter++
			return nil
		})

		c := &CountingFlow{}
		if err := incr.When(yes)(t.Context(), c); err != nil {
			t.Fatal(err)
		}
		if err := incr.Unless(yes)(t.Context(), c); err != nil {
			t.Fatal(err)
		}
		if c.Counter != 1 {
			t.Errorf("expected counter 1, got %d", c.Counter)
		}
	})

	t.Run("While", func(t *testing.T) {
		t.Parallel()
		c := &CountingFlow{}
		step := Step[*CountingFlow](func(_ context.Context, c *CountingFlow) error {
			c.Counter++
			return nil
		}).While(func(_ context.Context, c *CountingFlow) (bool, error) {
			return c.Counter < 5, nil
		})
		if err := step(t.Context(), c); err != nil {
			t.Fatal(err)
		}
		if c.Counter != 5 {
			t.Errorf("expected counter 5, got %d", c.Counter)
		}
	})

	t.Run("IgnoreError", func(t *testing.T) {
		t.Parallel()
		step := Step[*CountingFlow](func(_ context.Context, _ *CountingFlow) error {
			return errors.New("ignored")
		}).IgnoreError()
		if err := step(t.Context(), &CountingFlow{}); err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("OnError", func(t *testing.T) {
		t.Parallel()
		c := &CountingFlow{}
		step := Step[*CountingFlow](func(_ context.Context, _ *CountingFlow) error {
			return errors.New("primary failed")
		}).OnError(FallbackTo(Step[*CountingFlow](func(_ context.Context, c *CountingFlow) error {
			c.Counter = 99
			return nil
		})))
		if err := step(t.Context(), c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.Counter != 99 {
			t.Errorf("expected counter 99, got %d", c.Counter)
		}
	})

	t.Run("WithTimeout", func(t *testing.T) {
		t.Parallel()
		step := Sleep[*CountingFlow](time.Second).WithTimeout(10 * time.Millisecond)
		err := step(t.Context(), &CountingFlow{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected deadline exceeded, got %v", err)
		}
	})
}

func TestExtractSpawn(t *testing.T) {
	t.Parallel()

	type child struct{ n int64 }

	derive := Extract[*CountingFlow, *child](func(_ context.Context, c *CountingFlow) (*child, error) {
		return &child{n: c.Counter + 1}, nil
	})

	c := &CountingFlow{Counter: 41}
	step := derive.Spawn(func(_ context.Context, ch *child) error {
		if ch.n != 42 {
			return fmt.Errorf("expected child n=42, got %d", ch.n)
		}
		return nil
	})
	if err := step(t.Context(), c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStepScopedWithCleanupTimeout(t *testing.T) {
	t.Parallel()

	var order []string
	acquire := func(_ context.Context, _ *CountingFlow) error {
		order = append(order, "acquire")
		return nil
	}
	cleanup := func(_ context.Context, _ *CountingFlow) error {
		order = append(order, "cleanup")
		return nil
	}

	step := Do(
		Manage(acquire, cleanup),
		func(_ context.Context, _ *CountingFlow) error {
			order = append(order, "work")
			return nil
		},
	).Scoped().WithCleanupTimeout(time.Second)

	if err := step(t.Context(), &CountingFlow{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"acquire", "work", "cleanup"}
	if fmt.Sprint(order) != fmt.Sprint(want) {
		t.Errorf("expected order %v, got %v", want, order)
	}
}

func TestPredicateMethods(t *testing.T) {
	t.Parallel()

	pred := func(b bool) Predicate[*CountingFlow] {
		return func(_ context.Context, _ *CountingFlow) (bool, error) {
			return b, nil
		}
	}

	check := func(t *testing.T, p Predicate[*CountingFlow], want bool) {
		t.Helper()
		got, err := p(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("expected %v, got %v", want, got)
		}
	}

	check(t, pred(true).And(pred(true)), true)
	check(t, pred(true).And(pred(false)), false)
	check(t, pred(false).Or(pred(true)), true)
	check(t, pred(false).Or(pred(false)), false)
	check(t, pred(false).Not(), true)
	check(t, pred(true).And(pred(true)).Or(pred(false)).Not(), false)
}

func TestKeyMethods(t *testing.T) {
	t.Parallel()

	key := NewKey[string]("mode")

	var got string
	var found bool
	step := key.Set("fast", Step[*CountingFlow](func(ctx context.Context, _ *CountingFlow) error {
		got, found = key.Get(ctx)
		return nil
	}))

	if err := step(t.Context(), &CountingFlow{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found || got != "fast" {
		t.Errorf("expected (fast, true), got (%q, %v)", got, found)
	}

	// Get outside a workflow context finds nothing.
	if _, ok := key.Get(t.Context()); ok {
		t.Error("expected key to be absent outside workflow")
	}
}
