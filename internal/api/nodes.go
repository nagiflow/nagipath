package api

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
)

// Every nodes.go handler moved to nodeservice.go as NodeService's RPCs
// (docs/adr/0018, proto/nagipath/api/v1/nodes.proto). Everything below
// stays here: shared with nodeservice.go.

// listCap bounds how many rows ListNodes returns in one call, same as
// internal/web/nodes.go's listCap.
const listCap = 500

func filterNodeRows(nodes []store.NodeListRow, q string) []store.NodeListRow {
	q = strings.ToLower(q)
	out := make([]store.NodeListRow, 0, len(nodes))
	for _, n := range nodes {
		if strings.Contains(strings.ToLower(n.DisplayName), q) ||
			strings.Contains(strings.ToLower(n.Address), q) {
			out = append(out, n)
		}
	}
	return out
}

// ---------------------------------------------------------------- node detail

// nodeBuild is GetNode's working accumulator — plain store/trace types,
// converted to proto/nagipath/api/v1/nodes.proto's NodeDetailResponse
// (docs/adr/0018) once, at the end, by toProto(). Keeps the tab-loading
// logic below identical to internal/web/nodes.go's.
type nodeBuild struct {
	Node           store.Node
	Credentials    []store.Credential
	Instances      []store.Instance
	Selected       *store.Instance
	HostKeys       []store.HostKey
	PendingKeys    []store.PendingHostKey
	Stats          store.NodeStats
	Running        bool
	Threshold      int
	Tabs           []nodeTab
	Tab            string
	CredentialName string
	BastionName    string

	Inst         *trace.Inst
	Upstreams    []store.UpstreamPool
	SelectedPool *store.UpstreamPool
	PoolMembers  []store.UpstreamMember
	Certificates []nodeCertBindingBuild
	Files        []store.FileRef
	Snapshot     store.Snapshot
	SelectedFile *store.FileRef
	FileBody     string
	ByteStart    int
	LineStart    int

	DriftRuns     []store.DriftRun
	DriftFindings []store.DriftFinding
	DriftByObject map[string]int
	HasDrift      bool
	NoDriftReason string

	SelectedSiteName string
	SelectedRoute    *trace.Route
	RouteEffect      *routeEffectBuild
}

type nodeTab struct {
	Label string
	Href  string
	Count int
	On    bool
}

type nodeCertBindingBuild struct {
	SubjectCN   string
	SiteName    string
	Port        int
	NotAfter    string
	Fingerprint string
	KeyType     string
	ChainLength int
}

type routeEffectBuild struct {
	UpstreamName  string
	MemberCount   int
	BalanceMethod string
	Members       []*trace.Member
}

func toPBNode(n store.Node) *pb.Node {
	return &pb.Node{
		Id: n.ID, Address: n.Address, SshPort: int32(n.SSHPort), DisplayName: n.DisplayName,
		SshUsername: n.SSHUsername.String, CredentialId: n.CredentialID.Int64, OsFamily: n.OSFamily,
		SudoAvailable: n.SudoAvailable, Enabled: n.Enabled, Source: n.Source, Notes: n.Notes,
		ConsecutiveFailures: int32(n.ConsecutiveFailures), FirstSeenAt: n.FirstSeenAt,
		LastCollection: n.LastCollection.String, LastStatus: n.LastStatus.String,
		BastionNodeId: n.BastionNodeID.Int64,
	}
}

func toPBInstance(in store.Instance) *pb.Instance {
	return &pb.Instance{
		Id: in.ID, NodeId: in.NodeID, ClusterId: in.ClusterID.Int64, Vendor: in.Vendor,
		DisplayName: in.DisplayName, Version: in.Version, MainConfigPath: in.MainConfigPath,
		NodeDisplayName: in.NodeDisplayName, SiteCount: int32(in.SiteCount), RouteCount: int32(in.RouteCount),
		CertCount: int32(in.CertCount), LastCaptured: in.LastCaptured.String, ClusterName: in.ClusterName,
		Degraded: in.Degraded, ParseState: in.ParseState, State: in.State(),
		RestartCommand: effectiveRestartCommand(in),
	}
}

func toPBUpstreamMemberRefs(members []*trace.Member) []*pb.UpstreamMemberRef {
	out := make([]*pb.UpstreamMemberRef, 0, len(members))
	for _, m := range members {
		out = append(out, &pb.UpstreamMemberRef{Id: m.ID, Host: m.Host, Port: int32(m.Port), Scheme: m.Scheme, Flags: m.Flags})
	}
	return out
}

func toPBRoute(rt *trace.Route) *pb.Route {
	if rt == nil {
		return nil
	}
	out := &pb.Route{
		Id: rt.ID, MatchType: rt.MatchType, Pattern: rt.Pattern, Ordinal: int32(rt.Ordinal),
		IsTerminal: rt.IsTerminal, UpstreamId: rt.UpstreamID.Int64, TargetRaw: rt.TargetRaw,
	}
	for _, c := range rt.Children {
		out.Children = append(out.Children, toPBRoute(c))
	}
	return out
}

func toPBInst(inst *trace.Inst) *pb.Inst {
	if inst == nil {
		return nil
	}
	out := &pb.Inst{
		Id: inst.ID, NodeName: inst.NodeName, Vendor: inst.Vendor, DisplayName: inst.DisplayName,
		ClusterName: inst.ClusterName, Degraded: inst.Degraded,
	}
	for _, site := range inst.Sites {
		s := &pb.Site{Id: site.ID, PrimaryName: site.PrimaryName, Kind: site.Kind}
		for _, n := range site.Names {
			s.Names = append(s.Names, n.Name)
		}
		for _, rt := range site.Routes {
			s.Routes = append(s.Routes, toPBRoute(rt))
		}
		out.Sites = append(out.Sites, s)
	}
	for _, up := range inst.Upstreams {
		out.Upstreams = append(out.Upstreams, &pb.Upstream{
			Id: up.ID, Name: up.Name, Kind: up.Kind, BalanceMethod: up.BalanceMethod,
			Members: toPBUpstreamMemberRefs(up.Members),
		})
	}
	return out
}

func (nb *nodeBuild) toProto() *pb.NodeDetailResponse {
	resp := &pb.NodeDetailResponse{
		Node: toPBNode(nb.Node), Running: nb.Running, Threshold: int32(nb.Threshold),
		Tab: nb.Tab, CredentialName: nb.CredentialName, BastionName: nb.BastionName,
		Stats: &pb.NodeStats{
			Sites: int32(nb.Stats.Sites), Routes: int32(nb.Stats.Routes), Upstreams: int32(nb.Stats.Upstreams),
			CertCount: int32(nb.Stats.CertCount), DriftCount: int32(nb.Stats.DriftCount),
		},
		Snapshot: &pb.Snapshot{
			Id: nb.Snapshot.ID, CapturedAt: nb.Snapshot.CapturedAt, Degraded: nb.Snapshot.Degraded,
			DegradedReason: nb.Snapshot.DegradedReason, FileCount: int32(nb.Snapshot.FileCount),
			BytesRaw: nb.Snapshot.BytesRaw, ParseState: nb.Snapshot.ParseState,
		},
		HasDrift: nb.HasDrift, NoDriftReason: nb.NoDriftReason,
		SelectedSiteName: nb.SelectedSiteName, FileBody: nb.FileBody,
		ByteStart: int32(nb.ByteStart), LineStart: int32(nb.LineStart),
	}
	for _, c := range nb.Credentials {
		resp.Credentials = append(resp.Credentials, &pb.Credential{
			Id: c.ID, Name: c.Name, Username: c.Username, AuthKind: c.AuthKind,
			PublicKey: c.PublicKey, Fingerprint: c.Fingerprint, ExternalRef: c.ExternalRef, CreatedAt: c.CreatedAt,
		})
	}
	for _, in := range nb.Instances {
		resp.Instances = append(resp.Instances, toPBInstance(in))
	}
	if nb.Selected != nil {
		resp.Selected = toPBInstance(*nb.Selected)
	}
	for _, hk := range nb.HostKeys {
		resp.HostKeys = append(resp.HostKeys, &pb.HostKey{
			Id: hk.ID, NodeId: hk.NodeID, Algorithm: hk.Algorithm, Fingerprint: hk.Fingerprint,
			State: hk.State, FirstSeenAt: hk.FirstSeenAt, DecidedAt: hk.DecidedAt.String,
		})
	}
	for _, pk := range nb.PendingKeys {
		resp.PendingKeys = append(resp.PendingKeys, &pb.PendingHostKey{
			Key: &pb.HostKey{
				Id: pk.ID, NodeId: pk.NodeID, Algorithm: pk.Algorithm, Fingerprint: pk.Fingerprint,
				State: pk.State, FirstSeenAt: pk.FirstSeenAt, DecidedAt: pk.DecidedAt.String,
			},
			NodeName: pk.NodeName, NodeAddress: pk.NodeAddress, Previous: pk.Previous,
		})
	}
	for _, t := range nb.Tabs {
		resp.Tabs = append(resp.Tabs, &pb.NodeTabItem{Label: t.Label, Href: t.Href, Count: int32(t.Count), On: t.On})
	}
	resp.Inst = toPBInst(nb.Inst)
	for _, up := range nb.Upstreams {
		resp.Upstreams = append(resp.Upstreams, &pb.UpstreamPool{
			Id: up.ID, Name: up.Name, Kind: up.Kind, BalanceMethod: up.BalanceMethod,
			MemberCount: int32(up.MemberCount), UsedBy: up.UsedBy, Resolution: up.Resolution, State: up.State,
		})
	}
	if nb.SelectedPool != nil {
		resp.SelectedPool = &pb.UpstreamPool{
			Id: nb.SelectedPool.ID, Name: nb.SelectedPool.Name, Kind: nb.SelectedPool.Kind,
			BalanceMethod: nb.SelectedPool.BalanceMethod, MemberCount: int32(nb.SelectedPool.MemberCount),
			UsedBy: nb.SelectedPool.UsedBy, Resolution: nb.SelectedPool.Resolution, State: nb.SelectedPool.State,
		}
	}
	for _, m := range nb.PoolMembers {
		resp.PoolMembers = append(resp.PoolMembers, &pb.UpstreamMember{
			Host: m.Host, Port: int32(m.Port), Weight: int32(m.Weight), Flags: m.Flags,
			Role: m.Role, NodeName: m.NodeName.String,
		})
	}
	for _, c := range nb.Certificates {
		resp.Certificates = append(resp.Certificates, &pb.NodeCertBinding{
			SubjectCn: c.SubjectCN, SiteName: c.SiteName, Port: int32(c.Port), NotAfter: c.NotAfter,
			Fingerprint: c.Fingerprint, KeyType: c.KeyType, ChainLength: int32(c.ChainLength),
		})
	}
	for _, f := range nb.Files {
		resp.Files = append(resp.Files, &pb.FileRef{
			Id: f.ID, Kind: f.Kind, Path: f.Path, BytesRaw: f.BytesRaw, Truncated: f.Truncated,
		})
	}
	if nb.SelectedFile != nil {
		resp.SelectedFile = &pb.FileRef{
			Id: nb.SelectedFile.ID, Kind: nb.SelectedFile.Kind, Path: nb.SelectedFile.Path,
			BytesRaw: nb.SelectedFile.BytesRaw, Truncated: nb.SelectedFile.Truncated,
		}
	}
	for _, run := range nb.DriftRuns {
		resp.DriftRuns = append(resp.DriftRuns, &pb.DriftRun{
			Id: run.ID, InstanceId: run.InstanceID, BaselineKind: run.BaselineKind,
			BaselineLabel: run.BaselineLabel, ComputedAt: run.ComputedAt, FindingCount: int32(run.FindingCount),
		})
	}
	for _, f := range nb.DriftFindings {
		resp.DriftFindings = append(resp.DriftFindings, &pb.DriftFinding{
			Id: f.ID, ObjectKind: f.ObjectKind, NaturalKey: f.NaturalKey, Change: f.Change,
			Field: f.Field, BaselineText: f.BaselineText, SubjectText: f.SubjectText, ActionClass: f.ActionClass,
		})
	}
	if nb.DriftByObject != nil {
		resp.DriftByObject = map[string]int32{}
		for k, v := range nb.DriftByObject {
			resp.DriftByObject[k] = int32(v)
		}
	}
	resp.SelectedRoute = toPBRoute(nb.SelectedRoute)
	if nb.RouteEffect != nil {
		resp.RouteEffect = &pb.RouteEffect{
			UpstreamName: nb.RouteEffect.UpstreamName, MemberCount: int32(nb.RouteEffect.MemberCount),
			BalanceMethod: nb.RouteEffect.BalanceMethod, Members: toPBUpstreamMemberRefs(nb.RouteEffect.Members),
		}
	}
	return resp
}

func (s *Server) loadUpstreamsTab(ctx context.Context, nb *nodeBuild, snapshotID, poolID int64) error {
	pools, err := s.DB.NodeUpstreams(ctx, snapshotID)
	if err != nil {
		return err
	}
	nb.Upstreams = pools

	if poolID == 0 && len(pools) > 0 {
		poolID = pools[0].ID
	}
	for i := range pools {
		if pools[i].ID == poolID {
			nb.SelectedPool = &pools[i]
			members, err := s.DB.UpstreamMembers(ctx, poolID)
			if err != nil {
				return err
			}
			nb.PoolMembers = members
			break
		}
	}
	return nil
}

func (s *Server) loadCertificatesTab(ctx context.Context, nb *nodeBuild, snapshotID int64) error {
	query := `
		SELECT
			c.subject_cn,
			COALESCE((SELECT GROUP_CONCAT(DISTINCT sn.name)
			          FROM site_name sn WHERE sn.site_id = b.site_id), ''),
			COALESCE((SELECT l.port FROM listener l WHERE l.id = b.listener_id), 0),
			c.not_after,
			c.fingerprint_sha256,
			c.key_algorithm,
			c.key_bits,
			b.chain_depth
		FROM certificate_binding b
		JOIN certificate c ON c.id = b.certificate_id
		WHERE b.snapshot_id = ?
		ORDER BY c.not_after`

	rows, err := s.DB.R.QueryContext(ctx, query, snapshotID)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cb nodeCertBindingBuild
		var keyAlg string
		var keyBits sql.NullInt64
		if err := rows.Scan(&cb.SubjectCN, &cb.SiteName, &cb.Port, &cb.NotAfter, &cb.Fingerprint,
			&keyAlg, &keyBits, &cb.ChainLength); err != nil {
			return err
		}
		if keyBits.Valid {
			cb.KeyType = fmt.Sprintf("%s%d", keyAlg, keyBits.Int64)
		} else if keyAlg != "" {
			cb.KeyType = keyAlg
		}
		nb.Certificates = append(nb.Certificates, cb)
	}
	return rows.Err()
}

func (s *Server) loadFilesTab(ctx context.Context, nb *nodeBuild, snapshotID, fileID int64, byteStartParam string) error {
	files, err := s.DB.SnapshotFiles(ctx, snapshotID)
	if err != nil {
		return err
	}
	nb.Files = files

	if fileID == 0 {
		return nil
	}
	for i := range nb.Files {
		if nb.Files[i].ID == fileID {
			nb.SelectedFile = &nb.Files[i]
			break
		}
	}
	if nb.SelectedFile == nil {
		return nil
	}

	body, err := s.DB.Blob(ctx, nb.SelectedFile.Digest)
	if err != nil {
		return err
	}
	content := string(body)
	nb.FileBody = content

	if byteStart, err := strconv.Atoi(byteStartParam); err == nil {
		nb.ByteStart = byteStart
		nb.LineStart = 1
		for i := 0; i < byteStart && i < len(content); i++ {
			if content[i] == '\n' {
				nb.LineStart++
			}
		}
	}
	return nil
}

func (s *Server) loadDriftTab(ctx context.Context, nb *nodeBuild) error {
	if nb.Selected == nil {
		return nil
	}
	runs, err := s.DB.DriftRuns(ctx, 0, "")
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.InstanceID == nb.Selected.ID {
			nb.DriftRuns = append(nb.DriftRuns, run)
		}
	}
	if len(nb.DriftRuns) == 0 {
		if nb.Selected.ClusterID.Valid {
			nb.NoDriftReason = "Drift detection requires a previous snapshot or cluster baseline to compare against."
		} else {
			nb.NoDriftReason = "This node is not in a cluster. Drift detection requires cluster membership or a previous snapshot."
		}
		return nil
	}

	var runIDs []int64
	for _, run := range nb.DriftRuns {
		runIDs = append(runIDs, run.ID)
		if run.FindingCount > 0 {
			nb.HasDrift = true
		}
	}
	findings, err := s.DB.DriftFindings(ctx, runIDs)
	if err != nil {
		return err
	}
	nb.DriftFindings = findings

	nb.DriftByObject = make(map[string]int)
	for _, f := range findings {
		if f.IgnoredBy.Valid {
			continue
		}
		nb.DriftByObject[f.ObjectKind]++
	}
	return nil
}

func loadRoutesTab(nb *nodeBuild, site, routePattern string) {
	if nb.Inst == nil {
		return
	}
	nb.SelectedSiteName = site

	if nb.SelectedSiteName == "" && len(nb.Inst.Sites) > 0 {
		for _, s := range nb.Inst.Sites {
			if len(s.Routes) > 0 {
				nb.SelectedSiteName = s.PrimaryName
				break
			}
		}
	}

	for _, s := range nb.Inst.Sites {
		if s.PrimaryName != nb.SelectedSiteName {
			continue
		}
		if routePattern == "" && len(s.Routes) > 0 {
			nb.SelectedRoute = s.Routes[0]
		} else {
			for _, route := range s.Routes {
				if route.Pattern == routePattern {
					nb.SelectedRoute = route
					break
				}
			}
		}
		if nb.SelectedRoute != nil {
			effect := &routeEffectBuild{}
			if nb.SelectedRoute.UpstreamID.Valid {
				if up, ok := nb.Inst.Upstreams[nb.SelectedRoute.UpstreamID.Int64]; ok {
					effect.UpstreamName = up.Name
					effect.MemberCount = len(up.Members)
					effect.BalanceMethod = up.BalanceMethod
					if effect.BalanceMethod == "" {
						effect.BalanceMethod = "round robin"
					}
					effect.Members = up.Members
				}
			}
			nb.RouteEffect = effect
		}
		break
	}
}
