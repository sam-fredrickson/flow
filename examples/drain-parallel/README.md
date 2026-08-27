# DrainParallel Example

This example demonstrates streaming records out of a paginated API with `Expand` and saving them concurrently with `DrainParallel`, and how `ParallelOptions.Prefetch` keeps the worker pool busy while the next page is fetched.

## The Pipeline

```go
flow.Expand(flow.Stream(FetchPage)).
    DrainParallel(SaveOrder, flow.ParallelOptions{
        Limit:    4, // workers
        Prefetch: 8, // items pulled ahead of the workers
    })
```

- `Stream(FetchPage)` pulls one page of orders per call, until `ErrExhausted`.
- `Expand` turns pages into individual orders, buffering one page at a time.
- `DrainParallel` saves orders on a pool of workers.

## Running the Example

```bash
cd examples/drain-parallel
go run .
```

The pipeline runs twice, once without prefetching and once with it, and each run prints a timeline: `▒` is a page fetch, `█` is a worker saving an order, and `·` is idle.

```
Without prefetching (Prefetch: 0)
------------------------------------------------------------
          0s                  1s                  2s
fetch     ▒▒▒▒····▒▒▒▒····▒▒▒▒····▒▒▒▒····▒▒▒▒·····
worker 1  ····████····████····████····████····████·
worker 2  ····████····████····████····████····████·
worker 3  ····████····████····████····████····████·
worker 4  ····████····████····████····████····████·

✅ Saved 40 orders in 2.02s (workers busy 50% of the time)

With prefetching (Prefetch: 8, about one page)
------------------------------------------------------------
          0s                  1s
fetch     ▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒·····
worker 1  ····████████████████████·
worker 2  ····████████████████████·
worker 3  ····████████████████████·
worker 4  ····████████████████████·

✅ Saved 40 orders in 1.21s (workers busy 83% of the time)
```

## Why Prefetching Helps

By default, `DrainParallel` pulls an item only when a worker is ready for it. Once the current page runs out, the next worker to ask triggers the fetch of the next page, and every worker that finishes in the meantime waits for it to return.

With `Prefetch`, a dedicated goroutine pulls items ahead of the workers, keeping up to `Prefetch` items pulled but not yet taken. The next page is fetched while the workers are still saving the current one. A `Prefetch` of about one page starts that fetch as soon as the workers have started on the current page.

## Why It's Opt-In

If the drain stops early, because of an error or cancellation, up to `Prefetch` pulled items are discarded without being consumed. That's harmless for a paginated read, but for a source where pulling removes or commits the item, like popping from a list or auto-acknowledging queue receives, those items would be lost. Queues that are acknowledged after consuming are fine: discarded items are never acknowledged, so the queue redelivers them. Prefetching is off unless `Prefetch` is set.

Either way, the source is never pulled concurrently and is never called after `DrainParallel` returns.
