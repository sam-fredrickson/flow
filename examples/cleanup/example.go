// SPDX-License-Identifier: Apache-2.0

// Example cleanup demonstrates scope-based resource cleanup with the flow library.
//
// It uses Manage (the primary API) to acquire simulated resources stored in
// state, showing LIFO cleanup ordering (transaction rolled back before
// connection closed) and WithCleanupTimeout for resilient cleanup.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/sam-fredrickson/flow"
)

// AppState holds the application state for the workflow.
type AppState struct {
	DSN  string
	Conn string // simulated connection handle
	TxID string // simulated transaction ID
}

// OpenConnection simulates opening a database connection, storing it in state.
func OpenConnection(ctx context.Context, s *AppState) error {
	fmt.Println("  opening database connection")
	s.Conn = s.DSN
	return nil
}

// CloseConnection simulates closing the database connection from state.
func CloseConnection(ctx context.Context, s *AppState) error {
	fmt.Printf("  closing database connection %s\n", s.Conn)
	return nil
}

// BeginTx simulates beginning a transaction, storing it in state.
func BeginTx(ctx context.Context, s *AppState) error {
	fmt.Println("  beginning transaction")
	s.TxID = "tx-001"
	return nil
}

// RollbackTx simulates rolling back the transaction from state.
func RollbackTx(ctx context.Context, s *AppState) error {
	fmt.Printf("  rolling back transaction %s\n", s.TxID)
	return nil
}

// InsertRecords simulates inserting records within the active transaction.
func InsertRecords(ctx context.Context, s *AppState) error {
	fmt.Printf("  inserting records (conn=%s, tx=%s)\n", s.Conn, s.TxID)
	return nil
}

func main() {
	workflow := flow.WithCleanupTimeout(10*time.Second,
		flow.Scope(
			flow.Do(
				// Manage: acquire a connection, cleanup closes it.
				flow.Manage(OpenConnection, CloseConnection),
				// Manage: begin a transaction, cleanup rolls it back.
				flow.Manage(BeginTx, RollbackTx),
				// Do work with the resources stored in state.
				InsertRecords,
			),
		),
	)

	fmt.Println("running workflow:")
	state := &AppState{DSN: "postgres://localhost/mydb"}
	if err := workflow(context.Background(), state); err != nil {
		log.Fatal(err)
	}
	fmt.Println("done — cleanups ran in LIFO order (transaction before connection)")
}
