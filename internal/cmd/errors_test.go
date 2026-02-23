package cmd

import (
	"fmt"
	"testing"
)

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
		{"saturday", "sunday", 3},
		{"foo", "bar", 3},
		{"a", "b", 1},
		{"ab", "ba", 2},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%s", tt.a, tt.b), func(t *testing.T) {
			got := levenshtein(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestLevenshteinSymmetry(t *testing.T) {
	pairs := [][2]string{{"kitten", "sitting"}, {"abc", "def"}, {"hello", "world"}}
	for _, p := range pairs {
		ab := levenshtein(p[0], p[1])
		ba := levenshtein(p[1], p[0])
		if ab != ba {
			t.Errorf("levenshtein(%q,%q)=%d != levenshtein(%q,%q)=%d", p[0], p[1], ab, p[1], p[0], ba)
		}
	}
}

func TestMin(t *testing.T) {
	if min(3, 5) != 3 {
		t.Error("min(3,5) != 3")
	}
	if min(5, 3) != 3 {
		t.Error("min(5,3) != 3")
	}
	if min(4, 4) != 4 {
		t.Error("min(4,4) != 4")
	}
}

func TestIsArgValidationError(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"accepts 1 arg(s), received 0", true},
		{"accepts 2 arg(s), received 3", true},
		{"requires at least 1 arg(s), only received 0", true},
		{"accepts at most 3 arg(s), received 5", true},
		{"accepts between 1 and 3 arg(s), received 5", true},
		{`invalid argument "foo" for "bar"`, true},
		{"something else entirely", false},
		{"", false},
		{`unknown flag: --verbose`, false},
		{`unknown command "foo" for "bar"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			if got := isArgValidationError(tt.msg); got != tt.want {
				t.Errorf("isArgValidationError(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestFriendlyError_ExactArgs_ZeroProvided(t *testing.T) {
	err := friendlyError(fmt.Errorf("accepts 1 arg(s), received 0"))
	got := err.Error()
	want := "this command requires 1 argument(s) — none were provided"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_ExactArgs_TooMany(t *testing.T) {
	err := friendlyError(fmt.Errorf("accepts 1 arg(s), received 3"))
	got := err.Error()
	want := "this command requires exactly 1 argument(s), but 3 were provided"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_MinArgs(t *testing.T) {
	err := friendlyError(fmt.Errorf("requires at least 2 arg(s), only received 1"))
	got := err.Error()
	want := "this command requires at least 2 argument(s), but only 1 were provided"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_MaxArgs(t *testing.T) {
	err := friendlyError(fmt.Errorf("accepts at most 3 arg(s), received 5"))
	got := err.Error()
	want := "this command accepts at most 3 argument(s), but 5 were provided"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_RangeArgs(t *testing.T) {
	err := friendlyError(fmt.Errorf("accepts between 1 and 3 arg(s), received 5"))
	got := err.Error()
	want := "this command requires 1 to 3 argument(s), but 5 were provided"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_InvalidArg(t *testing.T) {
	err := friendlyError(fmt.Errorf(`invalid argument "foo" for "prysm bar"`))
	got := err.Error()
	want := `invalid argument "foo" for "prysm bar"`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_UnknownShortFlag(t *testing.T) {
	err := friendlyError(fmt.Errorf("unknown shorthand flag: 'x' in -x"))
	got := err.Error()
	want := "unknown flag -x"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyError_Passthrough(t *testing.T) {
	original := fmt.Errorf("some other error")
	err := friendlyError(original)
	if err != original {
		t.Errorf("expected passthrough, got %q", err)
	}
}

func TestRegexPatterns(t *testing.T) {
	// Verify regex patterns match expected cobra error messages
	if !reExactArgs.MatchString("accepts 1 arg(s), received 0") {
		t.Error("reExactArgs should match")
	}
	if !reMinArgs.MatchString("requires at least 1 arg(s), only received 0") {
		t.Error("reMinArgs should match")
	}
	if !reMaxArgs.MatchString("accepts at most 3 arg(s), received 5") {
		t.Error("reMaxArgs should match")
	}
	if !reRangeArgs.MatchString("accepts between 1 and 3 arg(s), received 5") {
		t.Error("reRangeArgs should match")
	}
	if !reInvalidArg.MatchString(`invalid argument "foo" for "bar"`) {
		t.Error("reInvalidArg should match")
	}
	if !reUnknownFlag.MatchString("unknown flag: --verbose") {
		t.Error("reUnknownFlag should match")
	}
	if !reUnknownShort.MatchString("unknown shorthand flag: 'x' in -x") {
		t.Error("reUnknownShort should match")
	}
	if !reUnknownCmd.MatchString(`unknown command "foo" for "bar"`) {
		t.Error("reUnknownCmd should match")
	}
}
