package protocol

import "time"

// Message types
const (
	MsgAgentRegister    = "agent_register"
	MsgAgentList        = "agent_list"
	MsgRunMeshTest      = "run_mesh_test"
	MsgRunInternetTest  = "run_internet_test"
	MsgTestResult       = "test_result"
	MsgTestProgress     = "test_progress"
	MsgHeartbeat        = "heartbeat"
	MsgDeployStatus     = "deploy_status"
	MsgDashboardUpdate  = "dashboard_update"
	MsgTestsComplete    = "tests_complete"
	MsgUpdateAgent      = "update_agent"
)

// Envelope wraps all WebSocket messages.
type Envelope struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// AgentInfo describes a connected agent.
type AgentInfo struct {
	ID        string `json:"id"`
	Hostname  string `json:"hostname"`
	IP        string `json:"ip"`
	SpeedPort int    `json:"speed_port"`
	OS        string `json:"os"`
}

// AgentListPayload is sent to dashboard clients.
type AgentListPayload struct {
	Agents []AgentInfo `json:"agents"`
}

// RunMeshTestPayload tells an agent to test against a target.
type RunMeshTestPayload struct {
	TargetID   string `json:"target_id"`
	TargetIP   string `json:"target_ip"`
	TargetPort int    `json:"target_port"`
	Direction  string `json:"direction"` // "upload" or "download"
	DurationMs int64  `json:"duration_ms"`
}

// RunInternetTestPayload tells an agent to test internet speed.
type RunInternetTestPayload struct{}

// TestSettings configurable from the dashboard.
type TestSettings struct {
	DurationSec    int `json:"duration_sec"`     // per-direction test duration (default 10)
	MaxParallel    int `json:"max_parallel"`     // max simultaneous link tests per round (0 = all)
	BufSizeKB      int `json:"buf_size_kb"`      // TCP buffer size in KB (default 128)
	Bidirectional  bool `json:"bidirectional"`   // test both directions (default true)
}

// TestResult is reported by agents after completing a test.
type TestResult struct {
	SourceID    string    `json:"source_id"`
	SourceName  string    `json:"source_name"`
	TargetID    string    `json:"target_id,omitempty"`
	TargetName  string    `json:"target_name,omitempty"`
	TestType    string    `json:"test_type"` // "mesh" or "internet"
	Direction   string    `json:"direction"` // "upload" or "download"
	BitsPerSec  float64   `json:"bits_per_sec"`
	DurationMs  int64     `json:"duration_ms"`
	Timestamp   time.Time `json:"timestamp"`
	Error       string    `json:"error,omitempty"`
}

// TestProgressPayload provides real-time progress info.
type TestProgressPayload struct {
	SourceID   string  `json:"source_id"`
	TargetID   string  `json:"target_id,omitempty"`
	TestType   string  `json:"test_type"`
	Direction  string  `json:"direction"`
	Percent    float64 `json:"percent"`
	BitsPerSec float64 `json:"bits_per_sec"`
}

// DeployStatusPayload reports deployment progress.
type DeployStatusPayload struct {
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	Status   string `json:"status"` // "discovering", "copying", "starting", "connected", "error"
	Error    string `json:"error,omitempty"`
}

// DashboardUpdatePayload pushes state changes to the dashboard.
type DashboardUpdatePayload struct {
	Agents  []AgentInfo  `json:"agents,omitempty"`
	Results []TestResult `json:"results,omitempty"`
}
