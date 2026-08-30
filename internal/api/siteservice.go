package api

import (
	"context"
	"net/url"
	"strings"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// siteService implements pb.SiteServiceServer (proto/nagipath/api/v1/sites.proto).
// Ports getSites/getSite (formerly sites.go), now building real
// *pb.SitesListResponse/*pb.SiteDetailResponse messages instead of the
// hand-written PascalCase JSON structs the old handlers used — sites.proto
// already defined these messages and the frontend (ui/src/api/queries/sites.ts)
// already decoded with fromJson against them, so the old handlers were
// actually sending the wrong wire format; this fixes that as a side effect
// of routing through protojson like every other domain.
type siteService struct {
	pb.UnimplementedSiteServiceServer
	s *Server
}

func toPBSiteListRow(row store.SiteListRow) *pb.SiteListRow {
	return &pb.SiteListRow{
		Name: row.Name, AliasCount: int32(row.AliasCount), Aliases: row.Aliases, Nodes: int32(row.Nodes),
		ListenerSummary: row.ListenerSummary, CertSubject: row.CertSubject, Routes: int32(row.Routes),
		Variants: int32(row.Variants), State: row.State, StateReason: row.StateReason,
	}
}

func (c *siteService) ListSites(ctx context.Context, req *pb.ListSitesRequest) (*pb.SitesListResponse, error) {
	s := c.s
	selected := strings.TrimSpace(req.Site)
	rows, variants, err := s.DB.SiteListWithVariants(ctx, selected)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}

	q := strings.TrimSpace(req.Q)
	if q != "" {
		needle := strings.ToLower(q)
		filtered := rows[:0]
		for _, row := range rows {
			if strings.Contains(strings.ToLower(row.Name), needle) ||
				strings.Contains(strings.ToLower(row.ListenerSummary), needle) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}

	stats, err := s.DB.SitesStats(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}

	resp := &pb.SitesListResponse{
		Query: q, Total: int32(len(rows)), Sel: selected,
		Stats: &pb.SiteStats{
			Hostnames: int32(stats.Hostnames), Nodes: int32(stats.Nodes), VariantHosts: int32(stats.VariantHosts),
			TlsTerminated: int32(stats.TLSTerminated), Plaintext: int32(stats.Plaintext),
			ExpiringCerts: int32(stats.ExpiringCerts), ExpiringBindings: int32(stats.ExpiringBindings),
		},
	}
	for _, row := range rows {
		resp.Rows = append(resp.Rows, toPBSiteListRow(row))
	}
	for _, v := range variants {
		pv := &pb.SiteVariant{Key: v.Key, RawText: v.RawText, Nodes: int32(v.Nodes), NodeNames: v.NodeNames}
		for _, rt := range v.Routes {
			pv.Routes = append(pv.Routes, &pb.SiteVariantRoute{Pattern: rt.Pattern, Action: rt.Action, Target: rt.Target, Upstream: rt.Upstream})
		}
		resp.Variants = append(resp.Variants, pv)
	}
	return resp, nil
}

func (c *siteService) GetSite(ctx context.Context, req *pb.GetSiteRequest) (*pb.SiteDetailResponse, error) {
	s := c.s
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, status.Error(codes.NotFound, "No such site.")
	}

	tab := req.Tab
	if tab == "" {
		tab = "overview"
	}
	variant := req.Variant

	overview, err := s.DB.SiteOverviewData(ctx, name, variant)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	if overview.Stats.Nodes == 0 {
		return nil, status.Error(codes.NotFound, "No node in any current snapshot serves this hostname.")
	}

	resp := &pb.SiteDetailResponse{Name: name, Tab: tab, Variant: variant}
	resp.Tabs = []*pb.SiteTabItem{
		{Label: "Overview", Href: "/sites/" + url.PathEscape(name), On: tab == "overview"},
		{Label: "Nodes", Href: "/sites/" + url.PathEscape(name) + "?tab=nodes", Count: int32(overview.Nodes), On: tab == "nodes"},
		// design/'s 7a lists Routes as a tab of its own. It needs the same
		// overview payload the default branch already builds, so no case for it.
		{Label: "Routes", Href: "/sites/" + url.PathEscape(name) + "?tab=routes", Count: int32(overview.Stats.Routes), On: tab == "routes"},
		{Label: "Upstreams", Href: "/sites/" + url.PathEscape(name) + "?tab=upstreams", Count: int32(overview.Stats.Upstreams), On: tab == "upstreams"},
		{Label: "Certificates", Href: "/sites/" + url.PathEscape(name) + "?tab=certificates", On: tab == "certificates"},
	}
	for i := range overview.Variants {
		key := string(rune('A' + i))
		resp.VariantOpts = append(resp.VariantOpts, &pb.SiteVariantOpt{Key: key, Label: "Variant " + key, On: key == overview.VariantKey})
	}

	loadNodes := func() error {
		nodes, err := s.DB.SiteNodesData(ctx, name)
		if err != nil {
			return err
		}
		for _, n := range nodes {
			resp.Nodes = append(resp.Nodes, &pb.SiteNodeRow{
				NodeId: n.NodeID, NodeName: n.NodeName, Cluster: n.Cluster, Variant: n.Variant,
				Listener: n.Listener, Certificate: n.Certificate, LastColl: n.LastColl,
				State: n.State, StateReason: n.StateReason,
			})
		}
		return nil
	}

	switch tab {
	case "nodes":
		if err := loadNodes(); err != nil {
			return nil, status.Error(codes.Unavailable, err.Error())
		}
	case "upstreams":
		upstreams, err := s.DB.SiteUpstreamsData(ctx, name)
		if err != nil {
			return nil, status.Error(codes.Unavailable, err.Error())
		}
		for _, u := range upstreams {
			resp.Upstreams = append(resp.Upstreams, &pb.SiteUpstreamMember{
				Upstream: u.Upstream, Host: u.Host, Port: int32(u.Port), Scheme: u.Scheme,
				Weight: int32(u.Weight), Flags: u.Flags, NodeName: u.NodeName,
			})
		}
	case "certificates":
		certs, err := s.DB.SiteCertificatesData(ctx, name)
		if err != nil {
			return nil, status.Error(codes.Unavailable, err.Error())
		}
		for _, ct := range certs {
			resp.Certs = append(resp.Certs, &pb.SiteCertBinding{
				Subject: ct.Subject, Sans: ct.SANs, Issuer: ct.Issuer, NotAfter: ct.NotAfter,
				ExpiryDays: int32(ct.ExpiryDays), Bindings: int32(ct.Bindings), Uncovered: ct.Uncovered,
			})
		}
	default:
		resp.Overview = toPBSiteOverview(overview)
		if err := loadNodes(); err != nil {
			return nil, status.Error(codes.Unavailable, err.Error())
		}
	}

	return resp, nil
}

func toPBSiteOverview(o store.SiteOverview) *pb.SiteOverview {
	out := &pb.SiteOverview{
		Name: o.Name, Aliases: o.Aliases, Variants: int32(o.Variants), VariantKey: o.VariantKey, Nodes: int32(o.Nodes),
		ListenerSummary: o.ListenerSummary, ListenerFlags: o.ListenerFlags, CertSubject: o.CertSubject,
		CertIssuer: o.CertIssuer, CertExpiry: o.CertExpiry, CertExpiryDays: int32(o.CertExpiryDays),
		CertBindings: int32(o.CertBindings), CertUncovered: o.CertUncovered,
		Stats: &pb.SiteDetailStats{
			Nodes: int32(o.Stats.Nodes), Routes: int32(o.Stats.Routes), Variants: int32(o.Stats.Variants),
			Upstreams: int32(o.Stats.Upstreams), CertDays: int32(o.Stats.CertDays), CertSubject: o.Stats.CertSubject,
		},
	}
	for _, c := range o.Clusters {
		out.Clusters = append(out.Clusters, &pb.ClusterCount{Name: c.Name, Nodes: int32(c.Nodes)})
	}
	for _, u := range o.Upstreams {
		out.Upstreams = append(out.Upstreams, &pb.UpstreamSummary{Name: u.Name, Members: int32(u.Members), Variant: u.Variant})
	}
	for _, rt := range o.Routes {
		out.Routes = append(out.Routes, &pb.SiteDetailRoute{
			Ordinal: int32(rt.Ordinal), Pattern: rt.Pattern, MatchType: rt.MatchType, Action: rt.Action,
			Target: rt.Target, AlsoDoes: rt.AlsoDoes, Variant: rt.Variant,
		})
	}
	return out
}
