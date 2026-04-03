// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"sync"
	"sync/atomic"
)

// nextKeyID is a global counter for generating unique key identities.
var nextKeyID atomic.Uint64

// Key is a typed key for workflow-scoped values stored via [WithValue] and
// retrieved via [Lookup].
//
// Keys are identified by a unique ID, not by name. Two calls to [NewKey]
// with the same name produce distinct keys. Names are for debugging only.
type Key[V any] struct {
	id   uint64
	name string
}

// NewKey creates a new [Key] with the given name.
//
// Each call returns a distinct key, even if the name is the same. This follows
// the standard Go context-key pattern and prevents accidental collisions between
// packages.
func NewKey[V any](name string) Key[V] {
	return Key[V]{id: nextKeyID.Add(1), name: name}
}

// String returns the key's name for debugging.
func (k Key[V]) String() string {
	return k.name
}

// keyStore is a thread-safe key-value store shared across a workflow.
type keyStore struct {
	mu sync.RWMutex
	m  map[any]any
}

func (ks *keyStore) set(key, val any) {
	ks.mu.Lock()
	ks.m[key] = val
	ks.mu.Unlock()
}

func (ks *keyStore) get(key any) (any, bool) {
	ks.mu.RLock()
	val, ok := ks.m[key]
	ks.mu.RUnlock()
	return val, ok
}

// WithValue returns a [Step] that sets a workflow-scoped value before executing
// the given step.
//
// The value is stored in a shared [keyStore] that is visible everywhere in the
// workflow, including across [Spawn] boundaries where state T changes.
//
// WithValue is self-bootstrapping: if no flowCtx exists in the context, it
// creates one with a fresh keyStore. This means users don't need an outer
// [Named] or [Traced] wrapper just to use key-value storage.
//
// Example:
//
//	var DryRun = flow.NewKey[bool]("dry-run")
//
//	step := flow.WithValue(DryRun, true, myWorkflow)
func WithValue[T, V any](key Key[V], val V, step Step[T]) Step[T] {
	return func(ctx context.Context, t T) error {
		fc := getOrCreateFlowCtx(ctx)
		fc.keys.set(key, val)
		return step(fc, t)
	}
}

// Lookup retrieves a workflow-scoped value by key from the context.
//
// Returns the value and true if the key was found, or the zero value of V and
// false if the key was not set or no flowCtx exists.
//
// Example:
//
//	if dryRun, ok := flow.Lookup(ctx, DryRun); ok && dryRun {
//	    log.Println("skipping side effects (dry-run mode)")
//	    return nil
//	}
func Lookup[V any](ctx context.Context, key Key[V]) (V, bool) {
	f, ok := ctx.Value(flowCtxKey{}).(*flowCtx)
	if !ok || f.keys == nil {
		var zero V
		return zero, false
	}
	val, ok := f.keys.get(key)
	if !ok {
		var zero V
		return zero, false
	}
	return val.(V), true
}
