// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestKeyString(t *testing.T) {
	t.Parallel()
	key := NewKey[string]("my-key")
	if key.String() != "my-key" {
		t.Errorf("expected %q, got %q", "my-key", key.String())
	}
}

func TestWithValueAndLookup(t *testing.T) {
	t.Parallel()

	t.Run("BasicRoundTrip", func(t *testing.T) {
		t.Parallel()
		key := NewKey[string]("env")
		var got string
		var ok bool

		step := WithValue(key, "production",
			func(ctx context.Context, c *CountingFlow) error {
				got, ok = Lookup(ctx, key)
				return nil
			},
		)
		err := step(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected key to be found")
		}
		if got != "production" {
			t.Errorf("expected %q, got %q", "production", got)
		}
	})

	t.Run("MissingKeyReturnsFalse", func(t *testing.T) {
		t.Parallel()
		key := NewKey[int]("missing")

		step := Named("test", func(ctx context.Context, c *CountingFlow) error {
			val, ok := Lookup(ctx, key)
			if ok {
				t.Error("expected ok=false for missing key")
			}
			if val != 0 {
				t.Errorf("expected zero value, got %d", val)
			}
			return nil
		})
		err := step(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("NoFlowCtxReturnsFalse", func(t *testing.T) {
		t.Parallel()
		key := NewKey[string]("no-ctx")
		val, ok := Lookup(context.Background(), key)
		if ok {
			t.Error("expected ok=false with no flowCtx")
		}
		if val != "" {
			t.Errorf("expected zero value, got %q", val)
		}
	})

	t.Run("DistinctKeysWithSameName", func(t *testing.T) {
		t.Parallel()
		key1 := NewKey[string]("env")
		key2 := NewKey[string]("env")

		step := WithValue(key1, "first",
			WithValue(key2, "second",
				func(ctx context.Context, c *CountingFlow) error {
					v1, ok1 := Lookup(ctx, key1)
					v2, ok2 := Lookup(ctx, key2)
					if !ok1 || !ok2 {
						t.Fatal("expected both keys to be found")
					}
					if v1 != "first" {
						t.Errorf("key1: expected %q, got %q", "first", v1)
					}
					if v2 != "second" {
						t.Errorf("key2: expected %q, got %q", "second", v2)
					}
					return nil
				},
			),
		)
		err := step(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ConcurrentReadWrite", func(t *testing.T) {
		t.Parallel()
		key := NewKey[int]("counter")
		const n = 100

		step := WithValue(key, 0,
			func(ctx context.Context, c *CountingFlow) error {
				// Get the flowCtx to access keys directly for concurrent writes
				f := ctx.Value(flowCtxKey{}).(*flowCtx)
				var wg sync.WaitGroup
				wg.Add(n * 2)
				for i := range n {
					go func(v int) {
						defer wg.Done()
						f.keys.set(NewKey[int]("k"), v)
					}(i)
					go func() {
						defer wg.Done()
						Lookup(ctx, key)
					}()
				}
				wg.Wait()
				return nil
			},
		)
		err := step(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("CrossSpawnPropagation", func(t *testing.T) {
		t.Parallel()
		key := NewKey[string]("shared")

		step := WithValue(key, "from-parent",
			Spawn(
				func(ctx context.Context, c *CountingFlow) (*ChildState, error) {
					return &ChildState{}, nil
				},
				func(ctx context.Context, child *ChildState) error {
					val, ok := Lookup(ctx, key)
					if !ok {
						return errorf("expected key to be found in child")
					}
					if val != "from-parent" {
						return errorf("expected %q, got %q", "from-parent", val)
					}
					return nil
				},
			),
		)
		err := step(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("SelfBootstrapping", func(t *testing.T) {
		t.Parallel()
		key := NewKey[bool]("flag")

		// WithValue without any outer Named/Traced
		step := WithValue(key, true,
			func(ctx context.Context, c *CountingFlow) error {
				val, ok := Lookup(ctx, key)
				if !ok {
					t.Error("expected key to be found")
				}
				if !val {
					t.Error("expected true")
				}
				return nil
			},
		)
		err := step(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("MultipleLayersCompose", func(t *testing.T) {
		t.Parallel()
		keyA := NewKey[string]("a")
		keyB := NewKey[int]("b")

		step := WithValue(keyA, "alpha",
			WithValue(keyB, 42,
				func(ctx context.Context, c *CountingFlow) error {
					va, okA := Lookup(ctx, keyA)
					vb, okB := Lookup(ctx, keyB)
					if !okA || !okB {
						t.Fatal("expected both keys to be found")
					}
					if va != "alpha" {
						t.Errorf("keyA: expected %q, got %q", "alpha", va)
					}
					if vb != 42 {
						t.Errorf("keyB: expected %d, got %d", 42, vb)
					}
					return nil
				},
			),
		)
		err := step(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// errorf returns a simple error with a formatted message.
func errorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
