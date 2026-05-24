package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCountStats(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedLines int
		expectedWords int
		expectedChars int
	}{
		{
			name:          "empty input",
			input:         "",
			expectedLines: 0,
			expectedWords: 0,
			expectedChars: 0,
		},
		{
			name:          "single line no newline",
			input:         "hello world",
			expectedLines: 0,
			expectedWords: 2,
			expectedChars: 11,
		},
		{
			name:          "single line with newline",
			input:         "hello world\n",
			expectedLines: 1,
			expectedWords: 2,
			expectedChars: 12,
		},
		{
			name:          "multiple lines",
			input:         "line one\nline two\nline three",
			expectedLines: 2,
			expectedWords: 6,
			expectedChars: 28,
		},
		{
			name:          "extra whitespace and tabs",
			input:         " \t hello \n\n world \t ",
			expectedLines: 2,
			expectedWords: 2,
			expectedChars: 20,
		},
		{
			name:          "unicode characters (Chinese)",
			input:         "张三\n李四\n五五五\n",
			expectedLines: 3,
			expectedWords: 3,
			expectedChars: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			lines, words, chars, err := countStats(reader)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedLines, lines, "lines")
			assert.Equal(t, tt.expectedWords, words, "words")
			assert.Equal(t, tt.expectedChars, chars, "chars")
		})
	}
}

func TestQuickCountLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "empty input",
			input:    "",
			expected: 0,
		},
		{
			name:     "no newlines",
			input:    "hello world",
			expected: 0,
		},
		{
			name:     "one newline",
			input:    "hello world\n",
			expected: 1,
		},
		{
			name:     "multiple newlines",
			input:    "a\nb\nc\n",
			expected: 3,
		},
		{
			name:     "consecutive newlines",
			input:    "\n\n\n",
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bytes.NewReader([]byte(tt.input))
			actual := quickCountLines(reader)
			assert.Equal(t, tt.expected, actual)
		})
	}
}
