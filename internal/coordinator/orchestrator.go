package coordinator

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"neteval/internal/config"
	"neteval/internal/protocol"
)

// Orchestrator schedules and runs mesh speed tests.
type Orchestrator struct {
	hub *Hub
	mu  sync.Mutex
	running bool
}

// NewOrchestrator creates a new Orchestrator.
func NewOrchestrator(hub *Hub) *Orchestrator {
	return &Orchestrator{hub: hub}
}

// IsRunning returns whether a test is currently in progress.
func (o *Orchestrator) IsRunning() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.running
}

// RunMeshTest runs a full mesh speed test across all connected agents.
// Tests are scheduled in rounds using round-robin tournament pairing
// to avoid network saturation.
func (o *Orchestrator) RunMeshTest(ctx context.Context) error {
	o.mu.Lock()
	if o.running {
		o.mu.Unlock()
		return fmt.Errorf("test already running")
	}
	o.running = true
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		o.running = false
		o.mu.Unlock()
	}()

	agents := o.hub.GetAgents()
	n := len(agents)
	if n < 2 {
		return fmt.Errorf("need at least 2 agents, have %d", n)
	}

	runID := fmt.Sprintf("mesh-%d", time.Now().Unix())
	o.hub.SetRunID(runID)
	log.Printf("starting mesh test with %d agents (run: %s)", n, runID)

	// Generate round-robin tournament rounds
	rounds := generateRounds(agents)

	for roundNum, pairs := range rounds {
		log.Printf("mesh test round %d/%d: %d pairs", roundNum+1, len(rounds), len(pairs))

		// Run all pairs in this round concurrently
		var wg sync.WaitGroup
		for _, pair := range pairs {
			wg.Add(1)
			go func(a, b protocol.AgentInfo) {
				defer wg.Done()
				o.testPair(ctx, a, b)
			}(pair[0], pair[1])
		}
		wg.Wait()

		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	log.Printf("mesh test complete")
	o.hub.BroadcastTestsComplete()
	return nil
}

// testPair runs upload and download tests between two agents.
func (o *Orchestrator) testPair(ctx context.Context, a, b protocol.AgentInfo) {
	duration := config.DefaultTestDuration.Milliseconds()

	// A uploads to B (A connects to B's speed server)
	agentA := o.hub.GetAgent(a.ID)
	if agentA == nil {
		return
	}

	log.Printf("testing %s -> %s (upload)", a.Hostname, b.Hostname)
	agentA.Send(ctx, protocol.Envelope{
		Type: protocol.MsgRunMeshTest,
		Payload: protocol.RunMeshTestPayload{
			TargetID:   b.ID,
			TargetIP:   b.IP,
			TargetPort: b.SpeedPort,
			Direction:  "upload",
			DurationMs: duration,
		},
	})

	// Wait for the test to complete plus some buffer
	select {
	case <-ctx.Done():
		return
	case <-time.After(config.DefaultTestDuration + 5*time.Second):
	}

	// A downloads from B
	log.Printf("testing %s -> %s (download)", a.Hostname, b.Hostname)
	agentA.Send(ctx, protocol.Envelope{
		Type: protocol.MsgRunMeshTest,
		Payload: protocol.RunMeshTestPayload{
			TargetID:   b.ID,
			TargetIP:   b.IP,
			TargetPort: b.SpeedPort,
			Direction:  "download",
			DurationMs: duration,
		},
	})

	select {
	case <-ctx.Done():
		return
	case <-time.After(config.DefaultTestDuration + 5*time.Second):
	}
}

// RunInternetTest runs internet speed tests on all connected agents.
func (o *Orchestrator) RunInternetTest(ctx context.Context) error {
	o.mu.Lock()
	if o.running {
		o.mu.Unlock()
		return fmt.Errorf("test already running")
	}
	o.running = true
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		o.running = false
		o.mu.Unlock()
	}()

	agents := o.hub.GetAgents()
	runID := fmt.Sprintf("internet-%d", time.Now().Unix())
	o.hub.SetRunID(runID)
	log.Printf("starting internet test with %d agents (run: %s)", len(agents), runID)

	var wg sync.WaitGroup
	for _, info := range agents {
		ac := o.hub.GetAgent(info.ID)
		if ac == nil {
			continue
		}
		wg.Add(1)
		go func(ac *AgentConn) {
			defer wg.Done()
			ac.Send(ctx, protocol.Envelope{
				Type:    protocol.MsgRunInternetTest,
				Payload: protocol.RunInternetTestPayload{},
			})
		}(ac)
	}
	wg.Wait()

	// Wait for results
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
	}

	o.hub.BroadcastTestsComplete()
	return nil
}

// generateRounds creates round-robin tournament pairings.
// Each round contains non-overlapping pairs so no agent tests twice in one round.
func generateRounds(agents []protocol.AgentInfo) [][][2]protocol.AgentInfo {
	n := len(agents)
	if n < 2 {
		return nil
	}

	// If odd number, add a "bye" (we'll filter it out)
	list := make([]protocol.AgentInfo, len(agents))
	copy(list, agents)
	if n%2 != 0 {
		list = append(list, protocol.AgentInfo{ID: "bye"})
		n++
	}

	numRounds := n - 1
	rounds := make([][][2]protocol.AgentInfo, 0, numRounds)

	for round := 0; round < numRounds; round++ {
		var pairs [][2]protocol.AgentInfo
		for i := 0; i < n/2; i++ {
			a := list[i]
			b := list[n-1-i]
			// Skip bye pairs
			if a.ID == "bye" || b.ID == "bye" {
				continue
			}
			pairs = append(pairs, [2]protocol.AgentInfo{a, b})
		}
		rounds = append(rounds, pairs)

		// Rotate: fix first element, rotate rest
		last := list[n-1]
		copy(list[2:], list[1:n-1])
		list[1] = last
	}

	return rounds
}
