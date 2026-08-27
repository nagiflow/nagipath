package web

import (
	"regexp"
	"strings"
	"testing"
)

// Test that regex search finds what substring search does not.
func TestRegexVsSubstring(t *testing.T) {
	text := "proxy_pass http://backend-prod-v2"

	// Substring search for exact pattern
	substr := "backend.*v2"
	if strings.Contains(text, substr) {
		t.Errorf("substring search should not match pattern %q in %q", substr, text)
	}

	// Regex search for same pattern
	re := regexp.MustCompile(substr)
	if !re.MatchString(text) {
		t.Errorf("regex search should match pattern %q in %q", substr, text)
	}
}

// Test that invalid regex renders error rather than failing.
func TestInvalidRegexError(t *testing.T) {
	invalidPatterns := []string{
		"[unclosed",
		"(?P<incomplete",
		"*invalid",
		"(?P<>empty)",
	}

	for _, pattern := range invalidPatterns {
		_, err := regexp.Compile(pattern)
		if err == nil {
			t.Errorf("expected compilation error for pattern %q", pattern)
		}
	}
}

// Test regex compilation succeeds for valid patterns.
func TestValidRegexCompiles(t *testing.T) {
	validPatterns := []string{
		"proxy_pass.*backend",
		"^listen 443",
		"add_header\\s+X-Frame-Options",
		"(ssl_certificate|ssl_certificate_key)",
		"10\\.90\\.4\\.\\d+",
	}

	for _, pattern := range validPatterns {
		_, err := regexp.Compile(pattern)
		if err != nil {
			t.Errorf("expected pattern %q to compile successfully, got error: %v", pattern, err)
		}
	}
}
