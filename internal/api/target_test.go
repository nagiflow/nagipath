package api

import "testing"

// TestPastedURLBecomesAQuery ports internal/web's old
// TestPastedURLBecomesAQuery: one pasted URL becomes a trace query, the
// scheme is optional, and the default port is not printed back — :443 on
// every trace is noise.
func TestPastedURLBecomesAQuery(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
	}{
		// A bare host is https on the default port with the root path, and the
		// default port is not printed back — :443 on every trace is noise.
		{"shop.example.com", "https://shop.example.com/"},
		{"shop.example.com/v2/charge", "https://shop.example.com/v2/charge"},
		{"http://shop.example.com:8080/x", "http://shop.example.com:8080/x"},
		{"https://shop.example.com:8443/", "https://shop.example.com:8443/"},
		// A query string is not part of a path the walk can match on.
		{"  https://SHOP.example.com/a?b=1  ", "https://shop.example.com/a"},
		{"", ""},
	} {
		q, err := parseTarget(c.in)
		if err != nil {
			t.Errorf("parseTarget(%q): %v", c.in, err)
			continue
		}
		got := ""
		if q.Hostname != "" {
			got = targetURL(q.Normalise())
		}
		if got != c.want {
			t.Errorf("parseTarget(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if _, err := parseTarget("http://[::1"); err == nil {
		t.Error("an unparseable paste should be an error, not an empty query")
	}
}
