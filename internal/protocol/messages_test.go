package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMessageTypeConstants(t *testing.T) {
	expected := map[string]string{
		"MsgAgentRegister":   "agent_register",
		"MsgAgentList":       "agent_list",
		"MsgRunMeshTest":     "run_mesh_test",
		"MsgRunInternetTest": "run_internet_test",
		"MsgTestResult":      "test_result",
		"MsgTestProgress":    "test_progress",
		"MsgHeartbeat":       "heartbeat",
		"MsgDeployStatus":    "deploy_status",
		"MsgDashboardUpdate": "dashboard_update",
		"MsgTestsComplete":   "tests_complete",
	}

	actual := map[string]string{
		"MsgAgentRegister":   MsgAgentRegister,
		"MsgAgentList":       MsgAgentList,
		"MsgRunMeshTest":     MsgRunMeshTest,
		"MsgRunInternetTest": MsgRunInternetTest,
		"MsgTestResult":      MsgTestResult,
		"MsgTestProgress":    MsgTestProgress,
		"MsgHeartbeat":       MsgHeartbeat,
		"MsgDeployStatus":    MsgDeployStatus,
		"MsgDashboardUpdate": MsgDashboardUpdate,
		"MsgTestsComplete":   MsgTestsComplete,
	}

	for name, want := range expected {
		if got := actual[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestEnvelopeJSON(t *testing.T) {
	env := Envelope{
		Type:    MsgAgentRegister,
		Payload: AgentInfo{ID: "agent-1", Hostname: "test-host", IP: "10.0.0.1", SpeedPort: 9999, OS: "linux"},
	}

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Envelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Type != MsgAgentRegister {
		t.Errorf("Type = %q, want %q", decoded.Type, MsgAgentRegister)
	}
}

func TestAgentInfoJSON(t *testing.T) {
	info := AgentInfo{
		ID:        "agent-42",
		Hostname:  "workstation",
		IP:        "192.168.1.100",
		SpeedPort: 12345,
		OS:        "windows",
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded AgentInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != "agent-42" {
		t.Errorf("ID = %q, want %q", decoded.ID, "agent-42")
	}
	if decoded.Hostname != "workstation" {
		t.Errorf("Hostname = %q, want %q", decoded.Hostname, "workstation")
	}
	if decoded.SpeedPort != 12345 {
		t.Errorf("SpeedPort = %d, want 12345", decoded.SpeedPort)
	}
}

func TestTestResultJSON(t *testing.T) {
	ts := time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)
	result := TestResult{
		SourceID:   "agent-1",
		SourceName: "host-a",
		TargetID:   "agent-2",
		TargetName: "host-b",
		TestType:   "mesh",
		Direction:  "upload",
		BitsPerSec: 1_000_000_000, // 1 Gbps
		DurationMs: 10000,
		Timestamp:  ts,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded TestResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.BitsPerSec != 1_000_000_000 {
		t.Errorf("BitsPerSec = %f, want 1e9", decoded.BitsPerSec)
	}
	if decoded.TestType != "mesh" {
		t.Errorf("TestType = %q, want %q", decoded.TestType, "mesh")
	}
	if decoded.Direction != "upload" {
		t.Errorf("Direction = %q, want %q", decoded.Direction, "upload")
	}
}

func TestTestResultWithError(t *testing.T) {
	result := TestResult{
		SourceID: "agent-1",
		TestType: "internet",
		Error:    "connection refused",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded TestResult
	json.Unmarshal(data, &decoded)

	if decoded.Error != "connection refused" {
		t.Errorf("Error = %q, want %q", decoded.Error, "connection refused")
	}
	if decoded.BitsPerSec != 0 {
		t.Errorf("BitsPerSec should be 0 on error, got %f", decoded.BitsPerSec)
	}
}

func TestTestProgressJSON(t *testing.T) {
	progress := TestProgressPayload{
		SourceID:   "agent-1",
		TargetID:   "agent-2",
		TestType:   "mesh",
		Direction:  "download",
		Percent:    75.5,
		BitsPerSec: 500_000_000,
	}

	data, err := json.Marshal(progress)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded TestProgressPayload
	json.Unmarshal(data, &decoded)

	if decoded.Percent != 75.5 {
		t.Errorf("Percent = %f, want 75.5", decoded.Percent)
	}
}

func TestDeployStatusPayload(t *testing.T) {
	status := DeployStatusPayload{
		Hostname: "win-server-01",
		IP:       "10.0.0.50",
		Status:   "deploying",
	}

	data, _ := json.Marshal(status)
	var decoded DeployStatusPayload
	json.Unmarshal(data, &decoded)

	if decoded.Hostname != "win-server-01" {
		t.Errorf("Hostname = %q, want %q", decoded.Hostname, "win-server-01")
	}
	if decoded.Status != "deploying" {
		t.Errorf("Status = %q, want %q", decoded.Status, "deploying")
	}
}

func TestDashboardUpdatePayload(t *testing.T) {
	payload := DashboardUpdatePayload{
		Agents: []AgentInfo{
			{ID: "a1", Hostname: "host1"},
			{ID: "a2", Hostname: "host2"},
		},
		Results: []TestResult{
			{SourceID: "a1", TargetID: "a2", BitsPerSec: 100},
		},
	}

	data, _ := json.Marshal(payload)
	var decoded DashboardUpdatePayload
	json.Unmarshal(data, &decoded)

	if len(decoded.Agents) != 2 {
		t.Errorf("Agents count = %d, want 2", len(decoded.Agents))
	}
	if len(decoded.Results) != 1 {
		t.Errorf("Results count = %d, want 1", len(decoded.Results))
	}
}

func TestRunMeshTestPayload(t *testing.T) {
	payload := RunMeshTestPayload{
		TargetID:   "agent-3",
		TargetIP:   "10.0.0.3",
		TargetPort: 9090,
		Direction:  "download",
		DurationMs: 5000,
	}

	data, _ := json.Marshal(payload)
	var decoded RunMeshTestPayload
	json.Unmarshal(data, &decoded)

	if decoded.TargetID != "agent-3" {
		t.Errorf("TargetID = %q, want %q", decoded.TargetID, "agent-3")
	}
	if decoded.DurationMs != 5000 {
		t.Errorf("DurationMs = %d, want 5000", decoded.DurationMs)
	}
}
