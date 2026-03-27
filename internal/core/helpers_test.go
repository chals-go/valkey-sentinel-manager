package core

import "testing"

func TestParseSentinelAddr(t *testing.T) {
	tests := []struct {
		input    string
		wantHost string
		wantPort int
	}{
		{"10.0.0.1:26379", "10.0.0.1", 26379},
		{"sentinel.example.com:6379", "sentinel.example.com", 6379},
		{"10.0.0.1", "10.0.0.1", 26379}, // no port → default
		{"[::1]:26379", "[::1]", 26379},
	}
	for _, tt := range tests {
		host, port := parseSentinelAddr(tt.input)
		if host != tt.wantHost || port != tt.wantPort {
			t.Errorf("parseSentinelAddr(%q) = (%q, %d), want (%q, %d)", tt.input, host, port, tt.wantHost, tt.wantPort)
		}
	}
}

func TestFlatSliceToMap(t *testing.T) {
	m := flatSliceToMap([]string{"name", "mymaster", "ip", "10.0.0.1", "port", "6379"})
	if m["name"] != "mymaster" || m["ip"] != "10.0.0.1" || m["port"] != "6379" {
		t.Fatalf("unexpected map: %v", m)
	}

	// Odd length: last element ignored.
	m = flatSliceToMap([]string{"a", "b", "c"})
	if m["a"] != "b" {
		t.Fatalf("odd slice: %v", m)
	}
	if _, ok := m["c"]; ok {
		t.Fatal("odd element 'c' should not be a key")
	}

	// Empty.
	m = flatSliceToMap(nil)
	if len(m) != 0 {
		t.Fatalf("nil slice: len=%d, want 0", len(m))
	}
}

func TestValidateIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"::1", true},
		{"2001:db8::1", true},
		{"invalid", false},
		{"", false},
		{"999.999.999.999", false},
	}
	for _, tt := range tests {
		if got := validateIP(tt.ip); got != tt.want {
			t.Errorf("validateIP(%q) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

func TestHealthySlaveIPs(t *testing.T) {
	detail := &MasterDetail{
		MasterIP: "10.0.0.1",
		Slaves: []SlaveInfo{
			{IP: "10.0.0.2", Flags: "slave"},
			{IP: "10.0.0.3", Flags: "slave,s_down"},
			{IP: "10.0.0.1", Flags: "slave"}, // same as master
			{IP: "10.0.0.4", Flags: "slave"},
		},
	}
	ips := healthySlaveIPs(detail)
	if len(ips) != 2 {
		t.Fatalf("len = %d, want 2", len(ips))
	}
	for _, ip := range ips {
		if ip == "10.0.0.1" {
			t.Fatal("master IP should be excluded")
		}
		if ip == "10.0.0.3" {
			t.Fatal("s_down slave should be excluded")
		}
	}
}

func TestAllSlaveIPs(t *testing.T) {
	detail := &MasterDetail{
		MasterIP: "10.0.0.1",
		Slaves: []SlaveInfo{
			{IP: "10.0.0.2", Flags: "slave"},
			{IP: "10.0.0.1", Flags: "slave"}, // same as master
			{IP: "10.0.0.3", Flags: "slave,s_down"},
		},
	}
	ips := allSlaveIPs(detail)
	if len(ips) != 2 {
		t.Fatalf("len = %d, want 2", len(ips))
	}
	for _, ip := range ips {
		if ip == "10.0.0.1" {
			t.Fatal("master IP should be excluded")
		}
	}
}
