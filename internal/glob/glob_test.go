package glob

import (
	"strings"
	"testing"
	"time"
)

func TestMatch(t *testing.T) {
	testCases := []struct {
		pattern string
		text    string
		want    bool
	}{
		{"*", "", true},
		{"*", "anything", true},
		{"", "", true},
		{"", "a", false},
		{"hello", "hello", true},
		{"hello", "hell", false},
		{"hello", "helloo", false},

		{"h?llo", "hello", true},
		{"h?llo", "hallo", true},
		{"h?llo", "hllo", false},

		{"h*llo", "hllo", true},
		{"h*llo", "heeeello", true},
		{"h*llo", "hello world", false},
		{"user:*", "user:42", true},
		{"user:*", "session:42", false},
		{"*:42", "user:42", true},
		{"a*b*c", "aXXbYYc", true},
		{"a*b*c", "aXXbYY", false},
		{"**a**", "bab", true},
		{"*a/b*", "x/a/b/y", true},

		{"h[ae]llo", "hello", true},
		{"h[ae]llo", "hallo", true},
		{"h[ae]llo", "hillo", false},
		{"h[^e]llo", "hallo", true},
		{"h[^e]llo", "hello", false},
		{"h[a-c]llo", "hbllo", true},
		{"h[a-c]llo", "hdllo", false},
		{"h[c-a]llo", "hbllo", true},
		{"log:[0-9]*", "log:2026", true},
		{"log:[0-9]*", "log:x", false},
		{"[\\]]", "]", true},
		{"[abc", "b", true},
		{"[abc", "d", false},

		{"h\\*llo", "h*llo", true},
		{"h\\*llo", "hello", false},
		{"h\\?llo", "h?llo", true},
		{"h\\?llo", "hallo", false},
		{"end\\", "end\\", true},
	}

	for _, testCase := range testCases {
		if got := Match(testCase.pattern, testCase.text); got != testCase.want {
			t.Errorf("Match(%q, %q) = %v, want %v", testCase.pattern, testCase.text, got, testCase.want)
		}
	}
}

func TestMatchStaysFastOnPathologicalPattern(t *testing.T) {
	pattern := strings.Repeat("a*", 30) + "b"
	text := strings.Repeat("a", 5000)

	startedAt := time.Now()
	if Match(pattern, text) {
		t.Fatal("pattern should not match")
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("matching took %v, want well under a second", elapsed)
	}
}
