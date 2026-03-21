package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"neteval/internal/protocol"
	"neteval/internal/store"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// AgentConn tracks a connected agent.
type AgentConn struct {
	Info protocol.AgentInfo
	Conn *websocket.Conn
	mu   sync.Mutex
}

// Send sends an envelope to this agent.
func (ac *AgentConn) Send(ctx context.Context, env protocol.Envelope) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return wsjson.Write(ctx, ac.Conn, env)
}

// Hub manages agent and dashboard WebSocket connections.
type Hub struct {
	mu         sync.RWMutex
	agents     map[string]*AgentConn
	dashboards map[*websocket.Conn]bool
	dashMu     sync.RWMutex
	results    []protocol.TestResult
	resultMu   sync.RWMutex
	nextID     int
	Store      *store.Store
	runID      string
	resultCh   chan protocol.TestResult // notifies orchestrator of incoming results
}

// NewHub creates a new Hub.
func NewHub() *Hub {
	return &Hub{
		agents:     make(map[string]*AgentConn),
		dashboards: make(map[*websocket.Conn]bool),
		resultCh:   make(chan protocol.TestResult, 100),
	}
}

// RegisterAgent adds an agent to the hub.
func (h *Hub) RegisterAgent(info protocol.AgentInfo, conn *websocket.Conn) string {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nextID++
	info.ID = fmt.Sprintf("agent-%d", h.nextID)

	h.agents[info.ID] = &AgentConn{
		Info: info,
		Conn: conn,
	}

	log.Printf("agent registered: %s (%s) at %s:%d", info.ID, info.Hostname, info.IP, info.SpeedPort)
	go h.broadcastAgentList()
	return info.ID
}

// UnregisterAgent removes an agent from the hub.
func (h *Hub) UnregisterAgent(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.agents, id)
	log.Printf("agent unregistered: %s", id)
	go h.broadcastAgentList()
}

// GetAgent returns an agent by ID.
func (h *Hub) GetAgent(id string) *AgentConn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.agents[id]
}

// GetAgents returns a copy of all agent infos.
func (h *Hub) GetAgents() []protocol.AgentInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	agents := make([]protocol.AgentInfo, 0, len(h.agents))
	for _, ac := range h.agents {
		agents = append(agents, ac.Info)
	}
	return agents
}

// AgentCount returns the number of connected agents.
func (h *Hub) AgentCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.agents)
}

// AddDashboard registers a dashboard WebSocket connection.
func (h *Hub) AddDashboard(conn *websocket.Conn) {
	h.dashMu.Lock()
	defer h.dashMu.Unlock()
	h.dashboards[conn] = true
}

// RemoveDashboard removes a dashboard WebSocket connection.
func (h *Hub) RemoveDashboard(conn *websocket.Conn) {
	h.dashMu.Lock()
	defer h.dashMu.Unlock()
	delete(h.dashboards, conn)
}

// SetRunID sets the current run identifier for result persistence.
func (h *Hub) SetRunID(id string) {
	h.runID = id
}

// AddResult stores a test result and broadcasts to dashboards.
func (h *Hub) AddResult(result protocol.TestResult) {
	h.resultMu.Lock()
	h.results = append(h.results, result)
	h.resultMu.Unlock()

	// Persist to SQLite if available
	if h.Store != nil && h.runID != "" {
		if err := h.Store.SaveResult(h.runID, result); err != nil {
			log.Printf("store: failed to save result: %v", err)
		}
	}

	// Notify orchestrator (non-blocking)
	select {
	case h.resultCh <- result:
	default:
	}

	h.broadcastToDashboards(protocol.Envelope{
		Type:    protocol.MsgTestResult,
		Payload: result,
	})
}

// GetResults returns all test results.
func (h *Hub) GetResults() []protocol.TestResult {
	h.resultMu.RLock()
	defer h.resultMu.RUnlock()
	out := make([]protocol.TestResult, len(h.results))
	copy(out, h.results)
	return out
}

// ClearResults clears all stored results.
func (h *Hub) ClearResults() {
	h.resultMu.Lock()
	h.results = nil
	h.resultMu.Unlock()
}

// broadcastAgentList sends the current agent list to all dashboards.
func (h *Hub) broadcastAgentList() {
	agents := h.GetAgents()
	h.broadcastToDashboards(protocol.Envelope{
		Type:    protocol.MsgAgentList,
		Payload: protocol.AgentListPayload{Agents: agents},
	})
}

// broadcastToDashboards sends a message to all connected dashboards.
func (h *Hub) broadcastToDashboards(env protocol.Envelope) {
	h.dashMu.RLock()
	defer h.dashMu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for conn := range h.dashboards {
		wsjson.Write(ctx, conn, env)
	}
}

// HandleAgentWS manages the lifecycle of an agent WebSocket connection.
func (h *Hub) HandleAgentWS(ctx context.Context, conn *websocket.Conn) {
	conn.SetReadLimit(1 << 20)

	// Read the registration message
	var env protocol.Envelope
	if err := wsjson.Read(ctx, conn, &env); err != nil {
		log.Printf("agent ws read register: %v", err)
		conn.CloseNow()
		return
	}

	if env.Type != protocol.MsgAgentRegister {
		log.Printf("expected agent_register, got %s", env.Type)
		conn.CloseNow()
		return
	}

	data, _ := json.Marshal(env.Payload)
	var info protocol.AgentInfo
	json.Unmarshal(data, &info)

	agentID := h.RegisterAgent(info, conn)
	defer h.UnregisterAgent(agentID)

	// Message loop - forward results to the hub
	for {
		var msg protocol.Envelope
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return
		}

		switch msg.Type {
		case protocol.MsgTestResult:
			data, _ := json.Marshal(msg.Payload)
			var result protocol.TestResult
			json.Unmarshal(data, &result)
			result.SourceID = agentID
			// Look up source hostname
			if ac := h.GetAgent(agentID); ac != nil {
				result.SourceName = ac.Info.Hostname
			}
			// Look up target hostname
			if result.TargetID != "" {
				if ac := h.GetAgent(result.TargetID); ac != nil {
					result.TargetName = ac.Info.Hostname
				}
			}
			h.AddResult(result)

		case protocol.MsgTestProgress:
			// Forward progress to dashboards
			h.broadcastToDashboards(msg)
		}
	}
}

// ResultCh returns the channel that receives test results.
func (h *Hub) ResultCh() <-chan protocol.TestResult {
	return h.resultCh
}

// BroadcastTestsComplete notifies dashboards that all tests are done.
func (h *Hub) BroadcastTestsComplete() {
	h.broadcastToDashboards(protocol.Envelope{
		Type: protocol.MsgTestsComplete,
	})
}
