package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"neteval/internal/protocol"

	_ "github.com/mattn/go-sqlite3"
)

// Store provides persistent storage for test results using SQLite.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// New opens (or creates) a SQLite database at the given path.
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS test_results (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id      TEXT NOT NULL,
			source_id   TEXT NOT NULL,
			source_name TEXT NOT NULL DEFAULT '',
			target_id   TEXT NOT NULL DEFAULT '',
			target_name TEXT NOT NULL DEFAULT '',
			test_type   TEXT NOT NULL,
			direction   TEXT NOT NULL DEFAULT '',
			bits_per_sec REAL NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			timestamp   TEXT NOT NULL,
			error       TEXT NOT NULL DEFAULT '',
			created_at  TEXT NOT NULL DEFAULT (datetime('now'))
		);

		CREATE INDEX IF NOT EXISTS idx_results_run ON test_results(run_id);
		CREATE INDEX IF NOT EXISTS idx_results_type ON test_results(test_type);
		CREATE INDEX IF NOT EXISTS idx_results_time ON test_results(timestamp);
	`)
	return err
}

// SaveResult persists a single test result.
func (s *Store) SaveResult(runID string, result protocol.TestResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO test_results (run_id, source_id, source_name, target_id, target_name,
			test_type, direction, bits_per_sec, duration_ms, timestamp, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID,
		result.SourceID,
		result.SourceName,
		result.TargetID,
		result.TargetName,
		result.TestType,
		result.Direction,
		result.BitsPerSec,
		result.DurationMs,
		result.Timestamp.Format(time.RFC3339),
		result.Error,
	)
	return err
}

// SaveResults persists a batch of results in a single transaction.
func (s *Store) SaveResults(runID string, results []protocol.TestResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO test_results (run_id, source_id, source_name, target_id, target_name,
			test_type, direction, bits_per_sec, duration_ms, timestamp, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, r := range results {
		_, err := stmt.Exec(
			runID, r.SourceID, r.SourceName, r.TargetID, r.TargetName,
			r.TestType, r.Direction, r.BitsPerSec, r.DurationMs,
			r.Timestamp.Format(time.RFC3339), r.Error,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// RunSummary represents a single test run with aggregate stats.
type RunSummary struct {
	RunID       string    `json:"run_id"`
	TestType    string    `json:"test_type"`
	ResultCount int       `json:"result_count"`
	AvgBps      float64   `json:"avg_bps"`
	MaxBps      float64   `json:"max_bps"`
	MinBps      float64   `json:"min_bps"`
	StartedAt   time.Time `json:"started_at"`
}

// GetRuns returns all test runs with summary stats.
func (s *Store) GetRuns() ([]RunSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT run_id, test_type,
			COUNT(*) as cnt,
			AVG(bits_per_sec) as avg_bps,
			MAX(bits_per_sec) as max_bps,
			MIN(bits_per_sec) as min_bps,
			MIN(timestamp) as started
		FROM test_results
		WHERE error = ''
		GROUP BY run_id, test_type
		ORDER BY started DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []RunSummary
	for rows.Next() {
		var r RunSummary
		var ts string
		err := rows.Scan(&r.RunID, &r.TestType, &r.ResultCount, &r.AvgBps, &r.MaxBps, &r.MinBps, &ts)
		if err != nil {
			return nil, err
		}
		r.StartedAt, _ = time.Parse(time.RFC3339, ts)
		runs = append(runs, r)
	}
	return runs, nil
}

// GetRunResults returns all results for a specific run.
func (s *Store) GetRunResults(runID string) ([]protocol.TestResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT source_id, source_name, target_id, target_name,
			test_type, direction, bits_per_sec, duration_ms, timestamp, error
		FROM test_results
		WHERE run_id = ?
		ORDER BY timestamp`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []protocol.TestResult
	for rows.Next() {
		var r protocol.TestResult
		var ts string
		err := rows.Scan(&r.SourceID, &r.SourceName, &r.TargetID, &r.TargetName,
			&r.TestType, &r.Direction, &r.BitsPerSec, &r.DurationMs, &ts, &r.Error)
		if err != nil {
			return nil, err
		}
		r.Timestamp, _ = time.Parse(time.RFC3339, ts)
		results = append(results, r)
	}
	return results, nil
}

// GetLatestResults returns the most recent N results.
func (s *Store) GetLatestResults(limit int) ([]protocol.TestResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT source_id, source_name, target_id, target_name,
			test_type, direction, bits_per_sec, duration_ms, timestamp, error
		FROM test_results
		ORDER BY id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []protocol.TestResult
	for rows.Next() {
		var r protocol.TestResult
		var ts string
		err := rows.Scan(&r.SourceID, &r.SourceName, &r.TargetID, &r.TargetName,
			&r.TestType, &r.Direction, &r.BitsPerSec, &r.DurationMs, &ts, &r.Error)
		if err != nil {
			return nil, err
		}
		r.Timestamp, _ = time.Parse(time.RFC3339, ts)
		results = append(results, r)
	}
	return results, nil
}

// ExportJSON returns all results as a JSON byte slice.
func (s *Store) ExportJSON() ([]byte, error) {
	results, err := s.GetLatestResults(10000)
	if err != nil {
		return nil, err
	}
	return json.Marshal(results)
}

// DeleteRun removes all results for a specific run.
func (s *Store) DeleteRun(runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM test_results WHERE run_id = ?", runID)
	return err
}
