// Copyright 2026 ICAP Mock

package config

import (
	"testing"
)

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		// Plain numbers
		{"0", 0, false},
		{"1024", 1024, false},
		{"104857600", 104857600, false},

		// KB
		{"100KB", 102400, false},
		{"100kb", 102400, false},
		{"100Kb", 102400, false},
		{"1KB", 1024, false},

		// MB
		{"100MB", 104857600, false},
		{"100mb", 104857600, false},
		{"10MB", 10485760, false},
		{"1MB", 1048576, false},

		// GB
		{"1GB", 1073741824, false},
		{"1gb", 1073741824, false},
		{"2GB", 2147483648, false},

		// Whitespace
		{" 100MB ", 104857600, false},
		{" 1024 ", 1024, false},

		// Errors
		{"", 0, true},
		{"abc", 0, true},
		{"MB", 0, true},
		{"100XB", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseByteSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseByteSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseByteSize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
