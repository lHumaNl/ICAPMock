// Package storage provides request persistence and scenario management
// for the ICAP Mock Server.
package storage

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/icap-mock/icap-mock/pkg/icap"
)

// TestScenarioRegistry_CIDR_Match tests matching scenarios by CIDR ranges.
func TestScenarioRegistry_CIDR_Match(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "internal-network"
    priority: 100
    match:
      cidr_ranges:
        - "192.168.1.0/24"
        - "10.0.0.0/8"
    response:
      icap_status: 200

  - name: "specific-client"
    priority: 90
    match:
      cidr_ranges:
        - "172.16.0.0/16"
    response:
      icap_status: 204

  - name: "default"
    priority: 1
    match: {}
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	tests := []struct {
		name         string
		clientIP     string
		wantScenario string
	}{
		{
			name:         "IP in first CIDR range",
			clientIP:     "192.168.1.100",
			wantScenario: "internal-network",
		},
		{
			name:         "IP in second CIDR range",
			clientIP:     "10.5.10.200",
			wantScenario: "internal-network",
		},
		{
			name:         "IP in specific client range",
			clientIP:     "172.16.5.50",
			wantScenario: "specific-client",
		},
		{
			name:         "IP not in any CIDR range",
			clientIP:     "8.8.8.8",
			wantScenario: "default",
		},
		{
			name:         "IP at start of CIDR range",
			clientIP:     "192.168.1.0",
			wantScenario: "internal-network",
		},
		{
			name:         "IP at end of CIDR range",
			clientIP:     "192.168.1.255",
			wantScenario: "internal-network",
		},
		{
			name:         "IP outside CIDR range",
			clientIP:     "192.168.2.100",
			wantScenario: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &icap.Request{
				Method:   icap.MethodREQMOD,
				URI:      "icap://localhost/scan",
				ClientIP: tt.clientIP,
			}

			scenario, err := registry.Match(req)
			if err != nil {
				t.Fatalf("Match() error = %v", err)
			}
			if scenario.Name != tt.wantScenario {
				t.Errorf("Match() scenario = %v, want %v", scenario.Name, tt.wantScenario)
			}
		})
	}
}

// TestScenarioRegistry_CIDR_Invalid tests loading scenarios with invalid CIDR ranges.
func TestScenarioRegistry_CIDR_Invalid(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "invalid_cidr.yaml")

	tests := []struct {
		name       string
		cidrRange  string
		wantErr    bool
		errField   string
		errMessage string
	}{
		{
			name:       "missing prefix length",
			cidrRange:  "192.168.1.0",
			wantErr:    true,
			errField:   "match.cidr_ranges",
			errMessage: "invalid CIDR address",
		},
		{
			name:       "invalid prefix length",
			cidrRange:  "192.168.1.0/33",
			wantErr:    true,
			errField:   "match.cidr_ranges",
			errMessage: "invalid CIDR address",
		},
		{
			name:       "invalid IP address",
			cidrRange:  "256.168.1.0/24",
			wantErr:    true,
			errField:   "match.cidr_ranges",
			errMessage: "invalid CIDR address",
		},
		{
			name:       "malformed CIDR",
			cidrRange:  "invalid-cidr",
			wantErr:    true,
			errField:   "match.cidr_ranges",
			errMessage: "invalid CIDR address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yamlContent := `
scenarios:
  - name: "invalid-cidr"
    priority: 100
    match:
      cidr_ranges:
        - "` + tt.cidrRange + `"
    response:
      icap_status: 204
`
			if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			registry := NewScenarioRegistry()
			err := registry.Load(scenarioFile)

			if !tt.wantErr {
				if err != nil {
					t.Errorf("Load() unexpected error = %v", err)
				}
				return
			}

			if err == nil {
				t.Error("Load() should return error for invalid CIDR")
				return
			}

			// Verify error structure
			var se *ScenarioError
			if !AsScenarioError(err, &se) {
				t.Errorf("Load() error should be ScenarioError, got %T", err)
				return
			}

			if se.Field != tt.errField {
				t.Errorf("Load() error field = %v, want %v", se.Field, tt.errField)
			}
		})
	}
}

// TestScenarioRegistry_CIDR_MultipleRanges tests matching with multiple CIDR ranges.
func TestScenarioRegistry_CIDR_MultipleRanges(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "multi-cidr"
    priority: 100
    match:
      cidr_ranges:
        - "192.168.1.0/24"
        - "10.0.0.0/8"
        - "172.16.0.0/16"
        - "192.168.100.0/24"
    response:
      icap_status: 200

  - name: "default"
    priority: 1
    match: {}
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	tests := []struct {
		name     string
		clientIP string
		wantBool bool
	}{
		{
			name:     "IP in first range",
			clientIP: "192.168.1.50",
			wantBool: true,
		},
		{
			name:     "IP in second range",
			clientIP: "10.5.10.200",
			wantBool: true,
		},
		{
			name:     "IP in third range",
			clientIP: "172.16.5.50",
			wantBool: true,
		},
		{
			name:     "IP in fourth range",
			clientIP: "192.168.100.200",
			wantBool: true,
		},
		{
			name:     "IP not in any range",
			clientIP: "8.8.8.8",
			wantBool: false,
		},
		{
			name:     "IP close but not in range",
			clientIP: "192.168.2.50",
			wantBool: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &icap.Request{
				Method:   icap.MethodREQMOD,
				URI:      "icap://localhost/scan",
				ClientIP: tt.clientIP,
			}

			scenario, err := registry.Match(req)
			if err != nil {
				t.Fatalf("Match() error = %v", err)
			}

			if tt.wantBool {
				if scenario.Name != "multi-cidr" {
					t.Errorf("Match() scenario = %v, want multi-cidr", scenario.Name)
				}
			} else {
				if scenario.Name != "default" {
					t.Errorf("Match() scenario = %v, want default", scenario.Name)
				}
			}
		})
	}
}

// TestScenarioRegistry_CIDR_EmptyRanges tests with empty CIDR ranges list.
func TestScenarioRegistry_CIDR_EmptyRanges(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "empty-cidr"
    priority: 100
    match:
      cidr_ranges: []
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Empty CIDR list should match any IP
	req := &icap.Request{
		Method:   icap.MethodREQMOD,
		URI:      "icap://localhost/scan",
		ClientIP: "8.8.8.8",
	}

	scenario, err := registry.Match(req)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if scenario.Name != "empty-cidr" {
		t.Errorf("Match() scenario = %v, want empty-cidr", scenario.Name)
	}
}

// TestScenarioRegistry_CIDR_NoCIDRField tests scenarios without CIDR field.
func TestScenarioRegistry_CIDR_NoCIDRField(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "no-cidr"
    priority: 100
    match:
      path_pattern: "^/scan.*"
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Should match regardless of client IP
	req := &icap.Request{
		Method:   icap.MethodREQMOD,
		URI:      "icap://localhost/scan/test",
		ClientIP: "8.8.8.8",
	}

	scenario, err := registry.Match(req)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if scenario.Name != "no-cidr" {
		t.Errorf("Match() scenario = %v, want no-cidr", scenario.Name)
	}
}

// TestScenarioRegistry_CIDR_ClientIPAndCIDR tests using both ClientIP and CIDRRanges.
func TestScenarioRegistry_CIDR_ClientIPAndCIDR(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "exact-ip-match"
    priority: 100
    match:
      client_ip: "192.168.1.100"
    response:
      icap_status: 200

  - name: "cidr-match"
    priority: 90
    match:
      cidr_ranges:
        - "10.0.0.0/8"
    response:
      icap_status: 204

  - name: "default"
    priority: 1
    match: {}
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	tests := []struct {
		name         string
		clientIP     string
		wantScenario string
	}{
		{
			name:         "exact IP match",
			clientIP:     "192.168.1.100",
			wantScenario: "exact-ip-match",
		},
		{
			name:         "CIDR match",
			clientIP:     "10.5.10.200",
			wantScenario: "cidr-match",
		},
		{
			name:         "no match",
			clientIP:     "8.8.8.8",
			wantScenario: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &icap.Request{
				Method:   icap.MethodREQMOD,
				URI:      "icap://localhost/scan",
				ClientIP: tt.clientIP,
			}

			scenario, err := registry.Match(req)
			if err != nil {
				t.Fatalf("Match() error = %v", err)
			}
			if scenario.Name != tt.wantScenario {
				t.Errorf("Match() scenario = %v, want %v", scenario.Name, tt.wantScenario)
			}
		})
	}
}

// TestScenarioRegistry_CIDR_VariousPrefixLengths tests different CIDR prefix lengths.
func TestScenarioRegistry_CIDR_VariousPrefixLengths(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	yamlContent := `
scenarios:
  - name: "various-prefixes"
    priority: 100
    match:
      cidr_ranges:
        - "10.0.0.0/8"
        - "172.16.0.0/12"
        - "192.168.0.0/16"
        - "192.168.100.0/24"
        - "192.168.100.128/25"
    response:
      icap_status: 200

  - name: "default"
    priority: 1
    match: {}
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	tests := []struct {
		name     string
		clientIP string
		wantBool bool
	}{
		{
			name:     "/8 prefix",
			clientIP: "10.255.255.255",
			wantBool: true,
		},
		{
			name:     "/12 prefix",
			clientIP: "172.31.255.255",
			wantBool: true,
		},
		{
			name:     "/16 prefix",
			clientIP: "192.168.255.255",
			wantBool: true,
		},
		{
			name:     "/24 prefix",
			clientIP: "192.168.100.255",
			wantBool: true,
		},
		{
			name:     "/25 prefix first half",
			clientIP: "192.168.100.128",
			wantBool: true,
		},
		{
			name:     "/25 prefix second half",
			clientIP: "192.168.100.200",
			wantBool: true,
		},
		{
			name:     "outside /25 prefix",
			clientIP: "192.168.100.127",
			wantBool: true, // Matches /24 prefix
		},
		{
			name:     "completely outside",
			clientIP: "8.8.8.8",
			wantBool: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &icap.Request{
				Method:   icap.MethodREQMOD,
				URI:      "icap://localhost/scan",
				ClientIP: tt.clientIP,
			}

			scenario, err := registry.Match(req)
			if err != nil {
				t.Fatalf("Match() error = %v", err)
			}

			if tt.wantBool {
				if scenario.Name != "various-prefixes" {
					t.Errorf("Match() scenario = %v, want various-prefixes", scenario.Name)
				}
			} else {
				if scenario.Name != "default" {
					t.Errorf("Match() scenario = %v, want default", scenario.Name)
				}
			}
		})
	}
}

// TestMatchByCIDR tests the matchByCIDR helper function.
func TestMatchByCIDR(t *testing.T) {
	tests := []struct {
		name       string
		cidrRanges []string
		clientIP   string
		want       bool
	}{
		{
			name:       "single CIDR match",
			cidrRanges: []string{"192.168.1.0/24"},
			clientIP:   "192.168.1.100",
			want:       true,
		},
		{
			name:       "single CIDR no match",
			cidrRanges: []string{"192.168.1.0/24"},
			clientIP:   "192.168.2.100",
			want:       false,
		},
		{
			name:       "multiple CIDRs first matches",
			cidrRanges: []string{"192.168.1.0/24", "10.0.0.0/8"},
			clientIP:   "192.168.1.100",
			want:       true,
		},
		{
			name:       "multiple CIDRs second matches",
			cidrRanges: []string{"192.168.1.0/24", "10.0.0.0/8"},
			clientIP:   "10.5.10.200",
			want:       true,
		},
		{
			name:       "multiple CIDRs none match",
			cidrRanges: []string{"192.168.1.0/24", "10.0.0.0/8"},
			clientIP:   "8.8.8.8",
			want:       false,
		},
		{
			name:       "empty CIDR list",
			cidrRanges: []string{},
			clientIP:   "192.168.1.100",
			want:       true,
		},
		{
			name:       "invalid client IP",
			cidrRanges: []string{"192.168.1.0/24"},
			clientIP:   "invalid-ip",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse CIDR ranges
			ipNets := make([]*net.IPNet, len(tt.cidrRanges))
			for i, cidr := range tt.cidrRanges {
				_, ipNet, err := net.ParseCIDR(cidr)
				if err != nil {
					t.Fatalf("ParseCIDR(%q) error = %v", cidr, err)
				}
				ipNets[i] = ipNet
			}

			got := matchByCIDR(ipNets, tt.clientIP)
			if got != tt.want {
				t.Errorf("matchByCIDR() = %v, want %v", got, tt.want)
			}
		})
	}
}

// BenchmarkScenarioRegistry_CIDR_Matching benchmarks CIDR matching performance.
func BenchmarkScenarioRegistry_CIDR_Matching(b *testing.B) {
	tmpDir := b.TempDir()
	scenarioFile := filepath.Join(tmpDir, "scenarios.yaml")

	// Create scenario with 100 CIDR ranges
	cidrList := "cidr_ranges:\n"
	for i := 0; i < 100; i++ {
		cidrList += fmt.Sprintf("        - \"10.%d.0.0/16\"\n", i)
	}

	yamlContent := `
scenarios:
  - name: "many-cidrs"
    priority: 100
    match:
      ` + cidrList + `
    response:
      icap_status: 200
`
	if err := os.WriteFile(scenarioFile, []byte(yamlContent), 0644); err != nil {
		b.Fatalf("WriteFile() error = %v", err)
	}

	registry := NewScenarioRegistry()
	if err := registry.Load(scenarioFile); err != nil {
		b.Fatalf("Load() error = %v", err)
	}

	req := &icap.Request{
		Method:   icap.MethodREQMOD,
		URI:      "icap://localhost/scan",
		ClientIP: "10.50.100.200",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = registry.Match(req)
	}
}
