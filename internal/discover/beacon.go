package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"
)

const (
	BroadcastPort = 19275 // arbitrary high port for discovery
	BeaconMagic   = "NETEVAL"
)

// Beacon is broadcast by the coordinator so followers can find it.
type Beacon struct {
	Magic string `json:"m"`
	Host  string `json:"h"`
	Port  int    `json:"p"`
}

// BroadcastPresence sends periodic UDP broadcasts so followers can discover this coordinator.
func BroadcastPresence(ctx context.Context, coordPort int) {
	beacon := Beacon{
		Magic: BeaconMagic,
		Host:  getLocalIP(),
		Port:  coordPort,
	}
	data, _ := json.Marshal(beacon)

	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("255.255.255.255:%d", BroadcastPort))
	if err != nil {
		log.Printf("beacon: resolve broadcast addr: %v", err)
		return
	}

	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		log.Printf("beacon: dial: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("broadcasting presence on UDP port %d", BroadcastPort)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Send immediately, then on ticker
	conn.Write(data)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			conn.Write(data)
		}
	}
}

// ListenForCoordinator listens for coordinator beacons and returns the address.
func ListenForCoordinator(ctx context.Context) (string, error) {
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", BroadcastPort))
	if err != nil {
		return "", err
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return "", fmt.Errorf("listen for beacon on port %d: %w", BroadcastPort, err)
	}
	defer conn.Close()

	log.Printf("listening for coordinator beacon on UDP port %d...", BroadcastPort)

	buf := make([]byte, 1024)
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			continue
		}

		var beacon Beacon
		if err := json.Unmarshal(buf[:n], &beacon); err != nil {
			continue
		}
		if beacon.Magic != BeaconMagic {
			continue
		}

		coordAddr := fmt.Sprintf("%s:%d", beacon.Host, beacon.Port)
		log.Printf("found coordinator at %s", coordAddr)
		return coordAddr, nil
	}
}

func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
