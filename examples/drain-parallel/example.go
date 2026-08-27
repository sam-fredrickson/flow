// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sam-fredrickson/flow"
)

// DrainParallel demonstrates streaming records out of a paginated API and
// saving them with a pool of workers, and how ParallelOptions.Prefetch keeps
// those workers busy while the next page is being fetched.
//
// The same pipeline runs twice, and each run prints a timeline of what the
// source and each worker were doing:
//
//  1. Without prefetching, an item is pulled only when a worker is free, so
//     the next page is fetched only once the current one has run out. Every
//     worker sits idle until the fetch returns.
//  2. With prefetching, a dedicated goroutine pulls items ahead of the
//     workers, so the next page is fetched while the current one is still
//     being saved.

const (
	PageCount  = 5
	PageSize   = 8
	Workers    = 4
	FetchDelay = 200 * time.Millisecond // per page
	SaveDelay  = 100 * time.Millisecond // per order
)

// State holds the API cursor and a timeline of the run for reporting.
type State struct {
	// API cursor. Only the source touches it, and DrainParallel never pulls
	// from the source concurrently, so it needs no synchronization.
	nextPage int

	timeline *Timeline
}

// Order is a record returned by the API.
type Order struct {
	ID     string
	Amount int
}

// FetchPage fetches the next page of orders from the (simulated) API,
// returning flow.ErrExhausted once every page has been fetched.
func FetchPage(ctx context.Context, state *State) ([]Order, error) {
	if state.nextPage >= PageCount {
		return nil, flow.ErrExhausted
	}
	page := state.nextPage + 1
	state.nextPage++

	defer state.timeline.Fetch()()
	if err := sleep(ctx, FetchDelay); err != nil {
		return nil, err
	}

	orders := make([]Order, PageSize)
	for i := range orders {
		orders[i] = Order{
			ID:     fmt.Sprintf("order-%d-%d", page, i+1),
			Amount: 10 * (i + 1),
		}
	}
	return orders, nil
}

// SaveOrder writes an order to the (simulated) warehouse.
func SaveOrder(ctx context.Context, state *State, order Order) error {
	defer state.timeline.Save()()
	return sleep(ctx, SaveDelay)
}

// SyncOrders streams orders out of the API one page at a time (Expand keeps
// at most one page buffered) and saves them with a pool of workers.
//
// A Prefetch of about one page lets the source fetch the next page as soon
// as the workers have started on the current one. Smaller values still
// help, but start the fetch later.
func SyncOrders(prefetch int) flow.Step[*State] {
	return flow.Expand(flow.Stream(FetchPage)).
		DrainParallel(SaveOrder, flow.ParallelOptions{
			Limit:    Workers,
			Prefetch: prefetch,
		})
}

func main() {
	fmt.Println("=== DrainParallel Example ===")
	fmt.Printf("%d pages of %d orders, %d workers. Fetching a page takes %v; saving an order takes %v.\n",
		PageCount, PageSize, Workers, FetchDelay, SaveDelay)

	run("Without prefetching (Prefetch: 0)", SyncOrders(0))
	run(fmt.Sprintf("With prefetching (Prefetch: %d, about one page)", PageSize), SyncOrders(PageSize))

	fmt.Println("\nKey Takeaways:")
	fmt.Println("• Without Prefetch, items are pulled only when a worker is free, so")
	fmt.Println("  every worker waits out each page fetch.")
	fmt.Println("• With Prefetch, the next page is fetched while workers are still busy.")
	fmt.Println("• Prefetch is opt-in: if the drain fails, prefetched items are discarded")
	fmt.Println("  unconsumed, which loses data if pulling removes or commits the item.")
}

func run(title string, step flow.Step[*State]) {
	fmt.Println()
	fmt.Println(title)
	fmt.Println(strings.Repeat("-", 60))

	state := &State{timeline: NewTimeline()}
	if err := step(context.Background(), state); err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		return
	}
	state.timeline.Print()
}

// sleep waits for d, or returns early if ctx is cancelled.
func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// =============================================================================
// Timeline: records what the source and each worker were doing, and when
// =============================================================================

// Tick is the width of one character in the printed timeline.
const Tick = 50 * time.Millisecond

type span struct{ from, to time.Duration }

// Timeline records fetch and save spans. Saves are assigned to lanes, one
// per worker, so the chart shows how busy the pool was.
type Timeline struct {
	start time.Time

	mu      sync.Mutex
	fetches []span
	busy    [Workers]bool
	saves   [Workers][]span
}

func NewTimeline() *Timeline {
	return &Timeline{start: time.Now()}
}

// Fetch marks the start of a page fetch and returns a func marking its end.
func (tl *Timeline) Fetch() (done func()) {
	from := time.Since(tl.start)
	return func() {
		tl.mu.Lock()
		defer tl.mu.Unlock()
		tl.fetches = append(tl.fetches, span{from, time.Since(tl.start)})
	}
}

// Save marks the start of a save on a free lane and returns a func marking
// its end. DrainParallel runs at most Workers saves at once, so a lane is
// always free.
func (tl *Timeline) Save() (done func()) {
	tl.mu.Lock()
	lane := slices.Index(tl.busy[:], false)
	tl.busy[lane] = true
	tl.mu.Unlock()

	from := time.Since(tl.start)
	return func() {
		tl.mu.Lock()
		defer tl.mu.Unlock()
		tl.busy[lane] = false
		tl.saves[lane] = append(tl.saves[lane], span{from, time.Since(tl.start)})
	}
}

// Print draws the timeline and a summary. Each character is one Tick:
// ▒ is a page fetch, █ is a worker saving an order, and · is idle.
func (tl *Timeline) Print() {
	elapsed := time.Since(tl.start)
	ticks := int((elapsed + Tick - 1) / Tick)

	ruler := []rune(strings.Repeat(" ", ticks+2))
	for s := 0; s*int(time.Second/Tick) < ticks; s++ {
		copy(ruler[s*int(time.Second/Tick):], []rune(fmt.Sprintf("%ds", s)))
	}
	fmt.Printf("%-9s %s\n", "", string(ruler))
	fmt.Printf("%-9s %s\n", "fetch", row(tl.fetches, ticks, '▒'))

	var saved int
	var busy time.Duration
	for i, spans := range tl.saves {
		fmt.Printf("%-9s %s\n", fmt.Sprintf("worker %d", i+1), row(spans, ticks, '█'))
		saved += len(spans)
		for _, s := range spans {
			busy += s.to - s.from
		}
	}

	fmt.Printf("\n✅ Saved %d orders in %.2fs (workers busy %.0f%% of the time)\n",
		saved, elapsed.Seconds(), 100*float64(busy)/float64(Workers*elapsed))
}

// row renders spans as ticks characters, marking each tick whose midpoint
// falls inside a span.
func row(spans []span, ticks int, mark rune) string {
	var b strings.Builder
	for t := range ticks {
		mid := time.Duration(t)*Tick + Tick/2
		c := '·'
		for _, s := range spans {
			if s.from <= mid && mid < s.to {
				c = mark
				break
			}
		}
		b.WriteRune(c)
	}
	return b.String()
}
