package ad

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// DeployAgent copies the current binary to the target machine and starts it.
// It uses SMB to copy the file and PsExec or WMI to start it.
func DeployAgent(target Computer, creds Credentials, coordinatorAddr string) error {
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	if runtime.GOOS == "windows" {
		return deployWindows(binaryPath, target, creds, coordinatorAddr)
	}
	return deployFromLinux(binaryPath, target, creds, coordinatorAddr)
}

func deployWindows(binaryPath string, target Computer, creds Credentials, coordinatorAddr string) error {
	host := target.IP
	if host == "" {
		host = target.Hostname
	}

	// Step 1: Copy binary via SMB admin share
	remotePath := fmt.Sprintf(`\\%s\ADMIN$\neteval.exe`, host)
	credStr := fmt.Sprintf(`%s\%s`, creds.Domain, creds.Username)

	// Use net use to establish connection with credentials
	connectCmd := exec.Command("net", "use", fmt.Sprintf(`\\%s\ADMIN$`, host),
		fmt.Sprintf("/user:%s", credStr), creds.Password)
	if out, err := connectCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("connect to admin share: %w: %s", err, string(out))
	}

	// Copy the binary
	copyCmd := exec.Command("copy", "/Y", binaryPath, remotePath)
	copyCmd.Env = os.Environ()
	if out, err := copyCmd.CombinedOutput(); err != nil {
		// Try xcopy as fallback
		copyCmd2 := exec.Command("xcopy", "/Y", binaryPath, remotePath)
		if out2, err2 := copyCmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("copy binary: %w: %s / %s", err, string(out), string(out2))
		}
	}

	// Step 2: Start via PsExec or WMI
	agentArgs := fmt.Sprintf(`C:\Windows\neteval.exe --agent --coordinator-addr=%s`, coordinatorAddr)

	// Try PsExec first
	psExec := exec.Command("PsExec.exe",
		fmt.Sprintf(`\\%s`, host),
		"-u", credStr,
		"-p", creds.Password,
		"-d", // detach - don't wait for completion
		"-accepteula",
		"cmd", "/c", agentArgs)

	if out, err := psExec.CombinedOutput(); err != nil {
		// Fallback: WMI via PowerShell
		return startViaWMI(host, creds, agentArgs)
	} else {
		_ = out
	}

	return nil
}

func startViaWMI(host string, creds Credentials, command string) error {
	script := fmt.Sprintf(`
$secpass = ConvertTo-SecureString '%s' -AsPlainText -Force
$cred = New-Object System.Management.Automation.PSCredential('%s\%s', $secpass)
Invoke-WmiMethod -Class Win32_Process -Name Create -ArgumentList '%s' -ComputerName '%s' -Credential $cred
`, creds.Password, creds.Domain, creds.Username, command, host)

	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("WMI start failed: %w: %s", err, string(out))
	}
	return nil
}

func deployFromLinux(binaryPath string, target Computer, creds Credentials, coordinatorAddr string) error {
	host := target.IP
	if host == "" {
		host = target.Hostname
	}

	// Cross-compile or use the pre-built Windows binary
	// For Linux deploying to Windows, we need the Windows binary
	windowsBinary := binaryPath
	if !strings.HasSuffix(binaryPath, ".exe") {
		// Look for the windows binary alongside the linux one
		windowsBinary = binaryPath + "-windows-amd64.exe"
		if _, err := os.Stat(windowsBinary); os.IsNotExist(err) {
			return fmt.Errorf("windows binary not found at %s - build with GOOS=windows GOARCH=amd64", windowsBinary)
		}
	}

	// Use smbclient to copy the binary
	smbCmd := exec.Command("smbclient",
		fmt.Sprintf(`//%s/ADMIN$`, host),
		"-U", fmt.Sprintf("%s/%s%%%s", creds.Domain, creds.Username, creds.Password),
		"-c", fmt.Sprintf(`put "%s" neteval.exe`, windowsBinary))

	if out, err := smbCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("smbclient copy failed: %w: %s", err, string(out))
	}

	// Use winexe or impacket's wmiexec to start the agent
	agentArgs := fmt.Sprintf(`C:\Windows\neteval.exe --agent --coordinator-addr=%s`, coordinatorAddr)

	// Try winexe
	winexeCmd := exec.Command("winexe",
		"-U", fmt.Sprintf("%s/%s%%%s", creds.Domain, creds.Username, creds.Password),
		fmt.Sprintf("//%s", host),
		fmt.Sprintf("cmd /c start /b %s", agentArgs))

	if out, err := winexeCmd.CombinedOutput(); err != nil {
		// Try impacket wmiexec
		return startViaImpacket(host, creds, agentArgs)
	} else {
		_ = out
	}

	return nil
}

func startViaImpacket(host string, creds Credentials, command string) error {
	// Use impacket's wmiexec.py
	cmd := exec.Command("wmiexec.py",
		fmt.Sprintf("%s/%s:%s@%s", creds.Domain, creds.Username, creds.Password, host),
		command)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("impacket wmiexec failed: %w: %s", err, string(out))
	}
	return nil
}
