package speedtest

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
)

// EnsureIperf3 makes sure iperf3 is available, installing it if needed.
func EnsureIperf3() error {
	if HasIperf3() {
		return nil
	}

	log.Println("iperf3 not found, attempting to install...")

	switch runtime.GOOS {
	case "linux":
		// Try apt, then yum, then dnf
		if tryInstall("apt-get", "install", "-y", "iperf3") == nil {
			return nil
		}
		if tryInstall("yum", "install", "-y", "iperf3") == nil {
			return nil
		}
		if tryInstall("dnf", "install", "-y", "iperf3") == nil {
			return nil
		}
		return fmt.Errorf("could not install iperf3 — please install manually: apt install iperf3")

	case "darwin":
		if tryInstall("brew", "install", "iperf3") == nil {
			return nil
		}
		return fmt.Errorf("could not install iperf3 — please install manually: brew install iperf3")

	case "windows":
		// On Windows, try winget or choco
		if tryInstall("winget", "install", "iperf3") == nil {
			return nil
		}
		if tryInstall("choco", "install", "iperf3", "-y") == nil {
			return nil
		}
		return fmt.Errorf("could not install iperf3 — please download from https://iperf.fr/iperf-download.php and add to PATH")

	default:
		return fmt.Errorf("please install iperf3 manually for %s", runtime.GOOS)
	}
}

func tryInstall(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("install via %s failed: %v: %s", name, err, string(out))
		return err
	}
	// Verify it worked
	if HasIperf3() {
		log.Println("iperf3 installed successfully")
		return nil
	}
	return fmt.Errorf("install appeared to succeed but iperf3 still not found")
}
