package coordinator

import (
	"testing"

	"neteval/internal/protocol"
)

func TestNewOrchestrator(t *testing.T) {
	hub := NewHub()
	orch := NewOrchestrator(hub)
	if orch == nil {
		t.Fatal("NewOrchestrator returned nil")
	}
	if orch.IsRunning() {
		t.Error("orchestrator should not be running initially")
	}
}

func TestGenerateRoundsEmpty(t *testing.T) {
	rounds := generateRounds(nil)
	if rounds != nil {
		t.Errorf("expected nil for empty agents, got %d rounds", len(rounds))
	}
}

func TestGenerateRoundsSingleAgent(t *testing.T) {
	rounds := generateRounds([]protocol.AgentInfo{
		{ID: "a1", Hostname: "host1"},
	})
	if rounds != nil {
		t.Errorf("expected nil for 1 agent, got %d rounds", len(rounds))
	}
}

func TestGenerateRoundsTwoAgents(t *testing.T) {
	agents := []protocol.AgentInfo{
		{ID: "a1", Hostname: "host1"},
		{ID: "a2", Hostname: "host2"},
	}
	rounds := generateRounds(agents)

	if len(rounds) != 1 {
		t.Fatalf("expected 1 round for 2 agents, got %d", len(rounds))
	}
	if len(rounds[0]) != 1 {
		t.Fatalf("expected 1 pair in round 0, got %d", len(rounds[0]))
	}

	pair := rounds[0][0]
	ids := map[string]bool{pair[0].ID: true, pair[1].ID: true}
	if !ids["a1"] || !ids["a2"] {
		t.Error("pair should contain a1 and a2")
	}
}

func TestGenerateRoundsThreeAgents(t *testing.T) {
	agents := []protocol.AgentInfo{
		{ID: "a1"}, {ID: "a2"}, {ID: "a3"},
	}
	rounds := generateRounds(agents)

	// 3 agents → 4 with bye → 3 rounds
	if len(rounds) != 3 {
		t.Fatalf("expected 3 rounds for 3 agents, got %d", len(rounds))
	}

	// Each round should have 1 pair (one agent gets a bye)
	for i, round := range rounds {
		if len(round) != 1 {
			t.Errorf("round %d: expected 1 pair, got %d", i, len(round))
		}
	}

	// All pairs should be unique
	seen := make(map[string]bool)
	for _, round := range rounds {
		for _, pair := range round {
			key := pair[0].ID + "-" + pair[1].ID
			if pair[0].ID > pair[1].ID {
				key = pair[1].ID + "-" + pair[0].ID
			}
			if seen[key] {
				t.Errorf("duplicate pair: %s", key)
			}
			seen[key] = true
		}
	}
}

func TestGenerateRoundsFourAgents(t *testing.T) {
	agents := []protocol.AgentInfo{
		{ID: "a1"}, {ID: "a2"}, {ID: "a3"}, {ID: "a4"},
	}
	rounds := generateRounds(agents)

	// 4 agents → 3 rounds, each with 2 non-overlapping pairs
	if len(rounds) != 3 {
		t.Fatalf("expected 3 rounds for 4 agents, got %d", len(rounds))
	}

	for i, round := range rounds {
		if len(round) != 2 {
			t.Errorf("round %d: expected 2 pairs, got %d", i, len(round))
		}

		// No agent should appear in two pairs in the same round
		used := make(map[string]bool)
		for _, pair := range round {
			if used[pair[0].ID] {
				t.Errorf("round %d: agent %s appears twice", i, pair[0].ID)
			}
			if used[pair[1].ID] {
				t.Errorf("round %d: agent %s appears twice", i, pair[1].ID)
			}
			used[pair[0].ID] = true
			used[pair[1].ID] = true
		}
	}

	// All 6 possible pairs should be covered (C(4,2) = 6)
	seen := make(map[string]bool)
	for _, round := range rounds {
		for _, pair := range round {
			key := pair[0].ID + "-" + pair[1].ID
			if pair[0].ID > pair[1].ID {
				key = pair[1].ID + "-" + pair[0].ID
			}
			seen[key] = true
		}
	}
	if len(seen) != 6 {
		t.Errorf("expected 6 unique pairs, got %d", len(seen))
	}
}

func TestGenerateRoundsSixAgents(t *testing.T) {
	agents := make([]protocol.AgentInfo, 6)
	for i := range agents {
		agents[i] = protocol.AgentInfo{ID: "a" + string(rune('1'+i))}
	}
	rounds := generateRounds(agents)

	// 6 agents → 5 rounds, each with 3 pairs
	if len(rounds) != 5 {
		t.Fatalf("expected 5 rounds for 6 agents, got %d", len(rounds))
	}

	for i, round := range rounds {
		if len(round) != 3 {
			t.Errorf("round %d: expected 3 pairs, got %d", i, len(round))
		}
	}

	// Should cover all C(6,2) = 15 pairs
	seen := make(map[string]bool)
	for _, round := range rounds {
		for _, pair := range round {
			key := pair[0].ID + "-" + pair[1].ID
			if pair[0].ID > pair[1].ID {
				key = pair[1].ID + "-" + pair[0].ID
			}
			seen[key] = true
		}
	}
	if len(seen) != 15 {
		t.Errorf("expected 15 unique pairs, got %d", len(seen))
	}
}

func TestGenerateRoundsNoByePairs(t *testing.T) {
	// Odd number → bye element. Verify no pair contains "bye" ID.
	agents := []protocol.AgentInfo{
		{ID: "a1"}, {ID: "a2"}, {ID: "a3"}, {ID: "a4"}, {ID: "a5"},
	}
	rounds := generateRounds(agents)

	for _, round := range rounds {
		for _, pair := range round {
			if pair[0].ID == "bye" || pair[1].ID == "bye" {
				t.Error("pair should not contain bye agent")
			}
		}
	}
}
