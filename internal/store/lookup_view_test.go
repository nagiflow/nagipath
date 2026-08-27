package store

import "testing"

// Test that plain-language table returns directive verbatim for unknown.
func TestRuleSummaryTextUnknown(t *testing.T) {
	tests := []struct {
		vendor    string
		directive string
		wantKnown bool
		want      string
	}{
		// Known directives
		{"nginx", "proxy_pass", true, "Send to upstream pool or URL"},
		{"nginx", "add_header", true, "Add response header to client"},
		{"nginx", "rewrite", true, "Rewrite request URL"},
		{"haproxy", "use_backend", true, "Route to backend based on condition"},
		{"haproxy", "acl", true, "Define access control condition"},
		{"apache", "ProxyPass", true, "Reverse proxy to backend URL"},
		{"apache", "RewriteRule", true, "Rewrite URL based on pattern"},

		// Unknown directives - should return directive verbatim
		{"nginx", "some_custom_module_directive", false, "some_custom_module_directive"},
		{"haproxy", "unknown_directive", false, "unknown_directive"},
		{"apache", "CustomDirective", false, "CustomDirective"},
		{"unknown_vendor", "any_directive", false, "any_directive"},

		// Case sensitivity check
		{"nginx", "PROXY_PASS", false, "PROXY_PASS"}, // Uppercase not in table
	}

	for _, tt := range tests {
		got := RuleSummaryText(tt.vendor, tt.directive)
		if got != tt.want {
			t.Errorf("RuleSummaryText(%q, %q) = %q, want %q", tt.vendor, tt.directive, got, tt.want)
		}

		// Verify that unknown directives return the directive itself
		if !tt.wantKnown && got != tt.directive {
			t.Errorf("unknown directive should return verbatim: got %q, want %q", got, tt.directive)
		}
	}
}

// Test that RuleSummary table has entries for common directives.
func TestRuleSummaryCompleteness(t *testing.T) {
	commonNginx := []string{"proxy_pass", "rewrite", "return", "add_header", "location", "server_name"}
	commonHAProxy := []string{"use_backend", "acl", "bind", "server", "balance"}
	commonApache := []string{"ProxyPass", "RewriteRule", "Header", "VirtualHost", "Directory"}

	for _, directive := range commonNginx {
		summary := RuleSummaryText("nginx", directive)
		if summary == directive {
			t.Errorf("expected summary for nginx %q, but got directive verbatim", directive)
		}
	}

	for _, directive := range commonHAProxy {
		summary := RuleSummaryText("haproxy", directive)
		if summary == directive {
			t.Errorf("expected summary for haproxy %q, but got directive verbatim", directive)
		}
	}

	for _, directive := range commonApache {
		summary := RuleSummaryText("apache", directive)
		if summary == directive {
			t.Errorf("expected summary for apache %q, but got directive verbatim", directive)
		}
	}
}
