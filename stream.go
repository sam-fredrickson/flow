// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"runtime"
)

// Source is an [Extract] that follows the pull-based iterator protocol:
// each call produces the next item, and [ErrExhausted] signals the end.
//
// A Source is what [Collect] consumes; the streaming combinators here
// ([Expand], [Drain], [DrainParallel]) let pipelines process items one at a
// time instead of materializing them into slices.
//
// The defined type exists to make the protocol explicit and to host the
// fluent streaming methods ([Source.Via], [Source.Drain], and friends),
// which operate per-item rather than per-slice. Because their signatures
// never mention []U, they avoid the instantiation cycle that rules out
// slice-lifting methods on [Transform] and [Consume].
//
// Sources produced by [Expand] carry iteration state in the closure and are
// one-shot: build a fresh one (by rebuilding the workflow value) for each
// run. Sources whose state lives in T, like a pager storing its cursor in
// state, are as reusable as that state allows.
type Source[T, U any] Extract[T, U]

// Stream marks an [Extract] as a pull-based [Source].
//
// This is a conversion, not a wrapper: the extract must already follow the
// iterator protocol, returning [ErrExhausted] when no more items are
// available.
//
// Example:
//
//	flow.Stream(FetchNextItem).
//	    Via(ValidateItem).
//	    Drain(SaveItem)
func Stream[T, U any](next Extract[T, U]) Source[T, U] {
	return Source[T, U](next)
}

// Expand turns a batch-producing extract into a per-item [Source].
//
// This is the streaming counterpart of [Collect] + [Flatten]: instead of
// realizing every batch into one big slice, at most one batch is buffered
// at a time. Empty batches are skipped. [ErrExhausted] and other errors
// from the underlying extract pass through unchanged.
//
// The returned Source buffers the current batch in its closure, so it is
// one-shot and must not be pulled from concurrently. [DrainParallel]
// serializes its pulls, so draining an expanded source in parallel is safe.
//
// Example:
//
//	// Stream records out of a paginated API, one page buffered at a time.
//	flow.Expand(flow.Stream(FetchNextPage).Via(ExtractRecords)).
//	    Via(ValidateRecord).
//	    Drain(SaveRecord)
func Expand[T, U any](next Source[T, []U]) Source[T, U] {
	var batch []U
	var i int
	return func(ctx context.Context, t T) (U, error) {
		var zero U
		for i >= len(batch) {
			if err := ctx.Err(); err != nil {
				return zero, err
			}
			b, err := next(ctx, t)
			if err != nil {
				return zero, err
			}
			batch, i = b, 0
		}
		u := batch[i]
		i++
		return u, nil
	}
}

// Drain pulls items from a [Source] until [ErrExhausted], consuming each
// one (serial, fail-fast).
//
// This is the streaming counterpart of [Collect] + [Apply]: no slice is
// ever realized. Per-item errors, from the source or the consumer, are
// wrapped with [IndexedError].
//
// Example:
//
//	flow.Drain(
//	    flow.Expand(flow.Stream(FetchNextPage).Via(ExtractRecords)),
//	    SaveRecord,
//	)
func Drain[T, U any](next Source[T, U], consume Consume[T, U]) Step[T] {
	return func(ctx context.Context, t T) error {
		for i := 0; ; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}

			u, err := next(ctx, t)
			if err != nil {
				if errors.Is(err, ErrExhausted) {
					return nil
				}
				return &IndexedError{Index: i, Err: err}
			}

			if err := consume(ctx, t, u); err != nil {
				return &IndexedError{Index: i, Err: err}
			}
		}
	}
}

// DrainParallel pulls items from a [Source] until [ErrExhausted], consuming
// them concurrently with a pool of workers.
//
// Pulls from the source are serialized, so the source needs no internal
// synchronization; consumption runs on up to opts.Limit goroutines (or
// [runtime.GOMAXPROCS] workers when opts.Limit <= 0, since an unbounded
// pool has no meaning for a pull-based loop). Items are in flight as soon
// as they are pulled, so consumption order is unspecified. Per-item errors
// are wrapped with [IndexedError], indexed by pull order.
//
// By default, an item is pulled only when a worker is ready for it, so a
// slow source stalls workers that finish while it is producing. With
// opts.Prefetch > 0, a dedicated goroutine pulls up to that many items
// ahead of the workers instead. Either way, the source is never called
// after DrainParallel returns.
//
// If a global semaphore is present in ctx (via [WithMaxConcurrency]), each
// worker acquires it around consuming an item.
//
// By default, the first error cancels the remaining work. With
// opts.JoinErrors, consumer errors are recorded and the pool keeps going;
// an error from the source itself still stops pulling, since the source is
// presumed broken. All recorded errors are joined in the result.
//
// Example:
//
//	flow.Expand(flow.Stream(FetchNextPage).Via(ExtractRecords)).
//	    Via(ValidateRecord).
//	    DrainParallel(SaveRecord, flow.ParallelOptions{Limit: 8, Prefetch: 16})
func DrainParallel[T, U any](
	next Source[T, U],
	consume Consume[T, U],
	opts ParallelOptions,
) Step[T] {
	return func(ctx context.Context, t T) error {
		workers := opts.Limit
		if workers <= 0 {
			workers = runtime.GOMAXPROCS(0)
		}

		pull := func(ctx context.Context) (U, error) { return next(ctx, t) }
		if opts.Prefetch > 0 {
			var stop func()
			pull, stop = prefetch(ctx, t, next, opts.Prefetch)
			defer stop()
		}

		return parallelPull(ctx, workers, opts,
			func(ctx context.Context, i int) (U, error) {
				u, err := pull(ctx)
				if err != nil && !errors.Is(err, ErrExhausted) {
					return u, &IndexedError{Index: i, Err: err}
				}
				return u, err
			},
			func(ctx context.Context, i int, u U) error {
				if err := consume(ctx, t, u); err != nil {
					return &IndexedError{Index: i, Err: err}
				}
				return nil
			},
		)
	}
}

// prefetch starts a goroutine that pulls from next ahead of demand, keeping
// at most n pulled items that pull has not yet returned.
//
// pull returns the results in order, or ctx's error if ctx is done first.
// The goroutine stops after the first error, including [ErrExhausted], and
// delivers it as the final result, so pull must not be called again after
// returning one. stop cancels the goroutine, aborting any pull in progress,
// and waits for it to exit, so next is never called after stop returns.
func prefetch[T, U any](
	ctx context.Context,
	t T,
	next Source[T, U],
	n int,
) (pull func(context.Context) (U, error), stop func()) {
	type result struct {
		u   U
		err error
	}
	// The goroutine holds one pulled item while blocked sending, so the
	// buffer holds the other n-1.
	results := make(chan result, n-1)
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for ctx.Err() == nil {
			u, err := next(ctx, t)
			select {
			case results <- result{u: u, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	pull = func(ctx context.Context) (U, error) {
		select {
		case r := <-results:
			return r.u, r.err
		case <-ctx.Done():
			var zero U
			return zero, ctx.Err()
		}
	}
	stop = func() {
		cancel()
		<-done
	}
	return pull, stop
}
