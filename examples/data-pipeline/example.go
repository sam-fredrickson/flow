// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sam-fredrickson/flow"
)

// DataPipeline demonstrates using collection operators (Collect, Render, Apply)
// to build clean, composable data processing pipelines.
//
// This example shows a typical ETL workflow:
// 1. Collect: Pull data from a paginated API
// 2. Render: Transform and validate each record
// 3. Apply: Save each processed record

// State represents the application state for the pipeline.
type State struct {
	// API client state
	apiCursor   string
	apiPageNum  int
	apiFinished bool

	// Statistics
	recordsProcessed int
	recordsSaved     int
}

// RawRecord represents data from the API.
type RawRecord struct {
	ID        string
	Name      string
	Value     int
	Timestamp string
}

// ValidatedRecord represents validated data.
type ValidatedRecord struct {
	ID        string
	Name      string
	Value     int
	Timestamp time.Time
}

// EnrichedRecord represents enriched data ready for storage.
type EnrichedRecord struct {
	ID              string
	Name            string
	NormalizedValue float64
	Timestamp       time.Time
	ProcessedAt     time.Time
	Category        string
}

// =============================================================================
// Example 1: Complete ETL Pipeline with Collect, Render, Apply
// =============================================================================

// ProcessAllData demonstrates the full pipeline:
// Collect pages → Extract records → Validate → Enrich → Save.
func ProcessAllData() flow.Step[*State] {
	return flow.With(
		flow.From(
			flow.Collect(FetchNextPage), // Collect all pages from API
			flow.Chain4(
				flow.Render(ExtractRecords), // Extract records from each page: []Page → [][]Record
				flow.Flatten,                // Flatten: [][]Record → []Record
				flow.Render(ValidateRecord), // Validate each: []Record → []ValidatedRecord
				flow.Render(EnrichRecord),   // Enrich each: []ValidatedRecord → []EnrichedRecord
			),
		),
		flow.Apply(SaveRecord), // Save each enriched record
	)
}

// =============================================================================
// Example 2: Simple Queue Processing with Collect + Apply
// =============================================================================

// ProcessQueue demonstrates a simpler pattern: pull items from queue and process.
func ProcessQueue() flow.Step[*State] {
	return flow.With(
		flow.Collect(PopNextItem),    // Pull items until queue is empty
		flow.Apply(ProcessQueueItem), // Process each item
	)
}

// =============================================================================
// Example 3: Batch Transform with Render
// =============================================================================

// TransformBatch demonstrates batch transformation without collection.
func TransformBatch() flow.Step[*State] {
	return flow.Pipeline(
		GetCachedRecords, // Extract[*State, []RawRecord]
		flow.Chain(
			flow.Render(ValidateRecord), // Transform each: RawRecord → ValidatedRecord
			flow.Render(EnrichRecord),   // Transform each: ValidatedRecord → EnrichedRecord
		),
		flow.Apply(SaveRecord), // Save each
	)
}

// =============================================================================
// Collection: Collect - Pull data from paginated API
// =============================================================================

// FetchNextPage fetches the next page from the API.
// Returns flow.ErrExhausted when no more pages are available.
func FetchNextPage(ctx context.Context, state *State) (APIPage, error) {
	if state.apiFinished {
		return APIPage{}, flow.ErrExhausted
	}

	// Simulate API call with delay
	time.Sleep(10 * time.Millisecond)

	// Simulate pagination (3 pages total)
	state.apiPageNum++
	page := APIPage{
		PageNum: state.apiPageNum,
		Data:    generateMockPageData(state.apiPageNum),
	}

	// Check if this is the last page
	if state.apiPageNum >= 3 {
		state.apiFinished = true
		page.NextCursor = ""
	} else {
		page.NextCursor = fmt.Sprintf("cursor_%d", state.apiPageNum+1)
	}

	state.apiCursor = page.NextCursor
	fmt.Printf("📥 Fetched page %d (%d records)\n", page.PageNum, len(page.Data))

	return page, nil
}

// PopNextItem pops the next item from a queue.
// Returns flow.ErrExhausted when queue is empty.
func PopNextItem(ctx context.Context, state *State) (string, error) {
	// In a real implementation, this would pop from an actual queue
	// For this example, we'll simulate a queue with 5 items
	if state.recordsProcessed >= 5 {
		return "", flow.ErrExhausted
	}

	state.recordsProcessed++
	return fmt.Sprintf("queue-item-%d", state.recordsProcessed), nil
}

// =============================================================================
// Transformation: Render - Transform data
// =============================================================================

// ExtractRecords extracts the records array from an API page.
func ExtractRecords(ctx context.Context, state *State, page APIPage) ([]RawRecord, error) {
	return page.Data, nil
}

// ValidateRecord validates and parses a raw record.
func ValidateRecord(ctx context.Context, state *State, raw RawRecord) (ValidatedRecord, error) {
	// Validate required fields
	if raw.ID == "" {
		return ValidatedRecord{}, fmt.Errorf("missing ID")
	}
	if raw.Name == "" {
		return ValidatedRecord{}, fmt.Errorf("missing Name")
	}

	// Parse timestamp
	timestamp, err := time.Parse(time.RFC3339, raw.Timestamp)
	if err != nil {
		return ValidatedRecord{}, fmt.Errorf("invalid timestamp: %w", err)
	}

	return ValidatedRecord{
		ID:        raw.ID,
		Name:      raw.Name,
		Value:     raw.Value,
		Timestamp: timestamp,
	}, nil
}

// EnrichRecord enriches a validated record with additional computed fields.
func EnrichRecord(ctx context.Context, state *State, validated ValidatedRecord) (EnrichedRecord, error) {
	// Simulate enrichment logic
	normalized := float64(validated.Value) / 100.0
	category := categorizeValue(validated.Value)

	return EnrichedRecord{
		ID:              validated.ID,
		Name:            validated.Name,
		NormalizedValue: normalized,
		Timestamp:       validated.Timestamp,
		ProcessedAt:     time.Now(),
		Category:        category,
	}, nil
}

// =============================================================================
// Consumption: Apply - Save data
// =============================================================================

// SaveRecord saves an enriched record to the database.
func SaveRecord(ctx context.Context, state *State, record EnrichedRecord) error {
	// Simulate database save with delay
	time.Sleep(5 * time.Millisecond)

	state.recordsSaved++
	fmt.Printf("💾 Saved record %s: %s (%.2f, %s)\n",
		record.ID, record.Name, record.NormalizedValue, record.Category)

	return nil
}

// ProcessQueueItem processes a queue item.
func ProcessQueueItem(ctx context.Context, state *State, item string) error {
	fmt.Printf("⚙️  Processing queue item: %s\n", item)
	time.Sleep(10 * time.Millisecond)
	return nil
}

// =============================================================================
// Helper Types and Functions
// =============================================================================

type APIPage struct {
	PageNum    int
	Data       []RawRecord
	NextCursor string
}

// GetCachedRecords simulates fetching records from a cache.
func GetCachedRecords(ctx context.Context, state *State) ([]RawRecord, error) {
	return generateMockPageData(1), nil
}

func generateMockPageData(pageNum int) []RawRecord {
	baseID := (pageNum - 1) * 10
	records := make([]RawRecord, 10)

	for i := 0; i < 10; i++ {
		id := baseID + i + 1
		records[i] = RawRecord{
			ID:        fmt.Sprintf("rec-%03d", id),
			Name:      fmt.Sprintf("Record %d", id),
			Value:     50 + (id * 13 % 100), // Varying values
			Timestamp: time.Now().Add(-time.Duration(id) * time.Hour).Format(time.RFC3339),
		}
	}

	return records
}

func categorizeValue(value int) string {
	switch {
	case value < 50:
		return "low"
	case value < 100:
		return "medium"
	default:
		return "high"
	}
}

// =============================================================================
// Main - Demonstrate the examples
// =============================================================================

func main() {
	fmt.Println("=== Data Pipeline Examples ===")
	fmt.Println()

	// Example 1: Complete ETL Pipeline
	fmt.Println("Example 1: Complete ETL Pipeline (Collect → Render → Apply)")
	fmt.Println(strings.Repeat("-", 60))
	state1 := &State{}
	ctx := context.Background()

	if err := ProcessAllData()(ctx, state1); err != nil {
		fmt.Printf("❌ Error: %v\n", err)
	} else {
		fmt.Printf("\n✅ Pipeline completed: %d records saved\n", state1.recordsSaved)
	}

	// Example 2: Queue Processing
	fmt.Println()
	fmt.Println()
	fmt.Println("Example 2: Queue Processing (Collect → Apply)")
	fmt.Println(strings.Repeat("-", 60))
	state2 := &State{}

	if err := ProcessQueue()(ctx, state2); err != nil {
		fmt.Printf("❌ Error: %v\n", err)
	} else {
		fmt.Printf("\n✅ Queue processing completed: %d items processed\n", state2.recordsProcessed)
	}

	// Example 3: Batch Transform
	fmt.Println()
	fmt.Println()
	fmt.Println("Example 3: Batch Transform (Extract → Render → Apply)")
	fmt.Println(strings.Repeat("-", 60))
	state3 := &State{}

	if err := TransformBatch()(ctx, state3); err != nil {
		fmt.Printf("❌ Error: %v\n", err)
	} else {
		fmt.Printf("\n✅ Batch transform completed: %d records saved\n", state3.recordsSaved)
	}

	// Example 4: Demonstrate error handling in Render/Apply (fail-fast)
	fmt.Println()
	fmt.Println()
	fmt.Println("Example 4: Error Handling (Render with invalid data)")
	fmt.Println(strings.Repeat("-", 60))
	state4 := &State{}

	// Create a pipeline that will fail on validation
	failPipeline := flow.Pipeline(
		func(ctx context.Context, state *State) ([]RawRecord, error) {
			return []RawRecord{
				{ID: "valid-1", Name: "Valid", Value: 100, Timestamp: time.Now().Format(time.RFC3339)},
				{ID: "", Name: "Invalid", Value: 50, Timestamp: time.Now().Format(time.RFC3339)}, // Missing ID
				{ID: "valid-2", Name: "Valid", Value: 75, Timestamp: time.Now().Format(time.RFC3339)},
			}, nil
		},
		flow.Render(ValidateRecord),
		flow.Apply(func(ctx context.Context, state *State, record ValidatedRecord) error {
			fmt.Printf("💾 Would save: %s\n", record.ID)
			return nil
		}),
	)

	if err := failPipeline(ctx, state4); err != nil {
		fmt.Printf("❌ Expected error (fail-fast): %v\n", err)
	}

	fmt.Println("\n=== Examples Complete ===")
	fmt.Println("\nKey Takeaways:")
	fmt.Println("• Collect: Repeatedly extract until exhausted (iterator pattern)")
	fmt.Println("• Render: Transform each element in a slice (serial, fail-fast)")
	fmt.Println("• Apply: Consume each element in a slice (serial, fail-fast)")
	fmt.Println("• All three compose cleanly with Pipeline, From, Chain, Feed, With")
}
