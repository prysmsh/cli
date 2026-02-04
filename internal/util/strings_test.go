package util

import "testing"

func TestQuoteYAMLString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple string",
			input: "hello",
			want:  `"hello"`,
		},
		{
			name:  "empty string",
			input: "",
			want:  `""`,
		},
		{
			name:  "string with quotes",
			input: `say "hello"`,
			want:  `"say \"hello\""`,
		},
		{
			name:  "string with backslash",
			input: `path\to\file`,
			want:  `"path\\to\\file"`,
		},
		{
			name:  "string with both",
			input: `"quoted\path"`,
			want:  `"\"quoted\\path\""`,
		},
		{
			name:  "string with newline",
			input: "line1\nline2",
			want:  "\"line1\nline2\"",
		},
		{
			name:  "unicode string",
			input: "hello 世界",
			want:  `"hello 世界"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := QuoteYAMLString(tt.input)
			if got != tt.want {
				t.Errorf("QuoteYAMLString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "shorter than max",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "equal to max",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "longer than max",
			input:  "hello world",
			maxLen: 8,
			want:   "hello...",
		},
		{
			name:   "empty string",
			input:  "",
			maxLen: 5,
			want:   "",
		},
		{
			name:   "max length 3",
			input:  "hello",
			maxLen: 3,
			want:   "hel",
		},
		{
			name:   "max length 2",
			input:  "hello",
			maxLen: 2,
			want:   "he",
		},
		{
			name:   "max length 1",
			input:  "hello",
			maxLen: 1,
			want:   "h",
		},
		{
			name:   "max length 0",
			input:  "hello",
			maxLen: 0,
			want:   "",
		},
		{
			name:   "just over threshold for ellipsis",
			input:  "hello",
			maxLen: 4,
			want:   "h...",
		},
		{
			name:   "long ascii string truncation",
			input:  "hello world test string",
			maxLen: 15,
			want:   "hello world ...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("TruncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestSafePathSegment(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid numeric", "123", false},
		{"valid CVE", "CVE-2024-1234", false},
		{"valid with underscore", "cluster_1", false},
		{"empty", "", true},
		{"path traversal", "../../etc/passwd", true},
		{"slash", "a/b", true},
		{"backslash", "a\\b", true},
		{"space", "a b", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SafePathSegment(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafePathSegment(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestTruncateStringLengthConstraint(t *testing.T) {
	input := "this is a long string that should be truncated"
	maxLen := 20

	result := TruncateString(input, maxLen)
	if len(result) > maxLen {
		t.Errorf("TruncateString result length %d exceeds max %d", len(result), maxLen)
	}
}
