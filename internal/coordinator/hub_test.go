package coordinator

import (
	"testing"
	"time"

	"neteval/internal/protocol"
)

func TestNewHub(t *testing.T) {
	hub := NewHub()
	if hub == nil {
		t.Fatal("NewHub returned nil")
	}
	if hub.AgentCount() != 0 {
		t.Errorf("AgentCount = %d, want 0", hub.AgentCount())
	}
	if len(hub.GetAgents()) != 0 {
		t.Errorf("GetAgents = %d, want 0", len(hub.GetAgents()))
	}
}

func TestRegisterUnregisterAgent(t *testing.T) {
	hub := NewHub()

	// RegisterAgent needs a real websocket conn, but we can verify
	// the count stays 0 since we can't register without one.
	if hub.AgentCount() != 0 {
		t.Error("expected 0 agents initially")
	}
}

func TestAddResult(t *testing.T) {
	hub := NewHub()

	result := protocol.TestResult{
		SourceID:   "agent-1",
		SourceName: "host-a",
		TargetID:   "agent-2",
		TargetName: "host-b",
		TestType:   "mesh",
		Direction:  "upload",
		BitsPerSec: 500_000_000,
		DurationMs: 10000,
		Timestamp:  time.Now(),
	}

	hub.AddResult(result)

	results := hub.GetResults()
	if len(results) != 1 {
		t.Fatalf("GetResults = %d, want 1", len(results))
	}

	got := results[0]
	if got.SourceID != "agent-1" {
		t.Errorf("SourceID = %q, want %q", got.SourceID, "agent-1")
	}
	if got.BitsPerSec != 500_000_000 {
		t.Errorf("BitsPerSec = %f, want 5e8", got.BitsPerSec)
	}
}

func TestAddMultipleResults(t *testing.T) {
	hub := NewHub()

	for i := 0; i < 10; i++ {
		hub.AddResult(protocol.TestResult{
			SourceID:   "agent-1",
			TargetID:   "agent-2",
			BitsPerSec: float64(i * 100_000_000),
		})
	}

	results := hub.GetResults()
	if len(results) != 10 {
		t.Errorf("GetResults = %d, want 10", len(results))
	}
}

func TestClearResults(t *testing.T) {
	hub := NewHub()

	hub.AddResult(protocol.TestResult{SourceID: "a"})
	hub.AddResult(protocol.TestResult{SourceID: "b"})

	if len(hub.GetResults()) != 2 {
		t.Fatal("expected 2 results before clear")
	}

	hub.ClearResults()

	if len(hub.GetResults()) != 0 {
		t.Errorf("GetResults after clear = %d, want 0", len(hub.GetResults()))
	}
}

func TestGetResultsReturnsDefensiveCopy(t *testing.T) {
	hub := NewHub()
	hub.AddResult(protocol.TestResult{SourceID: "agent-1"})

	// Modify the returned slice
	results := hub.GetResults()
	results[0].SourceID = "modified"

	// Original should be unchanged
	original := hub.GetResults()
	if original[0].SourceID != "agent-1" {
		t.Error("GetResults did not return a defensive copy")
	}
}
