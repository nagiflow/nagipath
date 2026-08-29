package api

import (
	"fmt"
	"net"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"

	"github.com/nagiflow/nagipath/internal/trace"
)

// remoteAddr is internal/web/web.go's own copy, ported here for postStartProbe's
// audit trail — see this file's doc comment on parseTarget/targetURL.
func remoteAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// parseTarget and targetURL are internal/web/trace.go's own copies, ported
// here for getRules — Trace itself isn't ported until Phase 6, at which
// point internal/web's copy is deleted and this one becomes the only one.

// parseTarget turns one pasted URL into a trace query. The scheme is optional,
// because an operator copying out of a ticket has a hostname and a path far more
// often than a well-formed URL.
func parseTarget(raw string) (trace.Query, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return trace.Query{}, nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := neturl.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return trace.Query{}, fmt.Errorf("%q is not a URL nagipath can read — try https://host/path", raw)
	}
	q := trace.Query{Scheme: u.Scheme, Hostname: u.Hostname(), Path: u.Path}
	if p, err := strconv.Atoi(u.Port()); err == nil {
		q.Port = p
	}
	return q, nil
}

// targetURL renders a query the way it would be pasted: the port only when it is
// not the scheme's default, because :443 on every https trace is noise.
func targetURL(q trace.Query) string {
	host := q.Hostname
	if q.Port != 0 && !(q.Scheme == "https" && q.Port == 443) && !(q.Scheme == "http" && q.Port == 80) {
		host = fmt.Sprintf("%s:%d", host, q.Port)
	}
	return q.Scheme + "://" + host + q.Path
}
