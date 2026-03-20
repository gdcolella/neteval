package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"neteval/internal/protocol"
	"neteval/web"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// Coordinator runs the web dashboard and manages agents.
type Coordinator struct {
	Hub          *Hub
	Orchestrator *Orchestrator
	Port         int
}

// New creates a new Coordinator.
func New(port int) *Coordinator {
	hub := NewHub()
	return &Coordinator{
		Hub:          hub,
		Orchestrator: NewOrchestrator(hub),
		Port:         port,
	}
}

// Run starts the coordinator HTTP server.
func (c *Coordinator) Run(ctx context.Context) error {
	mux := http.NewServeMux()

	// Serve embedded web UI
	mux.Handle("/", http.FileServer(http.FS(web.StaticFS)))

	// WebSocket endpoints
	mux.HandleFunc("/ws/agent", c.handleAgentWS)
	mux.HandleFunc("/ws/dashboard", c.handleDashboardWS)

	// REST API
	mux.HandleFunc("/api/agents", c.handleGetAgents)
	mux.HandleFunc("/api/tests/mesh", c.handleRunMeshTest)
	mux.HandleFunc("/api/tests/internet", c.handleRunInternetTest)
	mux.HandleFunc("/api/results", c.handleGetResults)
	mux.HandleFunc("/api/results/clear", c.handleClearResults)

	addr := fmt.Sprintf(":%d", c.Port)
	log.Printf("coordinator listening on http://0.0.0.0%s", addr)

	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	return srv.ListenAndServe()
}

func (c *Coordinator) handleAgentWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Allow connections from any origin
	})
	if err != nil {
		log.Printf("agent ws accept: %v", err)
		return
	}
	c.Hub.HandleAgentWS(r.Context(), conn)
}

func (c *Coordinator) handleDashboardWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("dashboard ws accept: %v", err)
		return
	}
	defer conn.CloseNow()

	c.Hub.AddDashboard(conn)
	defer c.Hub.RemoveDashboard(conn)

	// Send current state
	agents := c.Hub.GetAgents()
	results := c.Hub.GetResults()

	wsjson.Write(r.Context(), conn, protocol.Envelope{
		Type: protocol.MsgDashboardUpdate,
		Payload: protocol.DashboardUpdatePayload{
			Agents:  agents,
			Results: results,
		},
	})

	// Keep connection alive - read messages (the dashboard might send commands)
	for {
		_, _, err := conn.Read(r.Context())
		if err != nil {
			return
		}
	}
}

func (c *Coordinator) handleGetAgents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(c.Hub.GetAgents())
}

func (c *Coordinator) handleRunMeshTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if c.Orchestrator.IsRunning() {
		http.Error(w, "test already running", http.StatusConflict)
		return
	}

	go func() {
		if err := c.Orchestrator.RunMeshTest(context.Background()); err != nil {
			log.Printf("mesh test error: %v", err)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func (c *Coordinator) handleRunInternetTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if c.Orchestrator.IsRunning() {
		http.Error(w, "test already running", http.StatusConflict)
		return
	}

	go func() {
		if err := c.Orchestrator.RunInternetTest(context.Background()); err != nil {
			log.Printf("internet test error: %v", err)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func (c *Coordinator) handleGetResults(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(c.Hub.GetResults())
}

func (c *Coordinator) handleClearResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	c.Hub.ClearResults()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
}
