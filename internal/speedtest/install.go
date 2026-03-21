package speedtest

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// Well-known iperf3 download URLs
	iperf3WinURL = "https://github.com/ar51an/iperf3-win-builds/releases/download/3.20/iperf-3.20-win64.zip"
)

// EnsureIperf3 makes sure iperf3 is available, installing it if needed.
// coordinatorURL can be set to download iperf3 from the coordinator as a last resort.
func EnsureIperf3(coordinatorURL string) error {
	if HasIperf3() {
		return nil
	}

	log.Println("iperf3 not found, attempting to install...")

	switch runtime.GOOS {
	case "linux":
		if tryInstall("apt-get", "install", "-y", "iperf3") == nil {
			return nil
		}
		if tryInstall("yum", "install", "-y", "iperf3") == nil {
			return nil
		}
		if tryInstall("dnf", "install", "-y", "iperf3") == nil {
			return nil
		}

	case "darwin":
		if tryInstall("brew", "install", "iperf3") == nil {
			return nil
		}

	case "windows":
		// Download iperf3 and put it next to our binary
		if err := downloadIperf3Windows(); err == nil {
			return nil
		} else {
			log.Printf("direct download failed: %v", err)
		}
		// Try package managers as fallback
		if tryInstall("winget", "install", "iperf3", "--accept-source-agreements", "--accept-package-agreements") == nil {
			return nil
		}
		if tryInstall("choco", "install", "iperf3", "-y") == nil {
			return nil
		}
	}

	if HasIperf3() {
		return nil
	}
	return fmt.Errorf("could not install iperf3 automatically")
}

// downloadIperf3Windows downloads the iperf3 Windows binary and extracts it
// next to the current executable.
func downloadIperf3Windows() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exePath)
	targetPath := filepath.Join(dir, "iperf3.exe")

	// Already there?
	if _, err := os.Stat(targetPath); err == nil {
		return nil
	}

	log.Printf("downloading iperf3 for Windows...")

	resp, err := http.Get(iperf3WinURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read download: %w", err)
	}

	// It's a zip file — extract iperf3.exe and cygwin DLLs
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}

	for _, f := range zr.File {
		name := filepath.Base(f.Name)
		// Extract iperf3.exe and any required DLLs
		if strings.EqualFold(name, "iperf3.exe") || strings.HasSuffix(strings.ToLower(name), ".dll") {
			outPath := filepath.Join(dir, name)
			rc, err := f.Open()
			if err != nil {
				continue
			}
			out, err := os.Create(outPath)
			if err != nil {
				rc.Close()
				continue
			}
			io.Copy(out, rc)
			out.Close()
			rc.Close()
			log.Printf("extracted %s", name)
		}
	}

	if _, err := os.Stat(targetPath); err == nil {
		log.Println("iperf3 downloaded successfully")
		// Add the directory to PATH for this process so exec.LookPath finds it
		os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		return nil
	}

	return fmt.Errorf("iperf3.exe not found in zip")
}

func tryInstall(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("install via %s failed: %v: %s", name, err, string(out))
		return err
	}
	if HasIperf3() {
		log.Println("iperf3 installed successfully")
		return nil
	}
	return fmt.Errorf("install appeared to succeed but iperf3 still not found")
}
