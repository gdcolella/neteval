package config

import (
	"testing"
	"time"
)

func TestDefaultConstants(t *testing.T) {
	if DefaultPort != 8080 {
		t.Errorf("DefaultPort = %d, want 8080", DefaultPort)
	}
	if DefaultSpeedPort != 0 {
		t.Errorf("DefaultSpeedPort = %d, want 0 (random)", DefaultSpeedPort)
	}
	if DefaultTestDuration != 10*time.Second {
		t.Errorf("DefaultTestDuration = %v, want 10s", DefaultTestDuration)
	}
	if DefaultBufSize != 128*1024 {
		t.Errorf("DefaultBufSize = %d, want 131072", DefaultBufSize)
	}
	if HeartbeatInterval != 5*time.Second {
		t.Errorf("HeartbeatInterval = %v, want 5s", HeartbeatInterval)
	}
	if HeartbeatTimeout != 15*time.Second {
		t.Errorf("HeartbeatTimeout = %v, want 15s", HeartbeatTimeout)
	}
	if ReconnectBaseDelay != 1*time.Second {
		t.Errorf("ReconnectBaseDelay = %v, want 1s", ReconnectBaseDelay)
	}
	if ReconnectMaxDelay != 30*time.Second {
		t.Errorf("ReconnectMaxDelay = %v, want 30s", ReconnectMaxDelay)
	}
	if ShutdownTimeout != 10*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 10s", ShutdownTimeout)
	}
}

func TestOptionsStruct(t *testing.T) {
	opts := Options{
		Port:      9090,
		TLSCert:   "/path/cert.pem",
		TLSKey:    "/path/key.pem",
		AuthToken: "secret123",
		AgentMode: true,
		CoordAddr: "10.0.0.1:8080",
	}

	if opts.Port != 9090 {
		t.Error("Options.Port not set")
	}
	if opts.TLSCert != "/path/cert.pem" {
		t.Error("Options.TLSCert not set")
	}
	if !opts.AgentMode {
		t.Error("Options.AgentMode should be true")
	}
	if opts.CoordAddr != "10.0.0.1:8080" {
		t.Error("Options.CoordAddr not set")
	}
}
