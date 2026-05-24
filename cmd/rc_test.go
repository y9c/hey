package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReverseComplement(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard uppercase",
			input:    "ATCG",
			expected: "CGAT",
		},
		{
			name:     "standard lowercase",
			input:    "atcg",
			expected: "cgat",
		},
		{
			name:     "mixed case",
			input:    "aTcG",
			expected: "CgAt",
		},
		{
			name:     "ambiguous bases uppercase",
			input:    "MKRYWSBVDHN",
			expected: "NDHBVSWRYMK",
		},
		{
			name:     "ambiguous bases lowercase",
			input:    "mkrywsbvdhn",
			expected: "ndhbvswrymk",
		},
		{
			name:     "unrecognized uppercase character",
			input:    "ATCGX",
			expected: "NCGAT",
		},
		{
			name:     "unrecognized lowercase character",
			input:    "atcgx",
			expected: "ncgat",
		},
		{
			name:     "empty sequence",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := reverseComplement(tt.input, dnaComplements)
			assert.Equal(t, tt.expected, actual)
		})
	}
}
