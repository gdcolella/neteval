package ad

import (
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Computer represents a discovered machine on the network.
type Computer struct {
	Hostname  string `json:"hostname"`
	IP        string `json:"ip"`
	OS        string `json:"os,omitempty"`
	DN        string `json:"dn,omitempty"`
	Status    string `json:"status"` // "discovered", "deploying", "running", "error"
	Error     string `json:"error,omitempty"`
}

// Credentials holds domain authentication info.
type Credentials struct {
	Domain   string `json:"domain"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// DiscoverComputers finds Windows computers on the Active Directory domain.
// Uses LDAP via PowerShell on Windows, or net lookup-based discovery elsewhere.
func DiscoverComputers(creds Credentials) ([]Computer, error) {
	if runtime.GOOS == "windows" {
		return discoverViaAD(creds)
	}
	return discoverViaNetScan(creds)
}

// discoverViaAD uses PowerShell + AD cmdlets to find computers.
func discoverViaAD(creds Credentials) ([]Computer, error) {
	// Use PowerShell to query AD for computer objects
	script := fmt.Sprintf(`
$secpass = ConvertTo-SecureString '%s' -AsPlainText -Force
$cred = New-Object System.Management.Automation.PSCredential('%s\%s', $secpass)
Get-ADComputer -Filter 'OperatingSystem -like "*Windows*"' -Credential $cred -Server '%s' -Properties DNSHostName,OperatingSystem |
  ForEach-Object { "$($_.DNSHostName)|$($_.OperatingSystem)|$($_.DistinguishedName)" }
`, creds.Password, creds.Domain, creds.Username, creds.Domain)

	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("AD query failed: %w: %s", err, string(out))
	}

	var computers []Computer
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		hostname := parts[0]
		os := ""
		dn := ""
		if len(parts) > 1 {
			os = parts[1]
		}
		if len(parts) > 2 {
			dn = parts[2]
		}

		ip := resolveHost(hostname)
		computers = append(computers, Computer{
			Hostname: hostname,
			IP:       ip,
			OS:       os,
			DN:       dn,
			Status:   "discovered",
		})
	}

	return computers, nil
}

// discoverViaNetScan falls back to ARP/DNS-based discovery on non-Windows.
func discoverViaNetScan(creds Credentials) ([]Computer, error) {
	// Try to use `net` tools or LDAP search via ldapsearch
	// First attempt: ldapsearch if available
	ldapServer := creds.Domain
	baseDN := domainToDN(creds.Domain)
	bindDN := fmt.Sprintf("%s@%s", creds.Username, creds.Domain)

	cmd := exec.Command("ldapsearch",
		"-H", fmt.Sprintf("ldap://%s", ldapServer),
		"-D", bindDN,
		"-w", creds.Password,
		"-b", baseDN,
		"(&(objectClass=computer)(operatingSystem=*Windows*))",
		"dNSHostName", "operatingSystem", "distinguishedName",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fall back to basic network scan
		return discoverViaSubnetScan()
	}

	return parseLdapOutput(string(out)), nil
}

func parseLdapOutput(output string) []Computer {
	var computers []Computer
	var current Computer

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "dn: ") {
			if current.Hostname != "" {
				current.IP = resolveHost(current.Hostname)
				current.Status = "discovered"
				computers = append(computers, current)
			}
			current = Computer{DN: strings.TrimPrefix(line, "dn: ")}
		} else if strings.HasPrefix(line, "dNSHostName: ") {
			current.Hostname = strings.TrimPrefix(line, "dNSHostName: ")
		} else if strings.HasPrefix(line, "operatingSystem: ") {
			current.OS = strings.TrimPrefix(line, "operatingSystem: ")
		}
	}
	if current.Hostname != "" {
		current.IP = resolveHost(current.Hostname)
		current.Status = "discovered"
		computers = append(computers, current)
	}

	return computers
}

// discoverViaSubnetScan does a basic ARP-based discovery of the local subnet.
func discoverViaSubnetScan() ([]Computer, error) {
	// Get local interfaces and scan each subnet
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var computers []Computer
	seen := make(map[string]bool)

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.To4() == nil {
				continue
			}

			// Scan common host range in subnet
			base := ipNet.IP.To4()
			ones, bits := ipNet.Mask.Size()
			if ones == 0 || bits == 0 || ones < 16 {
				continue
			}

			hostBits := uint(bits - ones)
			if hostBits > 8 {
				hostBits = 8 // limit to /24
			}
			maxHosts := (1 << hostBits) - 1

			for i := 1; i < maxHosts && i <= 254; i++ {
				ip := net.IPv4(base[0], base[1], base[2], byte(i))
				ipStr := ip.String()
				if seen[ipStr] || ipStr == ipNet.IP.String() {
					continue
				}
				seen[ipStr] = true

				// Quick TCP connect check on common Windows ports
				if isHostAlive(ipStr) {
					hostname := reverseLookup(ipStr)
					computers = append(computers, Computer{
						Hostname: hostname,
						IP:       ipStr,
						Status:   "discovered",
					})
				}
			}
		}
	}

	return computers, nil
}

func isHostAlive(ip string) bool {
	// Try SMB port (445) which is open on Windows machines
	conn, err := net.DialTimeout("tcp", ip+":445", 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func reverseLookup(ip string) string {
	names, err := net.LookupAddr(ip)
	if err != nil || len(names) == 0 {
		return ip
	}
	return strings.TrimSuffix(names[0], ".")
}

func resolveHost(hostname string) string {
	ips, err := net.LookupHost(hostname)
	if err != nil || len(ips) == 0 {
		return ""
	}
	return ips[0]
}

func domainToDN(domain string) string {
	parts := strings.Split(domain, ".")
	dns := make([]string, len(parts))
	for i, p := range parts {
		dns[i] = "DC=" + p
	}
	return strings.Join(dns, ",")
}
