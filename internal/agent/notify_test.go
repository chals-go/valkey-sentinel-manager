package agent

import "testing"

func TestParseSdownDescription(t *testing.T) {
	tests := []struct {
		name        string
		description string
		wantNil     bool
		wantIP      string
		wantPort    int
		wantMaster  string
	}{
		{
			name:        "valid slave sdown",
			description: "slave 10.0.0.3:6379 10.0.0.3 6379 @ mymaster 10.0.0.1 6379",
			wantIP:      "10.0.0.3",
			wantPort:    6379,
			wantMaster:  "mymaster",
		},
		{
			name:        "master sdown (not slave)",
			description: "master mymaster 10.0.0.1 6379",
			wantNil:     true,
		},
		{
			name:        "empty description",
			description: "",
			wantNil:     true,
		},
		{
			name:        "too few parts",
			description: "slave 10.0.0.3:6379",
			wantNil:     true,
		},
		{
			name:        "no @ separator",
			description: "slave 10.0.0.3:6379 10.0.0.3 6379 mymaster",
			wantNil:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSdownDescription(tt.description)
			if tt.wantNil {
				if result != nil {
					t.Fatalf("expected nil, got %v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result["ip"] != tt.wantIP {
				t.Fatalf("ip = %v, want %v", result["ip"], tt.wantIP)
			}
			if result["port"] != tt.wantPort {
				t.Fatalf("port = %v, want %v", result["port"], tt.wantPort)
			}
			if result["master_name"] != tt.wantMaster {
				t.Fatalf("master_name = %v, want %v", result["master_name"], tt.wantMaster)
			}
		})
	}
}
