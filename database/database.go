package database

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

// Connect to and setup the database.
func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	// Set the maximum number of open connections to 1.
	// This helps simplify the logic for handling concurrent requests, while
	// still keeping reasonable (or even improving) performance.
	// Reference: https://stackoverflow.com/a/35805826
	// Initial benchmarks support this claim, so we'll keep it for now.
	db.SetMaxOpenConns(1)

	// Enable WAL mode
	_, err = db.Exec("PRAGMA journal_mode=WAL")
	if err != nil {
		return nil, err
	}

	return db, nil
}
