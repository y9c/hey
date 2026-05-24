package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatWithCommas(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected string
	}{
		{
			name:     "single digit positive",
			input:    5.0,
			expected: "5",
		},
		{
			name:     "single digit negative",
			input:    -5.0,
			expected: "-5",
		},
		{
			name:     "hundreds",
			input:    123.0,
			expected: "123",
		},
		{
			name:     "thousands",
			input:    1234.0,
			expected: "1,234",
		},
		{
			name:     "millions",
			input:    1234567.0,
			expected: "1,234,567",
		},
		{
			name:     "with decimal place - short",
			input:    1234.56,
			expected: "1,234.56",
		},
		{
			name:     "with decimal place - long",
			input:    1234567.8912,
			expected: "1,234,567.8912",
		},
		{
			name:     "negative with decimal place",
			input:    -12345.67,
			expected: "-12,345.67",
		},
		{
			name:     "zero",
			input:    0.0,
			expected: "0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := formatWithCommas(tt.input)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestFormatValue(t *testing.T) {
	// Backup global flags
	origScaleToK := scaleToK
	origScaleToM := scaleToM
	defer func() {
		scaleToK = origScaleToK
		scaleToM = origScaleToM
	}()

	tests := []struct {
		name       string
		input      string
		scaleToK   bool
		scaleToM   bool
		expected   string
	}{
		{
			name:       "non-numeric value",
			input:      "not-a-number",
			scaleToK:   false,
			scaleToM:   false,
			expected:   "not-a-number",
		},
		{
			name:       "default formatting (with commas)",
			input:      "1234567.89",
			scaleToK:   false,
			scaleToM:   false,
			expected:   "1,234,567.89",
		},
		{
			name:       "scale to K",
			input:      "12345",
			scaleToK:   true,
			scaleToM:   false,
			expected:   "12.3k",
		},
		{
			name:       "scale to M",
			input:      "12345678",
			scaleToK:   false,
			scaleToM:   true,
			expected:   "12.3M",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scaleToK = tt.scaleToK
			scaleToM = tt.scaleToM
			actual := formatValue(tt.input)
			assert.Equal(t, tt.expected, actual)
		})
	}
}
