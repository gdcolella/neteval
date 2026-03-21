package ad

import (
	"strings"
	"testing"
)

func TestDomainToDN(t *testing.T) {
	tests := []struct {
		domain string
		want   string
	}{
		{"example.com", "DC=example,DC=com"},
		{"corp.contoso.com", "DC=corp,DC=contoso,DC=com"},
		{"local", "DC=local"},
	}

	for _, tc := range tests {
		got := domainToDN(tc.domain)
		if got != tc.want {
			t.Errorf("domainToDN(%q) = %q, want %q", tc.domain, got, tc.want)
		}
	}
}

func TestReverseLookupLocalhost(t *testing.T) {
	// This should return something for 127.0.0.1
	result := reverseLookup("127.0.0.1")
	if result == "" {
		t.Error("reverseLookup(127.0.0.1) returned empty string")
	}
}

func TestParseLdapOutput(t *testing.T) {
	output := `dn: CN=WS01,OU=Computers,DC=corp,DC=local
dNSHostName: ws01.corp.local
operatingSystem: Windows 10 Enterprise

dn: CN=SRV01,OU=Servers,DC=corp,DC=local
dNSHostName: srv01.corp.local
operatingSystem: Windows Server 2019

`
	computers := parseLdapOutput(output)

	if len(computers) != 2 {
		t.Fatalf("expected 2 computers, got %d", len(computers))
	}

	if computers[0].Hostname != "ws01.corp.local" {
		t.Errorf("first hostname = %q, want %q", computers[0].Hostname, "ws01.corp.local")
	}
	if computers[0].OS != "Windows 10 Enterprise" {
		t.Errorf("first OS = %q, want %q", computers[0].OS, "Windows 10 Enterprise")
	}
	if !strings.Contains(computers[0].DN, "CN=WS01") {
		t.Errorf("first DN = %q, should contain CN=WS01", computers[0].DN)
	}
	if computers[0].Status != "discovered" {
		t.Errorf("first status = %q, want %q", computers[0].Status, "discovered")
	}

	if computers[1].Hostname != "srv01.corp.local" {
		t.Errorf("second hostname = %q, want %q", computers[1].Hostname, "srv01.corp.local")
	}
}

func TestParseLdapOutputEmpty(t *testing.T) {
	computers := parseLdapOutput("")
	if len(computers) != 0 {
		t.Errorf("expected 0 computers from empty output, got %d", len(computers))
	}
}

func TestParseLdapOutputPartial(t *testing.T) {
	// Only DN, no hostname — should be skipped
	output := `dn: CN=ORPHAN,DC=local
operatingSystem: Windows
`
	computers := parseLdapOutput(output)
	if len(computers) != 0 {
		t.Errorf("expected 0 computers (no hostname), got %d", len(computers))
	}
}

func TestComputerStruct(t *testing.T) {
	c := Computer{
		Hostname: "test-pc",
		IP:       "10.0.0.5",
		OS:       "Windows 11",
		Status:   "discovered",
	}

	if c.Hostname != "test-pc" {
		t.Error("Hostname not set")
	}
	if c.Status != "discovered" {
		t.Error("Status not set")
	}
}

func TestCredentialsStruct(t *testing.T) {
	c := Credentials{
		Domain:   "corp.local",
		Username: "admin",
		Password: "P@ssw0rd",
	}

	if c.Domain != "corp.local" {
		t.Error("Domain not set")
	}
}
