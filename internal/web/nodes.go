package web

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
)

// ---------------------------------------------------------------- nodes

// listCap bounds how many rows /nodes renders in one page load.
const listCap = 500

type nodesData struct {
	Nodes          []store.NodeListRow
	Query          string
	Total          int
	Threshold      int
	Pending        int
	Quarantined    int
	NeverCollected int
	// For htmx expansion
	ExpandedNodeID int64
	ExpandedProcs  []store.Instance
}

func (s *Server) nodes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// htmx request for process expansion
	if r.Header.Get("HX-Request") == "true" && r.URL.Query().Get("expand") != "" {
		nodeID, _ := strconv.ParseInt(r.URL.Query().Get("expand"), 10, 64)
		procs, err := s.DB.NodeProcesses(ctx, nodeID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.renderFragment(w, r, "nodeprocs", procs)
		return
	}

	nodes, err := s.DB.NodesAggregated(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q != "" {
		nodes = filterNodeRows(nodes, q)
	}

	total := len(nodes)
	threshold := s.DB.SettingInt(ctx, "quarantine_after_failures")

	d := nodesData{Query: q, Total: total, Threshold: threshold}

	for _, n := range nodes {
		d.Pending += n.PendingHostKeys
		if store.Quarantined(n.ConsecutiveFailures, threshold) {
			d.Quarantined++
		}
		if !n.LastCollection.Valid {
			d.NeverCollected++
		}
	}

	if total > listCap {
		nodes = nodes[:listCap]
	}
	d.Nodes = nodes

	s.render(w, r, "nodes.html", "Nodes", d)
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

func (s *Server) addNode(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	address := strings.TrimSpace(r.FormValue("address"))
	if address == "" {
		redirect(w, r, "/nodes", "", "an address is required")
		return
	}
	if strings.Contains(address, "/") || strings.Contains(address, "-") && strings.Count(address, ".") == 3 {
		redirect(w, r, "/nodes", "", "nagipath does not scan networks; add one host at a time")
		return
	}
	port := 22
	if p, err := strconv.Atoi(r.FormValue("port")); err == nil && p > 0 {
		port = p
	}
	name := strings.TrimSpace(r.FormValue("display_name"))
	if name == "" {
		name = address
	}
	username := strings.TrimSpace(r.FormValue("username"))
	if username == "" {
		username = "nagipath"
	}
	var credID *int64
	if v, err := strconv.ParseInt(r.FormValue("credential_id"), 10, 64); err == nil && v > 0 {
		credID = &v
	}
	id, err := s.DB.AddNode(ctx, address, port, name, username, credID, nil, "manual", &u.ID)
	if err != nil {
		redirect(w, r, "/nodes", "", err.Error())
		return
	}
	redirect(w, r, fmt.Sprintf("/nodes/%d", id), "node added; collect to fetch its host key", "")
}

// ---------------------------------------------------------------- node detail

type nodeDetailData struct {
	Node        store.Node
	Credentials []store.Credential
	Instances   []store.Instance
	Selected    *store.Instance
	HostKeys    []store.HostKey
	Collections []store.Collection
	Stats       store.NodeStats
	Running     bool
	Threshold   int
	Tabs        []navItem
	Tab         string
	// CredentialName is the credential this node authenticates with, resolved to
	// its name here so the Identity panel can print it. Empty means none is
	// assigned, which is why collection has never run.
	CredentialName string
	// Tab-specific data
	Inst           *trace.Inst
	Upstreams      []store.UpstreamPool
	SelectedPool   *store.UpstreamPool
	PoolMembers    []store.UpstreamMember
	Certificates   []nodeCertBinding
	Files          []store.FileRef
	Snapshot       store.Snapshot
	SelectedFileID int64
	SelectedFile   *store.FileRef
	FileBody       string
	ByteStart      int
	LineStart      int
	// Drift tab
	DriftRuns     []store.DriftRun
	DriftFindings []store.DriftFinding
	DriftByObject map[string]int
	HasDrift      bool
	NoDriftReason string
	// Routes tab
	SelectedSiteName string
	SelectedRoute    *trace.Route
	RouteEffect      *routeEffect
}

// nodeCertBinding is certificate binding data for the node certificates tab.
type nodeCertBinding struct {
	SubjectCN   string
	SiteName    string
	Port        int
	NotAfter    string
	Fingerprint string
	KeyType     string
	ChainLength int
}

// routeEffect summarizes what a route does in plain words.
type routeEffect struct {
	UpstreamName  string
	MemberCount   int
	BalanceMethod string
	Members       []*trace.Member
	PathRewritten string
	Headers       []string
	Timeout       string
}

func (s *Server) nodeDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nodeID := idOf(r, "id")
	tab := r.PathValue("tab")

	n, err := s.DB.Node(ctx, nodeID)
	if err != nil {
		s.notFound(w, r)
		return
	}

	d := nodeDetailData{
		Node:      n,
		Tab:       tab,
		Threshold: s.DB.SettingInt(ctx, "quarantine_after_failures"),
	}
	if userOf(r).IsAdmin() {
		d.Credentials, _ = s.DB.Credentials(ctx)
	}

	d.HostKeys, _ = s.DB.HostKeys(ctx, nodeID)

	// Not `, _`: every tab on this page renders off the selected process, so a query
	// error here empties all of them and the page still answers 200. A bad column
	// name hid behind that for a whole session.
	d.Instances, err = s.DB.NodeProcesses(ctx, nodeID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	// ponytail: one pass over every credential rather than a by-id query. The list
	// is a handful of rows; add the lookup when it isn't.
	if n.CredentialID.Valid {
		creds, _ := s.DB.Credentials(ctx)
		for _, c := range creds {
			if c.ID == n.CredentialID.Int64 {
				d.CredentialName = c.Name
				break
			}
		}
	}

	// Select which process to show
	processID, _ := strconv.ParseInt(r.URL.Query().Get("process"), 10, 64)
	if processID == 0 && len(d.Instances) > 0 {
		processID = d.Instances[0].ID
	}
	for i := range d.Instances {
		if d.Instances[i].ID == processID {
			d.Selected = &d.Instances[i]
			break
		}
	}
	if d.Selected == nil && len(d.Instances) > 0 {
		d.Selected = &d.Instances[0]
	}

	// Load current snapshot for the selected process
	if d.Selected != nil {
		snap, err := s.DB.CurrentSnapshot(ctx, d.Selected.ID)
		if err == nil {
			d.Snapshot = snap
			d.Stats, _ = s.DB.NodeStats(ctx, d.Selected.ID)

			// Load parsed instance for routes/sites tabs
			if tab == "routes" || tab == "sites" || tab == "" {
				inst, err := trace.LoadInstance(ctx, s.DB, d.Selected.ID)
				if err == nil && inst != nil {
					d.Inst = inst
				}
			}

			// Load tab-specific data. A loader that swallowed its own query error
			// rendered the tab's empty state, which reads as "this node has no
			// upstreams" — the one thing the page must not say when it does not know.
			var err error
			switch tab {
			case "routes":
				s.loadRoutesTab(ctx, &d, r)
			case "upstreams":
				err = s.loadUpstreamsTab(ctx, &d, snap.ID, r)
			case "certificates":
				err = s.loadCertificatesTab(ctx, &d, snap.ID)
			case "files":
				err = s.loadFilesTab(ctx, &d, snap.ID, r)
			case "drift":
				err = s.loadDriftTab(ctx, &d)
			}
			if err != nil {
				s.serverError(w, r, err)
				return
			}
		}
	}

	// Build tabs - always show tabs, even if no instances yet
	base := fmt.Sprintf("/nodes/%d", nodeID)
	d.Tabs = []navItem{
		{Label: "Overview", Href: base, On: tab == ""},
	}
	if d.Selected != nil {
		process := fmt.Sprintf("?process=%d", d.Selected.ID)
		d.Tabs = []navItem{
			{Label: "Overview", Href: base + process, On: tab == ""},
			{Label: "Sites", Href: base + "/sites" + process, Count: d.Selected.SiteCount, On: tab == "sites"},
			{Label: "Routes", Href: base + "/routes" + process, Count: d.Selected.RouteCount, On: tab == "routes"},
			{Label: "Upstreams", Href: base + "/upstreams" + process, On: tab == "upstreams"},
			{Label: "Certificates", Href: base + "/certificates" + process, Count: d.Selected.CertCount, On: tab == "certificates"},
			{Label: "Config files", Href: base + "/files" + process, On: tab == "files"},
			{Label: "Drift", Href: base + "/drift" + process, On: tab == "drift"},
		}
	}

	// Check if collection is running
	cols, _ := s.DB.Collections(ctx, 10)
	for _, c := range cols {
		if c.NodeID == nodeID && c.Status == "running" {
			d.Running = true
			break
		}
	}

	s.render(w, r, "node.html", n.DisplayName, d)
}

func (s *Server) loadUpstreamsTab(ctx context.Context, d *nodeDetailData, snapshotID int64, r *http.Request) error {
	pools, err := s.DB.NodeUpstreams(ctx, snapshotID)
	if err != nil {
		return err
	}
	d.Upstreams = pools

	// Select pool from query param or first by default
	poolID, _ := strconv.ParseInt(r.URL.Query().Get("pool"), 10, 64)
	if poolID == 0 && len(pools) > 0 {
		poolID = pools[0].ID
	}
	for i := range pools {
		if pools[i].ID == poolID {
			d.SelectedPool = &pools[i]
			members, err := s.DB.UpstreamMembers(ctx, poolID)
			if err != nil {
				return err
			}
			d.PoolMembers = members
			break
		}
	}
	return nil
}

func (s *Server) loadCertificatesTab(ctx context.Context, d *nodeDetailData, snapshotID int64) error {
	// Key type and chain length are already parsed and stored in the database
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
		var cb nodeCertBinding
		var keyAlg string
		var keyBits sql.NullInt64
		err := rows.Scan(&cb.SubjectCN, &cb.SiteName, &cb.Port, &cb.NotAfter, &cb.Fingerprint,
			&keyAlg, &keyBits, &cb.ChainLength)
		if err != nil {
			return err
		}

		// Format key type as "rsa2048" or "ec256"
		if keyBits.Valid {
			cb.KeyType = fmt.Sprintf("%s%d", keyAlg, keyBits.Int64)
		} else if keyAlg != "" {
			cb.KeyType = keyAlg
		}

		d.Certificates = append(d.Certificates, cb)
	}
	return rows.Err()
}

func (s *Server) loadFilesTab(ctx context.Context, d *nodeDetailData, snapshotID int64, r *http.Request) error {
	files, err := s.DB.SnapshotFiles(ctx, snapshotID)
	if err != nil {
		return err
	}
	d.Files = files

	// If a file is selected via ?file=, load and render it. A ?file= naming
	// something not in this snapshot selects nothing rather than erroring: it is a
	// stale link, not a broken database.
	fileID, err := strconv.ParseInt(r.URL.Query().Get("file"), 10, 64)
	if err != nil {
		return nil
	}
	d.SelectedFileID = fileID
	for i := range d.Files {
		if d.Files[i].ID == fileID {
			d.SelectedFile = &d.Files[i]
			break
		}
	}
	if d.SelectedFile == nil {
		return nil
	}

	// Content is a blob, addressed by digest. There is no `file` table — this read
	// `SELECT content FROM file WHERE id = ?` and never returned a body, which is
	// why the panel was blank for every file anyone opened.
	body, err := s.DB.Blob(ctx, d.SelectedFile.Digest)
	if err != nil {
		return err
	}
	content := string(body)
	d.FileBody = content

	// Handle byte range highlight if ?b= is present
	if byteStart, err := strconv.Atoi(r.URL.Query().Get("b")); err == nil {
		d.ByteStart = byteStart
		// Find which line this byte offset is on
		d.LineStart = 1
		for i := 0; i < byteStart && i < len(content); i++ {
			if content[i] == '\n' {
				d.LineStart++
			}
		}
	}
	return nil
}

func (s *Server) loadDriftTab(ctx context.Context, d *nodeDetailData) error {
	if d.Selected == nil {
		return nil
	}

	// Get latest drift runs for this instance
	runs, err := s.DB.DriftRuns(ctx, 0, "")
	if err != nil {
		return err
	}

	// Filter to this instance's runs
	for _, run := range runs {
		if run.InstanceID == d.Selected.ID {
			d.DriftRuns = append(d.DriftRuns, run)
		}
	}

	if len(d.DriftRuns) == 0 {
		// No drift runs - determine why
		if d.Selected.ClusterID.Valid {
			d.NoDriftReason = "Drift detection requires a previous snapshot or cluster baseline to compare against."
		} else {
			d.NoDriftReason = "This node is not in a cluster. Drift detection requires cluster membership or a previous snapshot."
		}
		return nil
	}

	// Get findings for the latest run
	var runIDs []int64
	for _, run := range d.DriftRuns {
		runIDs = append(runIDs, run.ID)
		if run.FindingCount > 0 {
			d.HasDrift = true
		}
	}

	findings, err := s.DB.DriftFindings(ctx, runIDs)
	if err != nil {
		return err
	}
	d.DriftFindings = findings

	// Group findings by object kind for summary
	d.DriftByObject = make(map[string]int)
	for _, f := range findings {
		if f.IgnoredBy.Valid {
			continue // Skip ignored findings
		}
		d.DriftByObject[f.ObjectKind]++
	}
	return nil
}

func (s *Server) loadRoutesTab(ctx context.Context, d *nodeDetailData, r *http.Request) {
	if d.Inst == nil {
		return
	}

	// Get selected site and route from query params
	d.SelectedSiteName = r.URL.Query().Get("site")
	routePattern := r.URL.Query().Get("route")

	// If no site selected, default to first site with routes
	if d.SelectedSiteName == "" && len(d.Inst.Sites) > 0 {
		for _, site := range d.Inst.Sites {
			if len(site.Routes) > 0 {
				d.SelectedSiteName = site.PrimaryName
				break
			}
		}
	}

	// Find the selected site and route
	for _, site := range d.Inst.Sites {
		if site.PrimaryName != d.SelectedSiteName {
			continue
		}

		// If no route selected, pick first
		if routePattern == "" && len(site.Routes) > 0 {
			d.SelectedRoute = site.Routes[0]
		} else {
			// Find route by pattern
			for _, route := range site.Routes {
				if route.Pattern == routePattern {
					d.SelectedRoute = route
					break
				}
			}
		}

		// Build effect panel
		if d.SelectedRoute != nil {
			effect := &routeEffect{}

			// Find upstream if route has one
			if d.SelectedRoute.UpstreamID.Valid {
				for _, up := range d.Inst.Upstreams {
					if up.ID == d.SelectedRoute.UpstreamID.Int64 {
						effect.UpstreamName = up.Name
						effect.MemberCount = len(up.Members)
						effect.BalanceMethod = up.BalanceMethod
						if effect.BalanceMethod == "" {
							effect.BalanceMethod = "round robin"
						}
						effect.Members = up.Members
						break
					}
				}
			}

			d.RouteEffect = effect
		}
		break
	}
}

func (s *Server) collectNode(w http.ResponseWriter, r *http.Request) {
	id, u := idOf(r, "id"), userOf(r)
	s.DB.Audit(r.Context(), &u.ID, "collection.start", "node", &id, "")
	actor := u.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := s.collector.Node(ctx, id, "manual", &actor); err != nil {
			s.Log.Warn("collection failed", "node", id, "err", err)
		}
	}()
	redirect(w, r, fmt.Sprintf("/nodes/%d", id), "collection started", "")
}

func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	if err := s.DB.DeleteNode(r.Context(), idOf(r, "id"), &u.ID); err != nil {
		redirect(w, r, "/nodes", "", err.Error())
		return
	}
	redirect(w, r, "/nodes", "node removed", "")
}

func (s *Server) changeNodeCredential(w http.ResponseWriter, r *http.Request) {
	id, u := idOf(r, "id"), userOf(r)
	credentialID, err := strconv.ParseInt(r.FormValue("credential_id"), 10, 64)
	if err != nil || credentialID <= 0 {
		redirect(w, r, fmt.Sprintf("/nodes/%d", id), "", "select a credential")
		return
	}
	if err := s.DB.SetNodeCredential(r.Context(), id, credentialID, &u.ID); err != nil {
		redirect(w, r, fmt.Sprintf("/nodes/%d", id), "", err.Error())
		return
	}
	redirect(w, r, fmt.Sprintf("/nodes/%d", id), "credential updated", "")
}

// ---------------------------------------------------------------- helpers

// backTo is where a form that can be submitted from more than one screen returns
// to. It only ever yields a path on this server: "//evil.example" starts with a
// slash but a browser reads it as a host, so a bare HasPrefix("/") check is an
// open redirect.
func backTo(r *http.Request, def string) string {
	back := r.FormValue("back")
	if !strings.HasPrefix(back, "/") || strings.HasPrefix(back, "//") ||
		strings.Contains(back, "\\") {
		return def
	}
	return back
}

func (s *Server) decideHostKey(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	id := idOf(r, "id")
	approve := r.FormValue("decision") == "approve"
	if err := s.DB.DecideHostKey(ctx, id, approve, &u.ID); err != nil {
		redirect(w, r, "/nodes", "", err.Error())
		return
	}
	back := backTo(r, "/nodes")
	if approve {
		redirect(w, r, back, "host key approved", "")
		return
	}
	redirect(w, r, back, "host key rejected", "")
}
