package api

import (
	"strconv"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/probe"
	"github.com/nagiflow/nagipath/internal/trace"
)

// hopDepths and hopLabels port internal/web/funcs.go's hopLevels/hopLabels:
// hops are a tree, not a chain — two members of one upstream are
// alternatives a balancer picks between, not two sequential steps — so each
// hop's depth from the entry, and a label with a letter suffix when its rank
// holds alternatives (0, then 1-a, 1-b), are what the graph groups on.
func hopDepths(hops []*trace.Hop) []int {
	depth := make([]int, len(hops))
	for i, h := range hops {
		if h.ArrivedFrom >= 0 && h.ArrivedFrom < i {
			depth[i] = depth[h.ArrivedFrom] + 1
		}
	}
	return depth
}

func hopLabels(hops []*trace.Hop, depth []int) map[int]string {
	rank := map[int][]int{}
	for i := range hops {
		rank[depth[i]] = append(rank[depth[i]], i)
	}
	out := make(map[int]string, len(hops))
	for d, at := range rank {
		for j, i := range at {
			out[i] = strconv.Itoa(d)
			if len(at) > 1 {
				out[i] += "-" + suffix(j)
			}
		}
	}
	return out
}

// suffix is a, b, c … then a1, a2 past the alphabet. An upstream with 27
// members on one rank is not a graph anyone reads, but it must not collide.
func suffix(j int) string {
	if j < 26 {
		return string(rune('a' + j))
	}
	return "a" + strconv.Itoa(j-25)
}

// fired ports internal/web/funcs.go's fired: the rules that decided this
// request, with global tuning directives — which apply to every request
// identically — left out.
func fired(rules []trace.HopRule) []trace.HopRule {
	var out []trace.HopRule
	for _, hr := range rules {
		if hr.Scope != "global" {
			out = append(out, hr)
		}
	}
	return out
}

// firedOf ports internal/web/funcs.go's firedOf: the summary line's "11 of
// 31 rules fired" — routing rules that decided the request, out of every
// rule in scope at these hops.
func firedOf(hops []*trace.Hop) (fired, total int) {
	for _, h := range hops {
		f := 0
		for _, hr := range h.Rules {
			if hr.Scope != "global" {
				f++
			}
		}
		fired += f
		total += len(h.Rules) + len(h.Shadowed)
	}
	return fired, total
}

func siteKind(kind string) string {
	switch kind {
	case "haproxy_frontend":
		return "frontend"
	case "haproxy_backend":
		return "backend"
	case "nginx_server":
		return "server block"
	case "apache_vhost":
		return "vhost"
	}
	return kind
}

// routeKind is a route match type in words: `haproxy_default_backend` is a
// database value, "default backend" is what it means.
func routeKind(matchType string) string {
	out := matchType
	if len(out) > 8 && out[:8] == "haproxy_" {
		out = out[8:]
	}
	res := make([]rune, 0, len(out))
	for _, r := range out {
		if r == '_' {
			res = append(res, ' ')
		} else {
			res = append(res, r)
		}
	}
	return string(res)
}

func reasonText(code string) string {
	switch code {
	case "external_hop":
		return "the request leaves the fleet"
	case "no_matching_listener":
		return "nothing is listening there"
	case "no_matching_site":
		return "something listens, but no site claims that hostname"
	case "no_matching_route":
		return "the site has no route for that path and no catch-all"
	case "unresolvable_upstream":
		return "the destination cannot be determined from configuration"
	case "static_content":
		return "the request is served from disk here"
	case "hop_limit":
		return "the hop limit was reached"
	case "loop_detected":
		return "the request loops"
	}
	return code
}

// instLabel is the host a hop runs on, falling back to the instance name
// when the node is unknown.
func instLabel(i *trace.Inst) string {
	if i.NodeName != "" {
		return i.NodeName
	}
	return i.DisplayName
}

func toPBHop(h *trace.Hop, label string, level int) *pb.TraceHopPB {
	out := &pb.TraceHopPB{Ordinal: int32(h.Ordinal), Label: label, IsExternal: h.IsExternal,
		ExternalTarget: h.ExternalTarget, ExternalReason: h.ExternalReason,
		Confidence: h.Confidence, InboundPath: h.InboundPath, EffectivePath: h.EffectivePath,
		ArrivedFrom: int32(h.ArrivedFrom), Level: int32(level)}
	if h.Inst != nil {
		out.InstId, out.InstNodeName, out.InstDisplayName, out.InstVendor =
			h.Inst.ID, h.Inst.NodeName, h.Inst.DisplayName, h.Inst.Vendor
		out.ClusterName = h.Inst.ClusterName
	}
	if h.Listener != nil {
		out.ListenerPort = int32(h.Listener.Port)
	}
	if h.Site != nil {
		out.SiteKind, out.SiteName = siteKind(h.Site.Kind), h.Site.PrimaryName
	}
	if h.Route != nil {
		out.RouteKind, out.RoutePattern = routeKind(h.Route.MatchType), h.Route.Pattern
	}
	for _, hr := range fired(h.Rules) {
		if hr.Rule == nil {
			continue
		}
		out.FiredRules = append(out.FiredRules, &pb.RuleFiredPB{
			RuleId: hr.Rule.ID, Directive: hr.Rule.Directive, Args: hr.Rule.Args, ActionClass: hr.Rule.ActionClass})
	}
	return out
}

// probedHop and probed port internal/web/funcs.go's probedHop/probed: what
// one stored Probe proved about one hop, in the words the live run used —
// matched by ordinal AND instance both, so a verified row is never shown
// against a hop the fleet's configuration has since moved that ordinal to.
func probedHops(hops []*trace.Hop, last *probe.Past) []*pb.ProbedHopPB {
	if last == nil {
		return nil
	}
	var out []*pb.ProbedHopPB
	for _, h := range hops {
		if h.IsExternal || h.Inst == nil {
			continue
		}
		p := &pb.ProbedHopPB{Ordinal: int32(h.Ordinal), Label: instLabel(h.Inst), Before: "inferred", After: "inferred"}
		instance := h.Inst.DisplayName
		sawRow := false
		var logNote string
		for _, e := range last.Evidence {
			if int(e.HopOrdinal) != h.Ordinal || e.Instance != instance {
				continue
			}
			sawRow = true
			if e.Prior != "" {
				p.Before = e.Prior
			}
			switch {
			case e.Grants == "verified":
				p.After = "verified"
				p.Evidence = "access log line carrying the correlation token"
			case e.Grants == "observed_effect" && p.After != "verified":
				p.After = "observed_effect"
				p.Evidence = "response header this hop declares"
			case e.Kind == "access_log_absent":
				logNote = e.Raw
			case e.Kind == "access_log_error":
				logNote = "could not read its access log — " + e.Raw
			}
		}
		if p.Evidence == "" {
			switch {
			case logNote != "":
				p.Evidence = logNote
			case sawRow:
				p.Evidence = "no header this hop declares came back"
			default:
				p.Evidence = "nagipath read nothing from this hop"
			}
		}
		out = append(out, p)
	}
	return out
}
