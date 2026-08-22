package web

import (
	"context"
	"fmt"
	"net/http"
	neturl "net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nagiflow/nagipath/internal/probe"
	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
)

// ---------------------------------------------------------------- first run

// getSetup is reachable only while the database has no users. Once one exists it
// is closed permanently, so it cannot become a way to add an admin later.
func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) {
	if n, _ := s.DB.UserCount(r.Context()); n > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.render(w, r, "setup.html", "Set up nagipath", nil)
}

func (s *Server) postSetup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if n, _ := s.DB.UserCount(ctx); n > 0 {
		http.Error(w, "already set up", http.StatusForbidden)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || len(password) < 12 {
		redirect(w, r, "/setup", "", "a username and a password of at least 12 characters are required")
		return
	}
	if password != r.FormValue("confirm") {
		redirect(w, r, "/setup", "", "the two passwords do not match")
		return
	}
	id, err := s.DB.CreateUser(ctx, username, password, "admin", username, false)
	if err != nil {
		redirect(w, r, "/setup", "", err.Error())
		return
	}
	s.DB.Audit(ctx, &id, "user.create", "user", &id, username)
	token, err := s.DB.NewSession(ctx, id, r.UserAgent(), remoteAddr(r))
	if err != nil {
		redirect(w, r, "/login", "", err.Error())
		return
	}
	s.setCookie(w, token)
	redirect(w, r, "/nodes", "welcome — add your first node", "")
}

// ---------------------------------------------------------------- login

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) {
	if n, _ := s.DB.UserCount(r.Context()); n == 0 {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	s.render(w, r, "login.html", "Sign in", nil)
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	username := strings.TrimSpace(r.FormValue("username"))
	// Checked before Authenticate does any argon2id work: the point of a rate
	// limit is refusing a guess before paying for it, not after — the same
	// ordering probe's rate limit uses before a Probe's request goes out.
	if limited, err := s.DB.LoginRateLimited(ctx, username); err != nil {
		redirect(w, r, "/login", "", err.Error())
		return
	} else if limited {
		s.DB.AuditDetail(ctx, nil, "auth.login", "user", nil, username, nil, "denied", remoteAddr(r))
		redirect(w, r, "/login", "", "too many failed attempts for this account; try again later")
		return
	}
	u, err := s.DB.Authenticate(ctx, username, r.FormValue("password"))
	if err != nil {
		// The message is deliberately identical for a bad password and a missing
		// user: which one it was is not the operator's business to learn here.
		s.DB.AuditDetail(ctx, nil, "auth.login", "user", nil, username, nil, "failure", remoteAddr(r))
		redirect(w, r, "/login", "", "invalid username or password")
		return
	}
	token, err := s.DB.NewSession(ctx, u.ID, r.UserAgent(), remoteAddr(r))
	if err != nil {
		redirect(w, r, "/login", "", err.Error())
		return
	}
	s.DB.AuditDetail(ctx, &u.ID, "auth.login", "user", &u.ID, u.Username, nil, "success", remoteAddr(r))
	s.setCookie(w, token)
	next := r.FormValue("next")
	if next == "" || !strings.HasPrefix(next, "/") {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) postLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		// The same CSRF check every other mutating route gets via auth(). Checked
		// inline rather than wrapping in auth() itself, because auth() also
		// redirects a must-change-password user to /password before they ever
		// reach here — exactly the user who most needs a working sign-out button.
		if !s.checkCSRF(r, c.Value) {
			http.Error(w, "invalid or missing CSRF token", http.StatusForbidden)
			return
		}
		s.DB.EndSession(r.Context(), c.Value)
	}
	s.clearCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) getPassword(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "password.html", "Change password", nil)
}

func (s *Server) postPassword(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	next := r.FormValue("new")
	if len(next) < 12 {
		redirect(w, r, "/password", "", "the new password must be at least 12 characters")
		return
	}
	if next != r.FormValue("confirm") {
		redirect(w, r, "/password", "", "the two passwords do not match")
		return
	}
	// Requiring the current password stops a borrowed logged-in browser from
	// locking the real owner out.
	if !u.MustChangePassword {
		if _, err := s.DB.Authenticate(ctx, u.Username, r.FormValue("current")); err != nil {
			redirect(w, r, "/password", "", "the current password is not correct")
			return
		}
	}
	if err := s.DB.SetPassword(ctx, u.ID, next); err != nil {
		redirect(w, r, "/password", "", err.Error())
		return
	}
	s.DB.Audit(ctx, &u.ID, "user.password_change", "user", &u.ID, u.Username)
	if err := s.DB.EndAllSessions(ctx, u.ID); err != nil {
		redirect(w, r, "/login", "", err.Error())
		return
	}
	token, err := s.DB.NewSession(ctx, u.ID, r.UserAgent(), remoteAddr(r))
	if err != nil {
		redirect(w, r, "/login", "", err.Error())
		return
	}
	s.setCookie(w, token)
	redirect(w, r, "/", "password changed; other sessions were signed out", "")
}

// ---------------------------------------------------------------- dashboard

// activityBucket is one column of the collection-activity strip: a window of the
// last day and what happened in it. There is no chart library and no axis —
// the count is printed beside the shape.
type activityBucket struct {
	Label  string
	Total  int
	Failed int
}

// attention is one row of "needs attention". Kind is the badge word, and every
// row has a link, because a finding an operator cannot open is noise.
type attention struct {
	Kind string
	Text string
	Note string
	Link string
}

type dashboardData struct {
	Nodes     int
	Instances int
	Vendors   map[string]int
	Degraded  int
	Pending   int
	// Instance states, so "12 instances" can say how many are actually usable.
	OK, Unparsed, PendingInstances int
	Certs30                        int
	Drifted                        int

	Failing     []store.Node
	Collections []store.Collection
	Expiring    []store.CertificateView
	Activity    []activityBucket
	ActivityMax int
	Attention   []attention
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := dashboardData{Vendors: map[string]int{}}

	nodes, err := s.DB.Nodes(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	d.Nodes = len(nodes)
	for _, n := range nodes {
		if n.ConsecutiveFailures > 0 {
			d.Failing = append(d.Failing, n)
			d.Attention = append(d.Attention, attention{"DEG", n.DisplayName,
				fmt.Sprintf("%d collections failed in a row", n.ConsecutiveFailures),
				fmt.Sprintf("/nodes/%d", n.ID)})
		}
	}
	instances, err := s.DB.Instances(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	d.Instances = len(instances)
	for _, in := range instances {
		d.Vendors[in.Vendor]++
		switch in.State() {
		case "ok":
			d.OK++
		case "pending":
			d.PendingInstances++
		case "unparsed":
			d.Unparsed++
			d.Attention = append(d.Attention, attention{"DEG", in.DisplayName,
				"parse " + in.ParseState + " — traces refuse to continue through it",
				fmt.Sprintf("/instances/%d", in.ID)})
		case "degraded":
			d.Degraded++
			d.Attention = append(d.Attention, attention{"DEG", in.DisplayName,
				"the current snapshot is incomplete",
				fmt.Sprintf("/instances/%d", in.ID)})
		}
		// An instance with no derivable access log can never be raised past
		// observed_effect, which is a log-format gap and not a fault.
		if len(in.AccessLogPaths) == 0 && in.State() != "pending" {
			d.Attention = append(d.Attention, attention{"LOGFMT", in.DisplayName,
				"no access log path is derivable, so a probe cannot verify it",
				fmt.Sprintf("/instances/%d", in.ID)})
		}
	}
	d.Degraded += d.Unparsed
	d.Pending = s.DB.PendingHostKeyCount(ctx)
	if d.Pending > 0 {
		d.Attention = append(d.Attention, attention{"HOSTKEY",
			fmt.Sprintf("%d host key(s) waiting", d.Pending),
			"nothing is collected from those hosts until a person approves them",
			"/onboarding"})
	}
	d.Collections, _ = s.DB.Collections(ctx, 200)
	d.Activity, d.ActivityMax = activity(d.Collections)
	if len(d.Collections) > 5 {
		d.Collections = d.Collections[:5]
	}

	// Drift is counted per instance, not per finding: "three hosts diverge" is the
	// decision, and "forty-one differences" is the reading afterwards.
	if runs, err := s.DB.DriftRuns(ctx, 0, ""); err == nil {
		seen := map[int64]bool{}
		for _, run := range runs {
			if run.FindingCount > 0 && !seen[run.InstanceID] {
				seen[run.InstanceID] = true
				d.Drifted++
				d.Attention = append(d.Attention, attention{"DRIFT", run.InstanceName,
					fmt.Sprintf("%d divergence(s) from the %s", run.FindingCount, run.BaselineLabel),
					"/drift"})
			}
		}
	}

	// Certificates within 30 days, because that is the window in which an operator
	// can still do something about it.
	certs, _ := s.DB.Certificates(ctx)
	cutoff := time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02T15:04:05Z")
	for _, c := range certs {
		if c.NotAfter != "" && c.NotAfter <= cutoff {
			d.Expiring = append(d.Expiring, c)
			d.Certs30++
			d.Attention = append(d.Attention, attention{"CERT",
				orText(c.SubjectCN, "(no CN)"), expiry(c.NotAfter),
				fmt.Sprintf("/certificates?cert=%d", c.ID)})
		}
	}
	s.render(w, r, "dashboard.html", "Overview", d)
}

func orText(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// activity buckets the last day of collections into eight three-hour windows.
//
// ponytail: eight fixed buckets, computed in Go over the rows the dashboard has
// already read. A GROUP BY would be the upgrade when the window becomes a
// control rather than the fixed 24h the wireframe asks for.
func activity(list []store.Collection) ([]activityBucket, int) {
	const buckets, span = 8, 3 * time.Hour
	// The window ends now, not at the top of the hour: the last bucket has to be the
	// one a collection a minute ago lands in, or the busiest bar is always empty.
	now := time.Now().UTC()
	oldest := now.Add(-buckets * span)
	out := make([]activityBucket, buckets)
	for i := range out {
		out[i].Label = oldest.Add(time.Duration(i) * span).Truncate(time.Hour).Format("15:04")
	}
	max := 0
	for _, c := range list {
		t, err := time.Parse("2006-01-02T15:04:05Z", c.StartedAt)
		if err != nil || t.Before(oldest) {
			continue
		}
		i := int(t.Sub(oldest) / span)
		if i >= buckets {
			// A target clock running ahead still belongs at the right edge.
			i = buckets - 1
		}
		out[i].Total++
		if c.Status == "failed" {
			out[i].Failed++
		}
		if out[i].Total > max {
			max = out[i].Total
		}
	}
	return out, max
}

// ---------------------------------------------------------------- nodes

type nodesData struct {
	Nodes       []store.Node
	Credentials []store.Credential
}

func (s *Server) nodes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nodes, err := s.DB.Nodes(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	creds, _ := s.DB.Credentials(ctx)
	s.render(w, r, "nodes.html", "Nodes", nodesData{Nodes: nodes, Credentials: creds})
}

func (s *Server) addNode(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	address := strings.TrimSpace(r.FormValue("address"))
	if address == "" {
		redirect(w, r, "/nodes", "", "an address is required")
		return
	}
	// nagipath never discovers hosts by scanning. A range here would be a scan, so
	// it is refused outright rather than helpfully expanded (ADR-0003).
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

type nodeData struct {
	Node        store.Node
	HostKeys    []store.HostKey
	Instances   []store.Instance
	Collections []store.Collection
	Running     bool
}

func (s *Server) node(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := idOf(r, "id")
	n, err := s.DB.Node(ctx, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d := nodeData{Node: n}
	d.HostKeys, _ = s.DB.HostKeys(ctx, id)
	all, _ := s.DB.Instances(ctx)
	for _, in := range all {
		if in.NodeID == id {
			d.Instances = append(d.Instances, in)
		}
	}
	cols, _ := s.DB.Collections(ctx, 50)
	for _, c := range cols {
		if c.NodeID == id {
			d.Collections = append(d.Collections, c)
			if c.Status == "running" {
				d.Running = true
			}
		}
	}
	s.render(w, r, "node.html", n.DisplayName, d)
}

// collectNode starts a collection in the background. A collection can take tens of
// seconds on a slow link, and holding the request open would make the UI look
// broken; the Collection row is the progress indicator.
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

// decideHostKey is the gate: nothing runs on a Node until an operator accepts its
// host key by hand. There is no trust-on-first-use path anywhere in the product.
func (s *Server) decideHostKey(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	id := idOf(r, "id")
	approve := r.FormValue("decision") == "approve"
	if err := s.DB.DecideHostKey(ctx, id, approve, &u.ID); err != nil {
		redirect(w, r, "/nodes", "", err.Error())
		return
	}
	back := r.FormValue("back")
	if !strings.HasPrefix(back, "/") {
		back = "/nodes"
	}
	if approve {
		redirect(w, r, back, "host key approved", "")
		return
	}
	redirect(w, r, back, "host key rejected", "")
}

// ---------------------------------------------------------------- credentials

func (s *Server) credentials(w http.ResponseWriter, r *http.Request) {
	creds, err := s.DB.Credentials(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, r, "credentials.html", "Credentials", creds)
}

func (s *Server) addCredential(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	name := strings.TrimSpace(r.FormValue("name"))
	key := r.FormValue("private_key")
	if name == "" || strings.TrimSpace(key) == "" {
		redirect(w, r, "/credentials", "", "a name and a private key are required")
		return
	}
	kind := "private_key"
	if strings.TrimSpace(r.FormValue("certificate")) != "" {
		kind = "ssh_certificate"
	}
	_, err := s.DB.CreateCredential(ctx, s.Master, name,
		strings.TrimSpace(r.FormValue("username")), kind, key,
		r.FormValue("passphrase"), r.FormValue("certificate"), &u.ID)
	if err != nil {
		redirect(w, r, "/credentials", "", err.Error())
		return
	}
	// The key is now encrypted at rest and is never rendered again, by any route.
	redirect(w, r, "/credentials", "credential stored", "")
}

// ---------------------------------------------------------------- users

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.Users(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, r, "users.html", "Users", list)
}

func (s *Server) addUser(w http.ResponseWriter, r *http.Request) {
	ctx, actor := r.Context(), userOf(r)
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || len(password) < 12 {
		redirect(w, r, "/users", "", "a username and a password of at least 12 characters are required")
		return
	}
	if password != r.FormValue("confirm") {
		redirect(w, r, "/users", "", "the two passwords do not match")
		return
	}
	role := r.FormValue("role")
	if role != "admin" && role != "viewer" {
		redirect(w, r, "/users", "", "role must be admin or viewer")
		return
	}
	mustChange := r.FormValue("must_change") != ""
	id, err := s.DB.CreateUser(ctx, username, password, role, username, mustChange)
	if err != nil {
		redirect(w, r, "/users", "", err.Error())
		return
	}
	s.DB.Audit(ctx, &actor.ID, "user.create", "user", &id, username)
	redirect(w, r, "/users", "user created", "")
}

// disableUser refuses to leave the product with zero enabled admins — the same
// class of structural guardrail as Probe's GET/HEAD-only, enforced here rather
// than only by hiding the button, because a button that is not rendered is not
// a guarantee.
func (s *Server) disableUser(w http.ResponseWriter, r *http.Request) {
	ctx, actor := r.Context(), userOf(r)
	id := idOf(r, "id")
	target, err := s.DB.User(ctx, id)
	if err != nil {
		redirect(w, r, "/users", "", "user not found")
		return
	}
	if target.IsAdmin() && !target.Disabled {
		n, err := s.DB.EnabledAdminCount(ctx, id)
		if err != nil {
			redirect(w, r, "/users", "", err.Error())
			return
		}
		if n == 0 {
			redirect(w, r, "/users", "", "cannot disable the last enabled admin account")
			return
		}
	}
	if err := s.DB.SetUserDisabled(ctx, id, true); err != nil {
		redirect(w, r, "/users", "", err.Error())
		return
	}
	s.DB.Audit(ctx, &actor.ID, "user.disable", "user", &id, target.Username)
	redirect(w, r, "/users", "user disabled", "")
}

func (s *Server) enableUser(w http.ResponseWriter, r *http.Request) {
	ctx, actor := r.Context(), userOf(r)
	id := idOf(r, "id")
	target, err := s.DB.User(ctx, id)
	if err != nil {
		redirect(w, r, "/users", "", "user not found")
		return
	}
	if err := s.DB.SetUserDisabled(ctx, id, false); err != nil {
		redirect(w, r, "/users", "", err.Error())
		return
	}
	s.DB.Audit(ctx, &actor.ID, "user.enable", "user", &id, target.Username)
	redirect(w, r, "/users", "user enabled", "")
}

// ---------------------------------------------------------------- inventory

func (s *Server) instances(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.Instances(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, r, "instances.html", "Instances", list)
}

type instanceData struct {
	Instance  store.Instance
	Snapshot  store.Snapshot
	Snapshots []store.Snapshot
	Files     []store.FileRef
	Inst      *trace.Inst
	// Certs maps a hostname to the certificate serving it, so the sites table can
	// carry an expiry beside the name rather than on a separate screen.
	Certs map[string]store.CertificateView
	Rules int
	// Dump is the vendor's own effective configuration, if this snapshot has one.
	Dump *store.FileRef
	// Drift is this instance's newest comparison per baseline, with its findings.
	Drift    []store.DriftRun
	Findings []store.DriftFinding
	Cluster  string
}

func (s *Server) instance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := idOf(r, "id")
	in, err := s.DB.Instance(ctx, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d := instanceData{Instance: in, Certs: map[string]store.CertificateView{}}
	d.Snapshots, _ = s.DB.Snapshots(ctx, id, 20)
	snap, err := s.DB.CurrentSnapshot(ctx, id)
	if err == nil {
		d.Snapshot = snap
		d.Files, _ = s.DB.SnapshotFiles(ctx, snap.ID)
		for i := range d.Files {
			if d.Files[i].Kind == "vendor_dump" {
				d.Dump = &d.Files[i]
				break
			}
		}
		// Reusing the trace loader gives the detail page the same view of the
		// topology the trace engine uses, so the two can never disagree. This page
		// only ever shows one Instance, so it loads just that one rather than
		// paying for the whole fleet the way /trace and rule lookup have to.
		if inst, err := trace.LoadInstance(ctx, s.DB, id); err == nil {
			d.Inst = inst
			if d.Inst != nil {
				d.Rules = len(d.Inst.GlobalRules)
				for _, site := range d.Inst.Sites {
					d.Rules += len(site.Rules) + countRouteRules(site.Routes)
				}
			}
		}
	}
	if certs, err := s.DB.Certificates(ctx); err == nil {
		for _, c := range certs {
			for _, name := range c.Serves {
				// Soonest expiry wins: the name fails on the first certificate to go.
				if have, ok := d.Certs[name]; !ok || c.NotAfter < have.NotAfter {
					d.Certs[name] = c
				}
			}
		}
	}
	if runs, err := s.DB.DriftRuns(ctx, 0, ""); err == nil {
		var ids []int64
		for _, run := range runs {
			if run.InstanceID == id {
				d.Drift = append(d.Drift, run)
				ids = append(ids, run.ID)
			}
		}
		d.Findings, _ = s.DB.DriftFindings(ctx, ids)
	}
	if cl, err := s.DB.Clusters(ctx); err == nil && in.ClusterID.Valid {
		for _, c := range cl {
			if c.ID == in.ClusterID.Int64 {
				d.Cluster = c.Name
			}
		}
	}
	s.render(w, r, "instance.html", in.DisplayName, d)
}

// countRouteRules walks nested locations, because a rule in a nested block is
// still a rule this instance applies.
func countRouteRules(routes []*trace.Route) int {
	n := 0
	for _, r := range routes {
		n += len(r.Rules) + countRouteRules(r.Children)
	}
	return n
}

type fileData struct {
	Snapshot store.Snapshot
	File     store.FileRef
	Body     string
}

func (s *Server) snapshotFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	snap, err := s.DB.SnapshotByID(ctx, idOf(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	files, err := s.DB.SnapshotFiles(ctx, snap.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	wanted := idOf(r, "fileID")
	for _, f := range files {
		if f.ID != wanted {
			continue
		}
		body, err := s.DB.Blob(ctx, f.Digest)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		s.render(w, r, "file.html", f.Path, fileData{Snapshot: snap, File: f, Body: string(body)})
		return
	}
	http.NotFound(w, r)
}

func (s *Server) collections(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.Collections(r.Context(), 100)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, r, "collections.html", "Collections", list)
}

type certData struct {
	List []store.CertificateView
	// Sel is the certificate the right-hand panel is describing. Selection is a
	// query parameter rather than script, so a link can point at one certificate.
	Sel      *store.CertificateView
	Bindings []store.CertBinding
	// Trace is the entry point the panel's "trace affected entry points" link
	// prefills, derived from the first name the certificate actually serves.
	Trace string
}

func (s *Server) certificates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.DB.Certificates(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	d := certData{List: list}
	want, _ := strconv.ParseInt(r.URL.Query().Get("cert"), 10, 64)
	for i := range list {
		if list[i].ID == want {
			d.Sel = &list[i]
		}
	}
	if d.Sel != nil {
		d.Bindings, _ = s.DB.CertBindings(ctx, d.Sel.ID)
		if len(d.Sel.Serves) > 0 {
			d.Trace = "/trace?scheme=https&port=443&path=/&hostname=" +
				neturl.QueryEscape(d.Sel.Serves[0])
		}
	}
	s.render(w, r, "certificates.html", "Certificates", d)
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.AuditEvents(r.Context(), 200)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, r, "audit.html", "Audit log", list)
}

// ---------------------------------------------------------------- search

// searchFacet is one narrowing option and how many hits it would keep. The count
// is shown because a facet with no number is a guess about what filtering costs.
type searchFacet struct {
	Value string
	Count int
}

// searchGroup is the unit of the results column: one file of one instance, with
// every hit in it. Grouping is the point — twenty hits in one file is one finding.
type searchGroup struct {
	InstanceID int64
	Instance   string
	Vendor     string
	SnapshotID int64
	FileID     int64
	Path       string
	Rules      []store.RuleHit
	Texts      []store.TextHit
}

const searchPageSize = 5

type searchData struct {
	Query   string
	Rules   []store.RuleHit
	Configs []store.TextHit
	Groups  []searchGroup
	// Facets are computed over the unfiltered hits, so ticking one never makes the
	// others vanish and an operator can always widen again.
	Vendors  []searchFacet
	Files    []searchFacet
	Clusters []searchFacet
	SelVend  []string
	SelFile  []string
	SelClust []string
	Matches  int
	// Instances is the total this query touched; From/To page over it.
	Instances             int
	From, To, Page, Pages int
	Err                   string
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	d := searchData{
		Query:    strings.TrimSpace(q.Get("q")),
		SelVend:  nonEmpty(q["vendor"]),
		SelFile:  nonEmpty(q["file"]),
		SelClust: nonEmpty(q["cluster"]),
	}
	if d.Query == "" {
		s.render(w, r, "search.html", "Search", d)
		return
	}
	rules, err := s.DB.SearchRules(ctx, d.Query, 200)
	if err != nil {
		d.Err = "rule search failed: " + err.Error()
	}
	texts, _ := s.DB.SearchConfigText(ctx, d.Query, 100)

	vend, file, clust := map[string]int{}, map[string]int{}, map[string]int{}
	count := func(vendor, path, cluster string) {
		vend[vendor]++
		file[path]++
		if cluster == "" {
			cluster = "(no cluster)"
		}
		clust[cluster]++
	}
	for _, h := range rules {
		count(h.Vendor, h.Path, h.Cluster)
	}
	for _, h := range texts {
		count(h.Vendor, h.Path, h.Cluster)
	}
	d.Vendors, d.Files, d.Clusters = facetList(vend), facetList(file), facetList(clust)

	keep := func(vendor, path, cluster string) bool {
		if cluster == "" {
			cluster = "(no cluster)"
		}
		return matchesFacet(d.SelVend, vendor) && matchesFacet(d.SelFile, path) &&
			matchesFacet(d.SelClust, cluster)
	}
	// Group by (instance, file) in rank order, so the first box is the best hit.
	index := map[string]int{}
	for _, h := range rules {
		if !keep(h.Vendor, h.Path, h.Cluster) {
			continue
		}
		d.Rules = append(d.Rules, h)
		i := groupFor(&d.Groups, index, searchGroup{InstanceID: h.InstanceID,
			Instance: h.Instance, Vendor: h.Vendor, SnapshotID: h.SnapshotID,
			FileID: h.FileID, Path: h.Path})
		d.Groups[i].Rules = append(d.Groups[i].Rules, h)
	}
	for _, h := range texts {
		if !keep(h.Vendor, h.Path, h.Cluster) {
			continue
		}
		d.Configs = append(d.Configs, h)
		i := groupFor(&d.Groups, index, searchGroup{InstanceID: h.InstanceID,
			Instance: h.Instance, Vendor: h.Vendor, SnapshotID: h.SnapshotID,
			FileID: h.FileID, Path: h.Path})
		d.Groups[i].Texts = append(d.Groups[i].Texts, h)
	}
	d.Matches = len(d.Rules) + len(d.Configs)

	// Paging is by instance, not by hit: an instance split across two pages would
	// make "17 instances mention this" unreadable.
	var order []int64
	seen := map[int64]bool{}
	for _, g := range d.Groups {
		if !seen[g.InstanceID] {
			seen[g.InstanceID] = true
			order = append(order, g.InstanceID)
		}
	}
	d.Instances = len(order)
	d.Page, _ = strconv.Atoi(q.Get("page"))
	if d.Page < 1 {
		d.Page = 1
	}
	d.Pages = max((d.Instances+searchPageSize-1)/searchPageSize, 1)
	if d.Page > d.Pages {
		d.Page = d.Pages
	}
	start := (d.Page - 1) * searchPageSize
	end := min(start+searchPageSize, d.Instances)
	if d.Instances > 0 {
		d.From, d.To = start+1, end
	}
	onPage := map[int64]bool{}
	for _, id := range order[start:end] {
		onPage[id] = true
	}
	kept := d.Groups[:0]
	for _, g := range d.Groups {
		if onPage[g.InstanceID] {
			kept = append(kept, g)
		}
	}
	d.Groups = kept
	s.render(w, r, "search.html", "Search", d)
}

// groupFor returns the index of the (instance, file) group, appending it in
// first-seen order so rank survives the grouping.
func groupFor(groups *[]searchGroup, index map[string]int, g searchGroup) int {
	key := fmt.Sprintf("%d:%d:%s", g.InstanceID, g.FileID, g.Path)
	if i, ok := index[key]; ok {
		return i
	}
	*groups = append(*groups, g)
	index[key] = len(*groups) - 1
	return len(*groups) - 1
}

func facetList(counts map[string]int) []searchFacet {
	out := make([]searchFacet, 0, len(counts))
	for v, n := range counts {
		out = append(out, searchFacet{v, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Value < out[j].Value
	})
	return out
}

// matchesFacet treats an empty selection as "everything". Nothing ticked is not
// the same as every box ticked, and the screen says so in words.
func matchesFacet(selected []string, value string) bool {
	if len(selected) == 0 {
		return true
	}
	return has(selected, value)
}

// ---------------------------------------------------------------- trace

type traceData struct {
	Query  trace.Query
	Method string
	Result *trace.Trace
	Empty  bool
	// Run is the live Probe this page is following, if the operator started one.
	Run *runView
	// Last is the probe most recently sent at this URL, read back with its evidence.
	// A probe has no screen of its own: it is evidence about one request, so it reads
	// beside that request and nowhere else.
	Last *probe.Past
	// Probes is every probe sent at this URL, for the collapsed list under the graph.
	Probes []probe.Record
	// Gaps are the hops whose log format cannot identify a request. Recomputed rather
	// than stored with a probe: it is a fact about the configuration, and it was already
	// true before anyone sent a request.
	Gaps []probe.Gap
	// URL is what goes back in the one input the form has: the canonical form of
	// whatever was asked for, however it arrived.
	URL string
	// Recent is every stored Trace, newest first, shown before anything has been
	// asked for on this visit — the trace was stored last time, so the operator
	// should not have to retype the URL to see it again.
	Recent []trace.Summary
}

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

func (s *Server) trace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := traceData{Method: "GET"}

	get := r.URL.Query().Get
	if r.Method == http.MethodPost {
		get = r.FormValue
	}
	if m := strings.ToUpper(strings.TrimSpace(get("method"))); m == "HEAD" {
		d.Method = m
	}
	// The form posts one pasted URL. The separate scheme/hostname/path/port
	// parameters still work unchanged, because every deep link in the app — the
	// certificate panel, drift, the recent table — builds a trace link that way.
	q, err := parseTarget(get("url"))
	if err != nil {
		redirect(w, r, "/trace", "", err.Error())
		return
	}
	if q.Hostname == "" {
		q = trace.Query{
			Scheme:   get("scheme"),
			Hostname: strings.TrimSpace(get("hostname")),
			Path:     strings.TrimSpace(get("path")),
		}
		if p, err := strconv.Atoi(get("port")); err == nil {
			q.Port = p
		}
	}
	if q.Hostname == "" {
		count, _ := s.DB.Instances(ctx)
		d.Empty = len(count) == 0
		d.Recent, _ = trace.Recent(ctx, s.DB, 20)
		s.render(w, r, "trace.html", "Trace a request", d)
		return
	}

	top, err := trace.Load(ctx, s.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	d.Result = trace.Walk(top, q)
	d.Query = d.Result.Query
	d.URL = targetURL(d.Query)
	d.Last, _ = probe.Last(ctx, s.DB, d.URL)
	d.Probes, _ = probe.Recent(ctx, s.DB, d.URL, 20)
	if d.Last != nil {
		pr := &probe.Prober{DB: s.DB}
		for _, h := range d.Result.Hops {
			if h.IsExternal || h.Inst == nil {
				continue
			}
			if g := pr.LogFormatGap(ctx, h.Inst.ID, instLabel(h.Inst), h.Inst.Vendor); g != nil {
				d.Gaps = append(d.Gaps, *g)
			}
		}
	}
	runID, _ := strconv.ParseInt(get("run"), 10, 64)
	d.Run = s.probeRunView(runID)

	// Only a POST stores the Trace. A shared link that recomputes on every view
	// would otherwise fill the table with duplicates.
	if r.Method == http.MethodPost {
		u := userOf(r)
		if id, err := trace.Save(ctx, s.DB, d.Result, nil); err != nil {
			s.Log.Warn("trace not stored", "err", err)
		} else {
			// The stored id is what a Probe attaches its evidence to.
			d.Result.ID = id
		}
		s.DB.Audit(ctx, &u.ID, "trace.compute", "trace", nil,
			q.Scheme+"://"+q.Hostname+q.Path)
	}
	s.render(w, r, "trace.html", "Trace "+q.Hostname+q.Path, d)
}
