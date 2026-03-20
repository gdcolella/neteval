package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"

	"neteval/internal/ad"
	"neteval/internal/config"
	"neteval/internal/protocol"
	"neteval/web"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// Coordinator runs the web dashboard and manages agents.
type Coordinator struct {
	Hub              *Hub
	Orchestrator     *Orchestrator
	Port             int
	TLSCert          string
	TLSKey           string
	AuthToken        string
	discovered       []ad.Computer
	discoveredMu     sync.RWMutex
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

	// AD deployment
	mux.HandleFunc("/api/deploy/discover", c.handleDiscover)
	mux.HandleFunc("/api/deploy/machines", c.handleGetMachines)
	mux.HandleFunc("/api/deploy/start", c.handleDeploy)
	mux.HandleFunc("/api/results/export", c.handleExportResults)

	// Wrap with auth middleware if token is set
	var handler http.Handler = mux
	if c.AuthToken != "" {
		handler = c.authMiddleware(mux)
	}

	scheme := "http"
	if c.TLSCert != "" {
		scheme = "https"
	}
	addr := fmt.Sprintf(":%d", c.Port)
	log.Printf("coordinator listening on %s://0.0.0.0%s", scheme, addr)

	srv := &http.Server{Addr: addr, Handler: handler}

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		log.Println("shutting down coordinator...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	if c.TLSCert != "" && c.TLSKey != "" {
		return srv.ListenAndServeTLS(c.TLSCert, c.TLSKey)
	}
	return srv.ListenAndServe()
}

// authMiddleware checks for a valid auth token on API and WebSocket requests.
func (c *Coordinator) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Static files (the dashboard UI) are always accessible
		if r.URL.Path == "/" || r.URL.Path == "/style.css" || r.URL.Path == "/app.js" {
			next.ServeHTTP(w, r)
			return
		}

		// Check token from query param, header, or cookie
		token := r.URL.Query().Get("token")
		if token == "" {
			token = r.Header.Get("X-Auth-Token")
		}
		if token == "" {
			if cookie, err := r.Cookie("neteval_token"); err == nil {
				token = cookie.Value
			}
		}

		if token != c.AuthToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
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

func (c *Coordinator) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var creds ad.Credentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Broadcast discovery status
	c.Hub.broadcastToDashboards(protocol.Envelope{
		Type:    protocol.MsgDeployStatus,
		Payload: protocol.DeployStatusPayload{Status: "discovering"},
	})

	computers, err := ad.DiscoverComputers(creds)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	c.discoveredMu.Lock()
	c.discovered = computers
	c.discoveredMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(computers)
}

func (c *Coordinator) handleGetMachines(w http.ResponseWriter, r *http.Request) {
	c.discoveredMu.RLock()
	defer c.discoveredMu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(c.discovered)
}

func (c *Coordinator) handleDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Credentials ad.Credentials `json:"credentials"`
		Hostnames   []string       `json:"hostnames"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	coordinatorAddr := fmt.Sprintf("%s:%d", getLocalIP(), c.Port)

	go func() {
		c.discoveredMu.RLock()
		targets := make(map[string]ad.Computer)
		for _, comp := range c.discovered {
			targets[comp.Hostname] = comp
		}
		c.discoveredMu.RUnlock()

		for _, hostname := range req.Hostnames {
			target, ok := targets[hostname]
			if !ok {
				continue
			}

			c.Hub.broadcastToDashboards(protocol.Envelope{
				Type: protocol.MsgDeployStatus,
				Payload: protocol.DeployStatusPayload{
					Hostname: hostname,
					IP:       target.IP,
					Status:   "deploying",
				},
			})

			err := ad.DeployAgent(target, req.Credentials, coordinatorAddr)
			status := "started"
			errMsg := ""
			if err != nil {
				status = "error"
				errMsg = err.Error()
				log.Printf("deploy to %s failed: %v", hostname, err)
			}

			c.Hub.broadcastToDashboards(protocol.Envelope{
				Type: protocol.MsgDeployStatus,
				Payload: protocol.DeployStatusPayload{
					Hostname: hostname,
					IP:       target.IP,
					Status:   status,
					Error:    errMsg,
				},
			})
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deploying"})
}

func (c *Coordinator) handleExportResults(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	results := c.Hub.GetResults()

	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=neteval-results.csv")
		w.Write([]byte("source_id,source_name,target_id,target_name,test_type,direction,bits_per_sec,duration_ms,timestamp,error\n"))
		for _, r := range results {
			fmt.Fprintf(w, "%s,%s,%s,%s,%s,%s,%.0f,%d,%s,%s\n",
				r.SourceID, r.SourceName, r.TargetID, r.TargetName,
				r.TestType, r.Direction, r.BitsPerSec, r.DurationMs,
				r.Timestamp.Format("2006-01-02T15:04:05Z"), r.Error)
		}
	default:
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=neteval-results.json")
		json.NewEncoder(w).Encode(results)
	}
}

func getLocalIP() string {
	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			return ipNet.IP.String()
		}
	}
	return "127.0.0.1"
}
