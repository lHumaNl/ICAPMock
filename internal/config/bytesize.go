// Package config provides configuration structures and loading mechanisms.
package config

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseByteSize parses a human-readable byte size string into bytes.
// It supports plain numbers (interpreted as bytes), and suffixes KB, MB, GB (case insensitive).
// Uses binary units: 1KB = 1024, 1MB = 1048576, 1GB = 1073741824.
func ParseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty byte size string")
	}

	upper := strings.ToUpper(s)

	type suffix struct {
		label      string
		multiplier int64
	}
	suffixes := []suffix{
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
	}

	for _, sf := range suffixes {
		if strings.HasSuffix(upper, sf.label) {
			numStr := strings.TrimSpace(s[:len(s)-len(sf.label)])
			val, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid byte size %q: %w", s, err)
			}
			return int64(val * float64(sf.multiplier)), nil
		}
	}

	// Plain number
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid byte size %q: %w", s, err)
	}
	return val, nil
}
