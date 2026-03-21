package ad

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// DeployAgent copies the current binary to the target machine and starts it.
// creds can be domain credentials or per-machine local credentials (workgroup).
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
	userStr := creds.UserString()

	// Use net use to establish connection with credentials
	connectCmd := exec.Command("net", "use", fmt.Sprintf(`\\%s\ADMIN$`, host),
		fmt.Sprintf("/user:%s", userStr), creds.Password)
	if out, err := connectCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("connect to admin share: %w: %s", err, string(out))
	}

	// Copy the binary
	copyCmd := exec.Command("copy", "/Y", binaryPath, remotePath)
	copyCmd.Env = os.Environ()
	if out, err := copyCmd.CombinedOutput(); err != nil {
		copyCmd2 := exec.Command("xcopy", "/Y", binaryPath, remotePath)
		if out2, err2 := copyCmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("copy binary: %w: %s / %s", err, string(out), string(out2))
		}
	}

	// Step 2: Start via PsExec or WMI
	agentArgs := fmt.Sprintf(`C:\Windows\neteval.exe --agent --coordinator-addr=%s`, coordinatorAddr)

	psExec := exec.Command("PsExec.exe",
		fmt.Sprintf(`\\%s`, host),
		"-u", userStr,
		"-p", creds.Password,
		"-d",
		"-accepteula",
		"cmd", "/c", agentArgs)

	if out, err := psExec.CombinedOutput(); err != nil {
		return startViaWMI(host, creds, agentArgs)
	} else {
		_ = out
	}

	return nil
}

func startViaWMI(host string, creds Credentials, command string) error {
	userStr := creds.UserString()

	script := fmt.Sprintf(`
$secpass = ConvertTo-SecureString '%s' -AsPlainText -Force
$cred = New-Object System.Management.Automation.PSCredential('%s', $secpass)
Invoke-WmiMethod -Class Win32_Process -Name Create -ArgumentList '%s' -ComputerName '%s' -Credential $cred
`, creds.Password, userStr, command, host)

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

	windowsBinary := binaryPath
	if !strings.HasSuffix(binaryPath, ".exe") {
		windowsBinary = binaryPath + "-windows-amd64.exe"
		if _, err := os.Stat(windowsBinary); os.IsNotExist(err) {
			return fmt.Errorf("windows binary not found at %s - build with GOOS=windows GOARCH=amd64", windowsBinary)
		}
	}

	// Build smbclient auth string: DOMAIN/user%pass or user%pass for workgroup
	var smbAuth string
	if creds.Domain != "" {
		smbAuth = fmt.Sprintf("%s/%s%%%s", creds.Domain, creds.Username, creds.Password)
	} else {
		smbAuth = fmt.Sprintf("%s%%%s", creds.Username, creds.Password)
	}

	smbCmd := exec.Command("smbclient",
		fmt.Sprintf(`//%s/ADMIN$`, host),
		"-U", smbAuth,
		"-c", fmt.Sprintf(`put "%s" neteval.exe`, windowsBinary))

	if out, err := smbCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("smbclient copy failed: %w: %s", err, string(out))
	}

	agentArgs := fmt.Sprintf(`C:\Windows\neteval.exe --agent --coordinator-addr=%s`, coordinatorAddr)

	// Build winexe auth string
	winexeCmd := exec.Command("winexe",
		"-U", smbAuth,
		fmt.Sprintf("//%s", host),
		fmt.Sprintf("cmd /c start /b %s", agentArgs))

	if out, err := winexeCmd.CombinedOutput(); err != nil {
		return startViaImpacket(host, creds, agentArgs)
	} else {
		_ = out
	}

	return nil
}

func startViaImpacket(host string, creds Credentials, command string) error {
	// impacket format: DOMAIN/user:pass@host or user:pass@host
	var authStr string
	if creds.Domain != "" {
		authStr = fmt.Sprintf("%s/%s:%s@%s", creds.Domain, creds.Username, creds.Password, host)
	} else {
		authStr = fmt.Sprintf("%s:%s@%s", creds.Username, creds.Password, host)
	}

	cmd := exec.Command("wmiexec.py", authStr, command)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("impacket wmiexec failed: %w: %s", err, string(out))
	}
	return nil
}
