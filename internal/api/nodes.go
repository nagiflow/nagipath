package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
)

// listCap bounds how many rows GET /nodes returns in one call, same as
// internal/web/nodes.go's listCap.
const listCap = 500

// getNodes ports internal/web/nodes.go's nodes(): same ?q= filter over
// display name/address, same fleet-wide pending/quarantined/never-collected
// counts. The htmx row-expand fragment isn't ported — GET /nodes/{id} is one
// click away and covers the same ground.
func (s *Server) getNodes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nodes, err := s.DB.NodesAggregated(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q != "" {
		nodes = filterNodeRows(nodes, q)
	}

	total := len(nodes)
	threshold := s.DB.SettingInt(ctx, "quarantine_after_failures")
	resp := &pb.NodesListResponse{Query: q, Total: int32(total), Threshold: int32(threshold)}
	for _, n := range nodes {
		resp.Pending += int32(n.PendingHostKeys)
		if store.Quarantined(n.ConsecutiveFailures, threshold) {
			resp.Quarantined++
		}
		if !n.LastCollection.Valid {
			resp.NeverCollected++
		}
	}
	if total > listCap {
		nodes = nodes[:listCap]
	}
	for _, n := range nodes {
		resp.Nodes = append(resp.Nodes, &pb.NodeListRow{
			Id: n.ID, Address: n.Address, SshPort: int32(n.SSHPort), DisplayName: n.DisplayName,
			OsFamily: n.OSFamily, Enabled: n.Enabled, ConsecutiveFailures: int32(n.ConsecutiveFailures),
			InstanceCount: int32(n.InstanceCount), PendingHostKeys: int32(n.PendingHostKeys),
			LastCollection: n.LastCollection.String, LastStatus: n.LastStatus.String,
			Vendor: n.Vendor.String, Version: n.Version.String, Cluster: n.Cluster.String,
			ProcessCount: int32(n.ProcessCount), Listeners: n.Listeners.String, LastCaptured: n.LastCaptured.String,
		})
	}

	writeProto(w, http.StatusOK, resp)
}

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

// postAddNode ports internal/web/nodes.go's addNode().
func (s *Server) postAddNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address      string `json:"address"`
		Port         int    `json:"port"`
		DisplayName  string `json:"display_name"`
		Username     string `json:"username"`
		CredentialID int64  `json:"credential_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_body", "Could not parse request body.")
		return
	}
	address := strings.TrimSpace(body.Address)
	if address == "" {
		apiError(w, http.StatusUnprocessableEntity, "address_required", "An address is required.")
		return
	}
	if strings.Contains(address, "/") || strings.Contains(address, "-") && strings.Count(address, ".") == 3 {
		apiError(w, http.StatusUnprocessableEntity, "no_scanning", "nagipath does not scan networks; add one host at a time.")
		return
	}
	port := 22
	if body.Port > 0 {
		port = body.Port
	}
	name := strings.TrimSpace(body.DisplayName)
	if name == "" {
		name = address
	}
	username := strings.TrimSpace(body.Username)
	if username == "" {
		username = "nagipath"
	}
	var credID *int64
	if body.CredentialID > 0 {
		credID = &body.CredentialID
	}
	u := userOf(r)
	id, err := s.DB.AddNode(r.Context(), address, port, name, username, credID, nil, "manual", &u.ID)
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, "add_node_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// ---------------------------------------------------------------- node detail

// nodeBuild is getNode's working accumulator — plain store/trace types,
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
	}
}

func toPBInstance(in store.Instance) *pb.Instance {
	return &pb.Instance{
		Id: in.ID, NodeId: in.NodeID, ClusterId: in.ClusterID.Int64, Vendor: in.Vendor,
		DisplayName: in.DisplayName, Version: in.Version, MainConfigPath: in.MainConfigPath,
		NodeDisplayName: in.NodeDisplayName, SiteCount: int32(in.SiteCount), RouteCount: int32(in.RouteCount),
		CertCount: int32(in.CertCount), LastCaptured: in.LastCaptured.String, ClusterName: in.ClusterName,
		Degraded: in.Degraded, ParseState: in.ParseState, State: in.State(),
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
		Tab: nb.Tab, CredentialName: nb.CredentialName,
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

// getNode ports internal/web/nodes.go's nodeDetail() and its per-tab
// loaders, unchanged in structure: which process is selected, which tab's
// data is loaded, the collection-running flag React polls on.
func (s *Server) getNode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nodeID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	tab := r.PathValue("tab")

	n, err := s.DB.Node(ctx, nodeID)
	if err != nil {
		apiError(w, http.StatusNotFound, "not_found", "No such node.")
		return
	}

	nb := nodeBuild{Node: n, Tab: tab, Threshold: s.DB.SettingInt(ctx, "quarantine_after_failures")}
	if userOf(r).IsAdmin() {
		nb.Credentials, _ = s.DB.Credentials(ctx)
	}
	nb.HostKeys, _ = s.DB.HostKeys(ctx, nodeID)

	allPending, _ := s.DB.PendingHostKeys(ctx)
	for _, k := range allPending {
		if k.NodeID == nodeID {
			nb.PendingKeys = append(nb.PendingKeys, k)
		}
	}

	nb.Instances, err = s.DB.NodeProcesses(ctx, nodeID)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}

	if n.CredentialID.Valid {
		creds, _ := s.DB.Credentials(ctx)
		for _, c := range creds {
			if c.ID == n.CredentialID.Int64 {
				nb.CredentialName = c.Name
				break
			}
		}
	}

	processID, _ := strconv.ParseInt(r.URL.Query().Get("process"), 10, 64)
	if processID == 0 && len(nb.Instances) > 0 {
		processID = nb.Instances[0].ID
	}
	for i := range nb.Instances {
		if nb.Instances[i].ID == processID {
			nb.Selected = &nb.Instances[i]
			break
		}
	}
	if nb.Selected == nil && len(nb.Instances) > 0 {
		nb.Selected = &nb.Instances[0]
	}

	if nb.Selected != nil {
		snap, err := s.DB.CurrentSnapshot(ctx, nb.Selected.ID)
		if err == nil {
			nb.Snapshot = snap
			nb.Stats, _ = s.DB.NodeStats(ctx, nb.Selected.ID)

			if tab == "routes" || tab == "sites" || tab == "" {
				inst, err := trace.LoadInstance(ctx, s.DB, nb.Selected.ID)
				if err == nil && inst != nil {
					nb.Inst = inst
				}
			}

			var tabErr error
			switch tab {
			case "routes":
				s.loadRoutesTab(&nb, r)
			case "upstreams":
				tabErr = s.loadUpstreamsTab(ctx, &nb, snap.ID, r)
			case "certificates":
				tabErr = s.loadCertificatesTab(ctx, &nb, snap.ID)
			case "files":
				tabErr = s.loadFilesTab(ctx, &nb, snap.ID, r)
			case "drift":
				tabErr = s.loadDriftTab(ctx, &nb)
			}
			if tabErr != nil {
				apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", tabErr.Error())
				return
			}
		}
	}

	base := fmt.Sprintf("/nodes/%d", nodeID)
	nb.Tabs = []nodeTab{{Label: "Overview", Href: base, On: tab == ""}}
	if nb.Selected != nil {
		process := fmt.Sprintf("?process=%d", nb.Selected.ID)
		nb.Tabs = []nodeTab{
			{Label: "Overview", Href: base + process, On: tab == ""},
			{Label: "Sites", Href: base + "/sites" + process, Count: nb.Selected.SiteCount, On: tab == "sites"},
			{Label: "Routes", Href: base + "/routes" + process, Count: nb.Selected.RouteCount, On: tab == "routes"},
			{Label: "Upstreams", Href: base + "/upstreams" + process, On: tab == "upstreams"},
			{Label: "Certificates", Href: base + "/certificates" + process, Count: nb.Selected.CertCount, On: tab == "certificates"},
			{Label: "Config files", Href: base + "/files" + process, On: tab == "files"},
			{Label: "Drift", Href: base + "/drift" + process, On: tab == "drift"},
		}
	}

	cols, _ := s.DB.Collections(ctx, 10)
	for _, c := range cols {
		if c.NodeID == nodeID && c.Status == "running" {
			nb.Running = true
			break
		}
	}

	writeProto(w, http.StatusOK, nb.toProto())
}

func (s *Server) loadUpstreamsTab(ctx context.Context, nb *nodeBuild, snapshotID int64, r *http.Request) error {
	pools, err := s.DB.NodeUpstreams(ctx, snapshotID)
	if err != nil {
		return err
	}
	nb.Upstreams = pools

	poolID, _ := strconv.ParseInt(r.URL.Query().Get("pool"), 10, 64)
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

func (s *Server) loadFilesTab(ctx context.Context, nb *nodeBuild, snapshotID int64, r *http.Request) error {
	files, err := s.DB.SnapshotFiles(ctx, snapshotID)
	if err != nil {
		return err
	}
	nb.Files = files

	fileID, err := strconv.ParseInt(r.URL.Query().Get("file"), 10, 64)
	if err != nil {
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

	if byteStart, err := strconv.Atoi(r.URL.Query().Get("b")); err == nil {
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

func (s *Server) loadRoutesTab(nb *nodeBuild, r *http.Request) {
	if nb.Inst == nil {
		return
	}
	nb.SelectedSiteName = r.URL.Query().Get("site")
	routePattern := r.URL.Query().Get("route")

	if nb.SelectedSiteName == "" && len(nb.Inst.Sites) > 0 {
		for _, site := range nb.Inst.Sites {
			if len(site.Routes) > 0 {
				nb.SelectedSiteName = site.PrimaryName
				break
			}
		}
	}

	for _, site := range nb.Inst.Sites {
		if site.PrimaryName != nb.SelectedSiteName {
			continue
		}
		if routePattern == "" && len(site.Routes) > 0 {
			nb.SelectedRoute = site.Routes[0]
		} else {
			for _, route := range site.Routes {
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

// ---------------------------------------------------------------- mutations

func (s *Server) postCollectNode(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	u := userOf(r)
	s.DB.Audit(r.Context(), &u.ID, "collection.start", "node", &id, "")
	actor := u.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := s.Collector.Node(ctx, id, "manual", &actor); err != nil {
			s.Log.Warn("collection failed", "node", id, "err", err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (s *Server) postDeleteNode(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	u := userOf(r)
	if err := s.DB.DeleteNode(r.Context(), id, &u.ID); err != nil {
		apiError(w, http.StatusUnprocessableEntity, "delete_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) postChangeNodeCredential(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var body struct {
		CredentialID int64 `json:"credential_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CredentialID <= 0 {
		apiError(w, http.StatusUnprocessableEntity, "credential_required", "Select a credential.")
		return
	}
	u := userOf(r)
	if err := s.DB.SetNodeCredential(r.Context(), id, body.CredentialID, &u.ID); err != nil {
		apiError(w, http.StatusUnprocessableEntity, "update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) postDecideHostKey(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var body struct {
		Decision string `json:"decision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_body", "Could not parse request body.")
		return
	}
	u := userOf(r)
	approve := body.Decision == "approve"
	if err := s.DB.DecideHostKey(r.Context(), id, approve, &u.ID); err != nil {
		apiError(w, http.StatusUnprocessableEntity, "decide_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "approved": approve})
}
