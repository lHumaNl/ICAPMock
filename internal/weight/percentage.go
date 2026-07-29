// Copyright 2026 ICAP Mock

// Package weight provides exact fixed-point percentages for weighted responses.
package weight

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// Scale is the number of fixed-point units in one percent.
	Scale = 1000
	// TotalUnits is 100.000 percent represented in fixed-point units.
	TotalUnits = 100 * Scale
)

// Percentage stores a response probability in thousandths of one percent.
type Percentage int

// Parse converts a decimal percentage to fixed-point units. Values must be
// greater than zero and no greater than 100 before rounding to three decimals.
func Parse(value string) (Percentage, error) {
	whole, fraction, err := parseDecimalParts(value)
	if err != nil {
		return 0, err
	}
	if whole > 100 || (whole == 100 && strings.Trim(fraction, "0") != "") {
		return 0, fmt.Errorf("weight must be no greater than 100.000")
	}

	milli := roundedThousandths(fraction)
	units := whole*Scale + milli
	if units == 0 {
		return 0, fmt.Errorf("weight must round to at least 0.001")
	}
	if units > TotalUnits {
		units = TotalUnits
	}
	return Percentage(units), nil
}

func parseDecimalParts(value string) (whole int, fraction string, err error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return 0, "", fmt.Errorf("weight is required")
	}
	if strings.HasPrefix(normalized, "-") {
		return 0, "", fmt.Errorf("weight must be greater than 0.000")
	}
	if strings.HasPrefix(normalized, "+") {
		return 0, "", fmt.Errorf("weight %q is not a decimal number", normalized)
	}
	parts := strings.Split(normalized, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && parts[1] == "") {
		return 0, "", fmt.Errorf("weight %q is not a decimal number", normalized)
	}
	whole, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", fmt.Errorf("weight %q is not a decimal number", normalized)
	}
	if len(parts) == 2 {
		fraction = parts[1]
	}
	for _, digit := range fraction {
		if digit < '0' || digit > '9' {
			return 0, "", fmt.Errorf("weight %q is not a decimal number", normalized)
		}
	}
	return whole, fraction, nil
}

func roundedThousandths(fraction string) int {
	padded := fraction + "0000"
	milli, _ := strconv.Atoi(padded[:3])
	if padded[3] >= '5' {
		milli++
	}
	return milli
}

// MustParse is intended for static programmatic scenario definitions and tests.
func MustParse(value string) Percentage {
	parsed, err := Parse(value)
	if err != nil {
		panic(err)
	}
	return parsed
}

// Units returns thousandths-of-a-percent units.
func (p Percentage) Units() int {
	return int(p)
}

// String returns the canonical three-decimal percentage representation.
func (p Percentage) String() string {
	return fmt.Sprintf("%d.%03d", int(p)/Scale, int(p)%Scale)
}

// UnmarshalYAML parses an exact YAML scalar without float64 conversion.
func (p *Percentage) UnmarshalYAML(node *yaml.Node) error {
	parsed, err := Parse(node.Value)
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}

// MarshalYAML emits a decimal numeric scalar with exactly three decimals.
func (p Percentage) MarshalYAML() (any, error) {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: p.String()}, nil
}

// UnmarshalJSON parses an exact JSON number without float64 conversion.
func (p *Percentage) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return fmt.Errorf("weight is required")
	}
	parsed, err := Parse(string(data))
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}

// MarshalJSON emits the percentage as a JSON number.
func (p Percentage) MarshalJSON() ([]byte, error) {
	return json.Marshal(json.Number(p.String()))
}
