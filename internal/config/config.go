package config

import "time"

const (
	DefaultPort         = 8080
	DefaultSpeedPort    = 0 // 0 = pick a random available port
	DefaultTestDuration = 10 * time.Second
	DefaultBufSize      = 128 * 1024 // 128KB for saturating gigabit links
	HeartbeatInterval   = 5 * time.Second
	HeartbeatTimeout    = 15 * time.Second
	ReconnectBaseDelay  = 1 * time.Second
	ReconnectMaxDelay   = 30 * time.Second
)
