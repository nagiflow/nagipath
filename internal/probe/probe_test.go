package probe

import (
	"strings"
	"testing"
)

// Response evidence is only as good as the header name it looked for, and each
// vendor writes that name in a different position. Getting this wrong produces a
// confident "not observed" for a header that was there.
func TestHeaderFromArgs(t *testing.T) {
	cases := []struct{ directive, args, name, value string }{
		{"add_header", "X-Frame-Options DENY", "X-Frame-Options", "DENY"},
		{"add_header", `X-Frame-Options "DENY" always`, "X-Frame-Options", `DENY" always`},
		{"more_set_headers", "X-Route: inner", "X-Route", "inner"},
		{"proxy_set_header", "Host $host", "Host", "$host"},
		{"Header", `always set X-Frame-Options "DENY"`, "X-Frame-Options", "DENY"},
		{"header", `set X-Route inner`, "X-Route", "inner"},
		{"http-response", "set-header X-Foo bar", "X-Foo", "bar"},
		{"http-response", "add-header X-Foo bar", "X-Foo", "bar"},
		{"proxy_pass", "http://app/", "", ""},
		{"add_header", "", "", ""},
	}
	for _, c := range cases {
		name, value := headerFromArgs(c.directive, c.args)
		if name != c.name || value != c.value {
			t.Errorf("headerFromArgs(%q, %q) = (%q, %q), want (%q, %q)",
				c.directive, c.args, name, value, c.name, c.value)
		}
	}
}

// A LogFormat can name the vhost/site that answered and still have nothing that
// contains the Probe's own correlation token — attribution and correlation are
// separate requirements, and the token one is easy to add without noticing the
// other is still missing. Regression for the case that let app01's testlab
// format (%v present, no %{X-Nagipath-Probe}i or %{User-Agent}i) pass silently.
func TestLogFormatGapChecksAttributionAndTokenSeparately(t *testing.T) {
	cases := []struct {
		name       string
		vendor     string
		directives []string
		wantGap    bool
		wantNil    bool
	}{
		{"nginx stock combined has neither", "nginx",
			[]string{`log_format combined '$remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"'`},
			true, false},
		{"nginx with attribution but no token field still gaps", "nginx",
			[]string{`log_format fleet '$remote_addr $host $server_name $upstream_addr $status'`},
			true, false},
		{"nginx with attribution and token field is clean", "nginx",
			[]string{`log_format fleet_json escape=json '{"server_name":"$server_name","upstream_addr":"$upstream_addr","probe":"$http_x_nagipath_probe"}'`},
			false, true},
		{"apache with %v but no token field still gaps", "apache",
			[]string{`LogFormat "%v %a %t \"%r\" %>s %b %D \"%{X-Request-ID}i\" %{UNIQUE_ID}e" fleet`},
			true, false},
		{"apache with %v and User-Agent is clean", "apache",
			[]string{`LogFormat "%v %h %l %u %t \"%r\" %>s %b \"%{User-Agent}i\"" fleet`},
			false, true},
		{"apache missing %v entirely still gaps", "apache",
			[]string{`LogFormat "%h %l %u %t \"%r\" %>s %b \"%{User-Agent}i\"" combined`},
			true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gap := logFormatGap("inst01", c.vendor, c.directives)
			if c.wantNil && gap != nil {
				t.Fatalf("logFormatGap() = %+v, want nil", gap)
			}
			if c.wantGap && gap == nil {
				t.Fatalf("logFormatGap() = nil, want a Gap")
			}
		})
	}
}

// Correlation is by token, never by timestamp: two requests a millisecond apart
// are indistinguishable by time, and a wrong match is worse than no match.
func TestFindTokenMatchesTheTokenNotTheTime(t *testing.T) {
	const token = "9f3c1a7be40d2f8891ab54cc0e17d6b2"
	log := "10.0.0.1 - - [21/Aug/2026:10:00:00 +0000] \"GET / HTTP/1.1\" 200 12 \"-\" \"curl/8\"\n" +
		"10.0.0.2 - - [21/Aug/2026:10:00:00 +0000] \"GET /health HTTP/1.1\" 200 3 \"-\" \"nagipath-probe/dev (+correlation:" + token + ")\"\r\n" +
		"10.0.0.3 - - [21/Aug/2026:10:00:00 +0000] \"GET / HTTP/1.1\" 200 12 \"-\" \"curl/8\"\n"

	got := findToken(log, token)
	if got == "" {
		t.Fatal("the line carrying the token was not found")
	}
	if got[len(got)-1] == '\r' {
		t.Error("the carriage return was kept; the raw line has to be usable as evidence")
	}
	// The whole line is the evidence — a truncated one cannot be re-read later.
	if !strings.Contains(got, "/health") || !strings.Contains(got, token) {
		t.Errorf("evidence line is incomplete: %q", got)
	}

	// A same-second neighbour must never be returned in the token's place.
	if findToken(log, "0000000000000000000000000000dead") != "" {
		t.Error("a token that is not in the log matched a line anyway")
	}
}
