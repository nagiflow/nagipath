package web

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
)

// Version is stamped by main and used in the Probe User-Agent, so a request in a
// customer's access log can be attributed to a specific nagipath build.
var Version = "dev"

// ---------------------------------------------------------------- rule lookup

type rulesData struct {
	Query   trace.LookupQuery
	Results []trace.LookupResult
	Classes []string
	Vendors []string
	// Answered and Silent split the results: instances with candidates, and
	// instances that were considered and had none.
	Answered []trace.LookupResult
	Silent   []trace.LookupResult
	Rules    int
	Asked    bool
	Empty    bool
}

// rules answers "which rules are in effect for this context path" without
// following the request anywhere. It is the same site and route selection the
// trace engine uses, stopped after one hop.
func (s *Server) rules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	d := rulesData{Classes: trace.ActionClasses}
	d.Query = trace.LookupQuery{
		Hostname: strings.TrimSpace(q.Get("hostname")),
		Path:     strings.TrimSpace(q.Get("path")),
		Scheme:   q.Get("scheme"),
		Classes:  nonEmpty(q["class"]),
		Vendors:  nonEmpty(q["vendor"]),
	}
	if p, err := strconv.Atoi(q.Get("port")); err == nil {
		d.Query.Port = p
	}
	d.Query = d.Query.Normalise()

	instances, err := s.DB.Instances(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	d.Empty = len(instances) == 0
	seen := map[string]bool{}
	for _, in := range instances {
		if !seen[in.Vendor] {
			seen[in.Vendor] = true
			d.Vendors = append(d.Vendors, in.Vendor)
		}
	}
	sort.Strings(d.Vendors)

	if d.Query.Hostname == "" || d.Empty {
		s.render(w, r, "rules.html", "Rule lookup", d)
		return
	}
	d.Asked = true
	top, err := trace.Load(ctx, s.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	d.Results = trace.Lookup(top, d.Query)
	for _, res := range d.Results {
		if len(res.Rules) > 0 {
			d.Answered = append(d.Answered, res)
			d.Rules += len(res.Rules)
			continue
		}
		d.Silent = append(d.Silent, res)
	}
	s.render(w, r, "rules.html", "Rule lookup", d)
}

func nonEmpty(list []string) []string {
	var out []string
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ---------------------------------------------------------------- fleet

const fleetPageSize = 50

type fleetData struct {
	Nodes     []store.Node
	ByNode    map[int64][]store.Instance
	Total     int
	Instances int
	Degraded  int
	Pending   int
	// A cluster is the level above a node here: the fleet is where the grouping is
	// read, named, and where "in no cluster" is visible.
	Groups     []fleetGroup
	Clusters   []store.Cluster
	Members    map[int64][]store.Instance
	Unassigned []store.Instance
	// From and To are 1-based and inclusive, for the "1–50 of 137" line.
	From, To, Page, Pages int
}

// fleetGroup is one cluster's nodes. Membership is per instance, so a node belongs
// to the cluster its instances do.
type fleetGroup struct {
	ID      int64 // 0 for the nodes in no cluster
	Cluster store.Cluster
	Nodes   []store.Node
}

// fleet is nodes and their instances in one nested list, because "which node is
// this instance on" is the question every other screen makes the operator ask.
func (s *Server) fleet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// ponytail: clusters are re-derived when the fleet is opened, which keeps the
	// grouping true after any collection without a scheduler to own it. Move it to
	// the end of a collection when a fleet is big enough for this to be felt.
	if err := s.DB.ReconcileClusters(ctx); err != nil {
		s.Log.Warn("cluster discovery", "err", err)
	}
	nodes, err := s.DB.Nodes(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	instances, err := s.DB.Instances(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	d := fleetData{ByNode: map[int64][]store.Instance{}, Total: len(nodes),
		Instances: len(instances), Pending: s.DB.PendingHostKeyCount(ctx),
		Members: map[int64][]store.Instance{}}
	for _, in := range instances {
		d.ByNode[in.NodeID] = append(d.ByNode[in.NodeID], in)
		if in.State() == "degraded" || in.State() == "unparsed" {
			d.Degraded++
		}
		if in.ClusterID.Valid {
			d.Members[in.ClusterID.Int64] = append(d.Members[in.ClusterID.Int64], in)
			continue
		}
		d.Unassigned = append(d.Unassigned, in)
	}
	all, err := s.DB.Clusters(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// A cluster with no members is a name kept for a group that has diverged. It is
	// not a thing in the fleet right now, so it is not listed as one.
	for _, c := range all {
		if len(d.Members[c.ID]) > 0 {
			d.Clusters = append(d.Clusters, c)
		}
	}

	// ponytail: offset paging on a list the operator sorts by nothing. A fleet
	// large enough for this to be slow is large enough to want filters, which is
	// when to replace it.
	d.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if d.Page < 1 {
		d.Page = 1
	}
	d.Pages = (len(nodes) + fleetPageSize - 1) / fleetPageSize
	if d.Pages == 0 {
		d.Pages = 1
	}
	if d.Page > d.Pages {
		d.Page = d.Pages
	}
	start := (d.Page - 1) * fleetPageSize
	end := min(start+fleetPageSize, len(nodes))
	d.Nodes = nodes[start:end]
	if len(d.Nodes) > 0 {
		d.From, d.To = start+1, end
	}
	d.Groups = groupByCluster(d.Nodes, d.ByNode, d.Clusters)
	s.render(w, r, "fleet.html", "Fleet", d)
}

// groupByCluster puts each node under the cluster its instances belong to, in the
// cluster order given, with the unclustered nodes last.
func groupByCluster(nodes []store.Node, byNode map[int64][]store.Instance,
	clusters []store.Cluster) []fleetGroup {
	of := make(map[int64]int64, len(nodes))
	for _, n := range nodes {
		for _, in := range byNode[n.ID] {
			// ponytail: first membership wins. A node whose instances are in two
			// clusters is listed under one of them; split the row when a fleet
			// actually does that.
			if in.ClusterID.Valid {
				of[n.ID] = in.ClusterID.Int64
				break
			}
		}
	}
	var out []fleetGroup
	for _, c := range clusters {
		g := fleetGroup{ID: c.ID, Cluster: c}
		for _, n := range nodes {
			if of[n.ID] == c.ID {
				g.Nodes = append(g.Nodes, n)
			}
		}
		if len(g.Nodes) > 0 {
			out = append(out, g)
		}
	}
	var rest fleetGroup
	for _, n := range nodes {
		if of[n.ID] == 0 {
			rest.Nodes = append(rest.Nodes, n)
		}
	}
	if len(rest.Nodes) > 0 {
		out = append(out, rest)
	}
	return out
}

// ---------------------------------------------------------------- drift

type driftGroup struct {
	Kind      string
	Key       string
	Change    string
	Instances []string
	FirstSeen string
	Findings  []store.DriftFinding
}

type driftData struct {
	Clusters   []store.Cluster
	Cluster    store.Cluster
	ClusterID  int64
	Baseline   string
	Baselines  []struct{ Kind, Label string }
	Members    []store.Instance
	Unassigned []store.Instance
	Counts     map[int64]int
	Ignored    map[int64]int
	// RunAt dates a finding: the finding row itself has no timestamp, its run does.
	RunAt    map[int64]string
	Runs     []store.DriftRun
	Findings []store.DriftFinding
	Groups   []driftGroup
	Ignores  []store.IgnoreRule
	GroupBy  string
	Objects  int
	Empty    bool
}

func (s *Server) drift(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := driftData{Baselines: store.BaselineLabels, Counts: map[int64]int{},
		Ignored: map[int64]int{}, RunAt: map[int64]string{},
		GroupBy: r.URL.Query().Get("group")}
	if d.GroupBy != "instance" {
		d.GroupBy = "object"
	}
	d.Baseline = r.URL.Query().Get("baseline")

	var err error
	if d.Clusters, err = s.DB.Clusters(ctx); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	d.ClusterID, _ = strconv.ParseInt(r.URL.Query().Get("cluster"), 10, 64)
	if d.ClusterID == 0 && len(d.Clusters) > 0 {
		d.ClusterID = d.Clusters[0].ID
	}
	for _, c := range d.Clusters {
		if c.ID == d.ClusterID {
			d.Cluster = c
		}
	}

	instances, _ := s.DB.Instances(ctx)
	for _, in := range instances {
		switch {
		case in.ClusterID.Valid && in.ClusterID.Int64 == d.ClusterID:
			d.Members = append(d.Members, in)
		case !in.ClusterID.Valid:
			d.Unassigned = append(d.Unassigned, in)
		}
	}
	d.Empty = len(instances) == 0

	if d.ClusterID > 0 {
		d.Ignores, _ = s.DB.IgnoreRules(ctx, d.ClusterID)
	}
	d.Runs, _ = s.DB.DriftRuns(ctx, d.ClusterID, d.Baseline)
	var runIDs []int64
	for _, run := range d.Runs {
		runIDs = append(runIDs, run.ID)
		d.Counts[run.InstanceID] = run.FindingCount
		d.Ignored[run.InstanceID] = run.IgnoredCount
		d.RunAt[run.ID] = run.ComputedAt
	}
	d.Findings, _ = s.DB.DriftFindings(ctx, runIDs)
	d.Groups = groupFindings(d.Findings, d.GroupBy)
	d.Objects = len(d.Groups)
	s.render(w, r, "drift.html", "Drift", d)
}

// groupFindings collapses one finding per instance into one row per object, which
// is how the question is actually asked: "what is different about this location",
// not "what does each host say".
func groupFindings(list []store.DriftFinding, by string) []driftGroup {
	order := []string{}
	groups := map[string]*driftGroup{}
	for _, f := range list {
		key := f.ObjectKind + " " + f.NaturalKey
		if by == "instance" {
			key = f.InstanceName
		}
		g := groups[key]
		if g == nil {
			g = &driftGroup{Kind: f.ObjectKind, Key: f.NaturalKey, Change: f.Change,
				FirstSeen: f.Field}
			if by == "instance" {
				g.Kind, g.Key = "instance", f.InstanceName
			}
			groups[key] = g
			order = append(order, key)
		}
		g.Findings = append(g.Findings, f)
		if !slicesHas(g.Instances, f.InstanceName) {
			g.Instances = append(g.Instances, f.InstanceName)
		}
	}
	out := make([]driftGroup, 0, len(order))
	for _, k := range order {
		out = append(out, *groups[k])
	}
	// Most-diverged first: the object that differs on the most instances is the
	// one worth reading first.
	sort.SliceStable(out, func(i, j int) bool {
		return len(out[i].Instances) > len(out[j].Instances)
	})
	return out
}

func slicesHas(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// driftRecompute re-diffs every member of a cluster. It reports what it skipped:
// an instance whose snapshot predates the current parser version is refused
// rather than compared, because comparing across versions invents drift.
func (s *Server) driftRecompute(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.FormValue("cluster"), 10, 64)
	if id == 0 {
		redirect(w, r, "/drift", "", "pick a cluster to re-diff")
		return
	}
	u := userOf(r)
	s.DB.Audit(r.Context(), &u.ID, "drift.recompute", "cluster", &id, r.FormValue("baseline"))
	stored, skipped := s.DB.RecomputeCluster(r.Context(), id)
	back := fmt.Sprintf("/drift?cluster=%d", id)
	if len(skipped) > 0 {
		redirect(w, r, back, "", fmt.Sprintf("%d compared; %d skipped — %s",
			stored, len(skipped), strings.Join(skipped, "; ")))
		return
	}
	redirect(w, r, back, fmt.Sprintf("%d instance(s) compared", stored), "")
}

func (s *Server) driftIgnore(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	id, _ := strconv.ParseInt(r.FormValue("cluster"), 10, 64)
	back := fmt.Sprintf("/drift?cluster=%d", id)
	rule := store.IgnoreRule{
		ClusterID:  id,
		ObjectKind: strings.TrimSpace(r.FormValue("object_kind")),
		Field:      strings.TrimSpace(r.FormValue("field")),
		Pattern:    strings.TrimSpace(r.FormValue("pattern")),
		Reason:     strings.TrimSpace(r.FormValue("reason")),
	}
	if _, err := s.DB.AddIgnoreRule(ctx, rule, &u.ID); err != nil {
		redirect(w, r, back, "", err.Error())
		return
	}
	redirect(w, r, back, "ignored; matching findings stay visible as ignored", "")
}

func (s *Server) driftUnignore(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	if err := s.DB.DeleteIgnoreRule(r.Context(), idOf(r, "id"), &u.ID); err != nil {
		redirect(w, r, "/drift", "", err.Error())
		return
	}
	redirect(w, r, "/drift?cluster="+r.FormValue("cluster"), "ignore rule removed", "")
}

// driftGolden declares a golden peer. Declared, not detected: nagipath has no
// opinion which side of a divergence is correct, so a human names the reference.
func (s *Server) driftGolden(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	clusterID, _ := strconv.ParseInt(r.FormValue("cluster"), 10, 64)
	var inst *int64
	if v, err := strconv.ParseInt(r.FormValue("instance"), 10, 64); err == nil && v > 0 {
		inst = &v
	}
	back := fmt.Sprintf("/drift?cluster=%d", clusterID)
	if err := s.DB.SetGoldenPeer(r.Context(), clusterID, inst, &u.ID); err != nil {
		redirect(w, r, back, "", err.Error())
		return
	}
	redirect(w, r, back, "golden peer set", "")
}

// ---------------------------------------------------------------- onboarding

type onboardData struct {
	Step        int
	Nodes       []store.Node
	Credentials []store.Credential
	Pending     []store.PendingHostKey
	Collections []store.Collection
	// Imported and Refused are the result of the last inventory paste. Refused is
	// never empty-and-silent: a dropped host is a host that goes uncollected.
	Imported []string
	Refused  []string
	Source   string
}

// onboarding is the four-step first-run path: name the nodes, give them a
// credential, approve their host keys by hand, collect once. Nothing runs on a
// node before step three, and step three has no automatic answer.
func (s *Server) onboarding(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := onboardData{Source: r.URL.Query().Get("source")}
	if d.Source != "ini" && d.Source != "yaml" {
		d.Source = "list"
	}
	d.Nodes, _ = s.DB.Nodes(ctx)
	d.Credentials, _ = s.DB.Credentials(ctx)
	d.Pending, _ = s.DB.PendingHostKeys(ctx)
	d.Collections, _ = s.DB.Collections(ctx, 5)
	d.Step = onboardStep(d)
	s.render(w, r, "onboarding.html", "Connect the fleet", d)
}

// onboardStep is derived from state, never stored: an operator who leaves and
// comes back lands where the fleet actually is, not where a wizard remembers.
func onboardStep(d onboardData) int {
	switch {
	case len(d.Nodes) == 0:
		return 1
	case len(d.Credentials) == 0:
		return 2
	case len(d.Pending) > 0:
		return 3
	default:
		return 4
	}
}

// onboardNodes imports a pasted list or Ansible inventory. It is bulk entry, not
// discovery: every host in the text was named by a person, and anything that
// would expand into hosts nobody named is refused with a reason.
func (s *Server) onboardNodes(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	hosts, refused := parseInventory(r.FormValue("inventory"))
	if len(hosts) == 0 && len(refused) == 0 {
		redirect(w, r, "/onboarding", "", "no hosts in that text")
		return
	}
	var credID *int64
	if v, err := strconv.ParseInt(r.FormValue("credential_id"), 10, 64); err == nil && v > 0 {
		credID = &v
	}
	var bastion *int64
	if v, err := strconv.ParseInt(r.FormValue("bastion_id"), 10, 64); err == nil && v > 0 {
		bastion = &v
	}
	added := 0
	for _, h := range hosts {
		user := h.User
		if user == "" {
			user = strings.TrimSpace(r.FormValue("username"))
		}
		if _, err := s.DB.AddNode(ctx, h.Address, h.Port, h.Name, user, credID, bastion,
			"inventory", &u.ID); err != nil {
			refused = append(refused, h.label()+": "+err.Error())
			continue
		}
		added++
	}
	s.DB.AuditDetail(ctx, &u.ID, "node.import", "node", nil,
		fmt.Sprintf("%d added, %d refused", added, len(refused)),
		map[string]any{"added": added, "refused": refused}, "success", remoteAddr(r))
	msg := fmt.Sprintf("%d node(s) added; collect each one to fetch its host key", added)
	if len(refused) > 0 {
		redirect(w, r, "/onboarding", "", msg+" — refused: "+strings.Join(refused, "; "))
		return
	}
	redirect(w, r, "/onboarding", msg, "")
}

// approveHostKeys approves the keys the operator ticked. Each one is still an
// explicit decision on a specific fingerprint; there is no "trust everything"
// and no first-use shortcut anywhere in the product.
func (s *Server) approveHostKeys(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	ids := r.Form["key"]
	if len(ids) == 0 {
		redirect(w, r, "/onboarding", "", "no host key was selected")
		return
	}
	approve := r.FormValue("decision") != "reject"
	n := 0
	for _, raw := range ids {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			continue
		}
		if err := s.DB.DecideHostKey(ctx, id, approve, &u.ID); err != nil {
			redirect(w, r, "/onboarding", "", err.Error())
			return
		}
		n++
	}
	word := "approved"
	if !approve {
		word = "rejected"
	}
	redirect(w, r, "/onboarding", fmt.Sprintf("%d host key(s) %s", n, word), "")
}
