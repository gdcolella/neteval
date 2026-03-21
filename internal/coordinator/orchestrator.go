package coordinator

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"neteval/internal/protocol"
)

// Orchestrator schedules and runs mesh speed tests.
type Orchestrator struct {
	hub      *Hub
	mu       sync.Mutex
	running  bool
	Settings protocol.TestSettings
}

// NewOrchestrator creates a new Orchestrator.
func NewOrchestrator(hub *Hub) *Orchestrator {
	return &Orchestrator{
		hub: hub,
		Settings: protocol.TestSettings{
			DurationSec:   10,
			MaxParallel:   0,
			BufSizeKB:     128,
			Bidirectional: true,
		},
	}
}

func (o *Orchestrator) durationMs() int64 {
	d := o.Settings.DurationSec
	if d <= 0 {
		d = 10
	}
	return int64(d) * 1000
}

func (o *Orchestrator) testDuration() time.Duration {
	d := o.Settings.DurationSec
	if d <= 0 {
		d = 10
	}
	return time.Duration(d) * time.Second
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

	maxPar := o.Settings.MaxParallel
	for roundNum, pairs := range rounds {
		log.Printf("mesh test round %d/%d: %d pairs (max parallel: %d)", roundNum+1, len(rounds), len(pairs), maxPar)

		// Limit concurrency if MaxParallel is set
		var sem chan struct{}
		if maxPar > 0 {
			sem = make(chan struct{}, maxPar)
		}

		var wg sync.WaitGroup
		for _, pair := range pairs {
			wg.Add(1)
			if sem != nil {
				sem <- struct{}{}
			}
			go func(a, b protocol.AgentInfo) {
				defer wg.Done()
				if sem != nil {
					defer func() { <-sem }()
				}
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
	duration := o.durationMs()

	agentA := o.hub.GetAgent(a.ID)
	agentB := o.hub.GetAgent(b.ID)
	if agentA == nil || agentB == nil {
		return
	}

	// Tell B to prepare its iperf3 server, wait for ready
	port := o.prepareTarget(ctx, agentB, b)

	// Upload: A -> B
	log.Printf("testing %s -> %s (upload)", a.Hostname, b.Hostname)
	agentA.Send(ctx, protocol.Envelope{
		Type: protocol.MsgRunMeshTest,
		Payload: protocol.RunMeshTestPayload{
			TargetID:   b.ID,
			TargetIP:   b.IP,
			TargetPort: port,
			Direction:  "upload",
			DurationMs: duration,
		},
	})

	o.waitForResult(ctx, a.ID, b.ID, "upload")

	if !o.Settings.Bidirectional {
		return
	}

	// Prepare B's server again for the next test
	port = o.prepareTarget(ctx, agentB, b)

	// Download: A -> B (i.e. B sends to A via iperf3 -R)
	log.Printf("testing %s -> %s (download)", a.Hostname, b.Hostname)
	agentA.Send(ctx, protocol.Envelope{
		Type: protocol.MsgRunMeshTest,
		Payload: protocol.RunMeshTestPayload{
			TargetID:   b.ID,
			TargetIP:   b.IP,
			TargetPort: port,
			Direction:  "download",
			DurationMs: duration,
		},
	})

	o.waitForResult(ctx, a.ID, b.ID, "download")
}

// prepareTarget tells the target agent to start its speed test server and waits for ready.
func (o *Orchestrator) prepareTarget(ctx context.Context, ac *AgentConn, info protocol.AgentInfo) int {
	ac.Send(ctx, protocol.Envelope{Type: protocol.MsgPrepareServer})

	timeout := time.After(10 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return info.SpeedPort
		case <-timeout:
			log.Printf("timeout waiting for %s to prepare server, using last known port", info.Hostname)
			return info.SpeedPort
		case ready := <-o.hub.ServerReadyCh():
			if ready.AgentID == ac.Info.ID {
				return ready.Port
			}
			// Not our agent, keep waiting
		}
	}
}

// waitForResult waits for a specific test result or times out.
func (o *Orchestrator) waitForResult(ctx context.Context, sourceID, targetID, direction string) {
	timeout := o.testDuration() + 30*time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			log.Printf("timeout waiting for result %s -> %s (%s)", sourceID, targetID, direction)
			return
		case r := <-o.hub.ResultCh():
			if r.SourceID == sourceID && r.TargetID == targetID && r.Direction == direction {
				return
			}
			// Not our result — it was already stored by AddResult, just keep waiting
		}
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

// RunPairTest tests a single link between two agents (both directions).
func (o *Orchestrator) RunPairTest(ctx context.Context, sourceID, targetID string) error {
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

	srcConn := o.hub.GetAgent(sourceID)
	dstConn := o.hub.GetAgent(targetID)
	if srcConn == nil || dstConn == nil {
		return fmt.Errorf("one or both agents not connected")
	}

	runID := fmt.Sprintf("pair-%d", time.Now().Unix())
	o.hub.SetRunID(runID)
	log.Printf("retesting link %s <-> %s (run: %s)", srcConn.Info.Hostname, dstConn.Info.Hostname, runID)

	o.testPair(ctx, srcConn.Info, dstConn.Info)

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
