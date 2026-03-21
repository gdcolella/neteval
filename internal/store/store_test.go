package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"neteval/internal/protocol"
)

func tempDB(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNewStore(t *testing.T) {
	s := tempDB(t)
	if s == nil {
		t.Fatal("store is nil")
	}
}

func TestNewStoreBadPath(t *testing.T) {
	_, err := New("/nonexistent/path/db.sqlite")
	if err == nil {
		t.Error("expected error for bad path")
	}
}

func TestSaveAndGetResults(t *testing.T) {
	s := tempDB(t)

	ts := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	result := protocol.TestResult{
		SourceID:   "agent-1",
		SourceName: "host-a",
		TargetID:   "agent-2",
		TargetName: "host-b",
		TestType:   "mesh",
		Direction:  "upload",
		BitsPerSec: 1_000_000_000,
		DurationMs: 10000,
		Timestamp:  ts,
	}

	err := s.SaveResult("run-001", result)
	if err != nil {
		t.Fatalf("SaveResult: %v", err)
	}

	results, err := s.GetRunResults("run-001")
	if err != nil {
		t.Fatalf("GetRunResults: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	got := results[0]
	if got.SourceID != "agent-1" {
		t.Errorf("SourceID = %q, want %q", got.SourceID, "agent-1")
	}
	if got.BitsPerSec != 1_000_000_000 {
		t.Errorf("BitsPerSec = %f, want 1e9", got.BitsPerSec)
	}
	if got.Direction != "upload" {
		t.Errorf("Direction = %q, want %q", got.Direction, "upload")
	}
}

func TestSaveBatchResults(t *testing.T) {
	s := tempDB(t)

	results := make([]protocol.TestResult, 50)
	for i := range results {
		results[i] = protocol.TestResult{
			SourceID:   "agent-1",
			TargetID:   "agent-2",
			TestType:   "mesh",
			Direction:  "upload",
			BitsPerSec: float64(i * 100_000_000),
			Timestamp:  time.Now(),
		}
	}

	err := s.SaveResults("run-batch", results)
	if err != nil {
		t.Fatalf("SaveResults: %v", err)
	}

	got, err := s.GetRunResults("run-batch")
	if err != nil {
		t.Fatalf("GetRunResults: %v", err)
	}
	if len(got) != 50 {
		t.Errorf("expected 50 results, got %d", len(got))
	}
}

func TestGetRuns(t *testing.T) {
	s := tempDB(t)

	// Create two runs
	for i := 0; i < 5; i++ {
		s.SaveResult("run-A", protocol.TestResult{
			SourceID: "a1", TestType: "mesh",
			BitsPerSec: float64(100_000_000 * (i + 1)),
			Timestamp: time.Now(),
		})
	}
	for i := 0; i < 3; i++ {
		s.SaveResult("run-B", protocol.TestResult{
			SourceID: "a1", TestType: "internet",
			BitsPerSec: float64(50_000_000 * (i + 1)),
			Timestamp: time.Now(),
		})
	}

	runs, err := s.GetRuns()
	if err != nil {
		t.Fatalf("GetRuns: %v", err)
	}

	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}

	// Check that run summaries have reasonable data
	for _, run := range runs {
		if run.RunID == "" {
			t.Error("RunID is empty")
		}
		if run.ResultCount == 0 {
			t.Error("ResultCount is 0")
		}
		if run.AvgBps <= 0 {
			t.Errorf("AvgBps = %f, want > 0", run.AvgBps)
		}
		if run.MaxBps < run.MinBps {
			t.Errorf("MaxBps (%f) < MinBps (%f)", run.MaxBps, run.MinBps)
		}
	}
}

func TestGetLatestResults(t *testing.T) {
	s := tempDB(t)

	for i := 0; i < 20; i++ {
		s.SaveResult("run-1", protocol.TestResult{
			SourceID: "a1", TestType: "mesh",
			BitsPerSec: float64(i),
			Timestamp: time.Now(),
		})
	}

	results, err := s.GetLatestResults(5)
	if err != nil {
		t.Fatalf("GetLatestResults: %v", err)
	}
	if len(results) != 5 {
		t.Errorf("expected 5 results, got %d", len(results))
	}
}

func TestDeleteRun(t *testing.T) {
	s := tempDB(t)

	s.SaveResult("run-del", protocol.TestResult{
		SourceID: "a1", TestType: "mesh", Timestamp: time.Now(),
	})
	s.SaveResult("run-del", protocol.TestResult{
		SourceID: "a2", TestType: "mesh", Timestamp: time.Now(),
	})
	s.SaveResult("run-keep", protocol.TestResult{
		SourceID: "a1", TestType: "mesh", Timestamp: time.Now(),
	})

	err := s.DeleteRun("run-del")
	if err != nil {
		t.Fatalf("DeleteRun: %v", err)
	}

	// run-del should be gone
	results, _ := s.GetRunResults("run-del")
	if len(results) != 0 {
		t.Errorf("expected 0 results after delete, got %d", len(results))
	}

	// run-keep should remain
	results, _ = s.GetRunResults("run-keep")
	if len(results) != 1 {
		t.Errorf("expected 1 result for run-keep, got %d", len(results))
	}
}

func TestExportJSON(t *testing.T) {
	s := tempDB(t)

	s.SaveResult("run-export", protocol.TestResult{
		SourceID: "a1", TestType: "mesh",
		BitsPerSec: 500_000_000,
		Timestamp: time.Now(),
	})

	data, err := s.ExportJSON()
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	if len(data) == 0 {
		t.Error("ExportJSON returned empty data")
	}

	// Should be valid JSON
	if data[0] != '[' {
		t.Errorf("ExportJSON should return array, got %c...", data[0])
	}
}

func TestResultWithError(t *testing.T) {
	s := tempDB(t)

	s.SaveResult("run-err", protocol.TestResult{
		SourceID:  "a1",
		TestType:  "internet",
		Error:     "connection timed out",
		Timestamp: time.Now(),
	})

	results, _ := s.GetRunResults("run-err")
	if len(results) != 1 {
		t.Fatal("expected 1 result")
	}
	if results[0].Error != "connection timed out" {
		t.Errorf("Error = %q, want %q", results[0].Error, "connection timed out")
	}

	// Error results should NOT appear in run summaries (filtered by WHERE error = '')
	runs, _ := s.GetRuns()
	if len(runs) != 0 {
		t.Errorf("error results should be excluded from run summaries, got %d runs", len(runs))
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := tempDB(t)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			s.SaveResult("run-concurrent", protocol.TestResult{
				SourceID:   "a1",
				TestType:   "mesh",
				BitsPerSec: float64(n * 100_000_000),
				Timestamp:  time.Now(),
			})
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	results, err := s.GetRunResults("run-concurrent")
	if err != nil {
		t.Fatalf("GetRunResults: %v", err)
	}
	if len(results) != 10 {
		t.Errorf("expected 10 results, got %d", len(results))
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")

	// Write data
	s1, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s1.SaveResult("run-persist", protocol.TestResult{
		SourceID: "a1", TestType: "mesh",
		BitsPerSec: 999_999,
		Timestamp: time.Now(),
	})
	s1.Close()

	// Reopen and read
	s2, err := New(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	results, _ := s2.GetRunResults("run-persist")
	if len(results) != 1 {
		t.Fatalf("expected 1 result after reopen, got %d", len(results))
	}
	if results[0].BitsPerSec != 999_999 {
		t.Errorf("BitsPerSec = %f, want 999999", results[0].BitsPerSec)
	}
}

func TestEmptyDatabase(t *testing.T) {
	s := tempDB(t)

	results, err := s.GetLatestResults(10)
	if err != nil {
		t.Fatalf("GetLatestResults: %v", err)
	}
	if results != nil && len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}

	runs, err := s.GetRuns()
	if err != nil {
		t.Fatalf("GetRuns: %v", err)
	}
	if runs != nil && len(runs) != 0 {
		t.Errorf("expected empty runs, got %d", len(runs))
	}
}

func TestDBFileCreated(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "new.db")

	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created")
	}
}
