// Package trace answers "where does a request for this URL actually go?".
//
// The engine reads only the database — the current parsed Snapshot per Instance —
// and never touches the network. That is what makes a Trace fast, reproducible
// from checked-in fixtures, and safe to run against a production fleet.
//
// Two rules from trace.md shape every decision here:
//
//   - Every claim carries its confidence. Nothing in this package can produce
//     `verified`; only Probe evidence can.
//   - Every Trace states why it stopped. There is no `unknown` terminal reason.
package trace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"strings"

	"github.com/nagiflow/nagipath/internal/store"
)

type Listener struct {
	ID        int64
	Address   string
	Port      int
	TLS       bool
	Protocol  string
	IsDefault bool
}

type Name struct {
	Name      string
	MatchKind string
}

type Site struct {
	ID           int64
	InstanceID   int64
	ListenerIDs  []int64
	PrimaryName  string
	Kind         string
	DocumentRoot string
	Names        []Name
	Rules        []*Rule
	Routes       []*Route
}

type Route struct {
	ID             int64
	SiteID         int64
	ParentID       sql.NullInt64
	MatchType      string
	Pattern        string
	PrecedenceRank int
	Specificity    int
	Ordinal        int
	UpstreamID     sql.NullInt64
	TargetRaw      string
	IsTerminal     bool
	Rules          []*Rule
	Children       []*Route
}

type Upstream struct {
	ID            int64
	Name          string
	Kind          string
	BalanceMethod string
	Members       []*Member
}

type Member struct {
	ID     int64
	Host   string
	Port   int
	Scheme string
	Flags  string
}

type Rule struct {
	ID          int64
	ScopeKind   string
	ScopeID     sql.NullInt64
	Directive   string
	ActionClass string
	Args        string
	Raw         string
	Shadowed    bool
	ShadowedBy  string
	Path        string
	ByteStart   int
	// FileID and SnapshotID are the provenance link: every claim on screen has to
	// be one click from the file and offset it came from.
	FileID     int64
	SnapshotID int64
}

// Inst is one Instance plus everything its current Snapshot derived.
type Inst struct {
	ID          int64
	NodeID      int64
	NodeName    string
	NodeAddress string
	Vendor      string
	DisplayName string
	SnapshotID  int64
	// ClusterName is empty when the Instance is in no cluster. The graph groups a
	// rank by it: members of one cluster run byte-identical configuration, so they
	// are the same answer on two hosts rather than two answers.
	ClusterID      int64
	ClusterName    string
	Degraded       bool
	DegradedReason string
	ParseState     string
	Listeners      map[int64]*Listener
	Sites          []*Site
	Upstreams      map[int64]*Upstream
	GlobalRules    []*Rule
}

// Topology is the whole fleet at one moment: every Instance's current Snapshot,
// plus the DNS answers collected on the Nodes themselves.
type Topology struct {
	Instances []*Inst
	byID      map[int64]*Inst
	// byAddress maps an address or hostname a Node is known by to its Instances.
	byAddress map[string][]*Inst
	// DNS is keyed by node ID then name: the resolution that is ever valid for a
	// Hop is the one performed on the specific Node that holds the Upstream
	// referencing that name (ADR-0010), never another Node's answer for the same
	// name. See store.ResolvedNames for why this cannot be collapsed to a single
	// map[string][]string.
	DNS map[int64]map[string][]string
}

func (t *Topology) Instance(id int64) *Inst { return t.byID[id] }

// Load reads the current parsed topology. Instances whose Snapshot failed to
// parse are loaded but marked: the walk refuses to continue through them rather
// than pretending they route nothing (trace.md §7).
func Load(ctx context.Context, db *store.DB) (*Topology, error) {
	t := &Topology{byID: map[int64]*Inst{}, byAddress: map[string][]*Inst{}}

	rows, err := db.R.QueryContext(ctx, `SELECT i.id, i.node_id, n.display_name, n.address,
		i.vendor, i.display_name, s.id, s.degraded, s.degraded_reason, s.parse_state,
		COALESCE(i.cluster_id, 0), COALESCE(c.name, '')
		FROM instance i
		JOIN node n ON n.id = i.node_id
		JOIN snapshot s ON s.instance_id = i.id AND s.is_current = 1
		LEFT JOIN cluster c ON c.id = i.cluster_id
		WHERE i.retired_at IS NULL`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		in := &Inst{Listeners: map[int64]*Listener{}, Upstreams: map[int64]*Upstream{}}
		if err := rows.Scan(&in.ID, &in.NodeID, &in.NodeName, &in.NodeAddress, &in.Vendor,
			&in.DisplayName, &in.SnapshotID, &in.Degraded, &in.DegradedReason,
			&in.ParseState, &in.ClusterID, &in.ClusterName); err != nil {
			rows.Close()
			return nil, err
		}
		t.Instances = append(t.Instances, in)
		t.byID[in.ID] = in
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(t.Instances) == 0 {
		return t, nil
	}

	// bySnapshot is the fan-out key for every batched query below: each of listener,
	// rule, upstream, upstream_member, route, site and site_name is scoped to one
	// Instance's current Snapshot, and a snapshot_id column on the row is enough to
	// route it back to the right Inst without a second lookup table.
	snapshotIDs := make([]int64, len(t.Instances))
	bySnapshot := make(map[int64]*Inst, len(t.Instances))
	for i, in := range t.Instances {
		for _, key := range []string{in.NodeAddress, in.NodeName, hostOf(in.NodeName)} {
			if key != "" {
				t.byAddress[strings.ToLower(key)] = append(t.byAddress[strings.ToLower(key)], in)
			}
		}
		snapshotIDs[i] = in.SnapshotID
		bySnapshot[in.SnapshotID] = in
	}
	if err := loadFleet(ctx, db, bySnapshot, snapshotIDs); err != nil {
		return nil, err
	}
	if t.DNS, err = db.ResolvedNames(ctx); err != nil {
		return nil, err
	}
	return t, nil
}

// LoadInstance reads the current parsed topology for exactly one Instance. It is
// Load's per-Instance cost without the rest of the fleet's, for the pages that only
// ever show one Instance (the detail page): the fleet-wide seven queries Load
// batches across every Instance would otherwise still run seven times, once, for
// data the caller cannot use. A missing or retired Instance is not an error: it
// reports as (nil, nil), the same as top.Instance(id) on a Topology that never
// loaded it.
func LoadInstance(ctx context.Context, db *store.DB, instanceID int64) (*Inst, error) {
	in := &Inst{Listeners: map[int64]*Listener{}, Upstreams: map[int64]*Upstream{}}
	err := db.R.QueryRowContext(ctx, `SELECT i.id, i.node_id, n.display_name, n.address,
		i.vendor, i.display_name, s.id, s.degraded, s.degraded_reason, s.parse_state,
		COALESCE(i.cluster_id, 0), COALESCE(c.name, '')
		FROM instance i
		JOIN node n ON n.id = i.node_id
		JOIN snapshot s ON s.instance_id = i.id AND s.is_current = 1
		LEFT JOIN cluster c ON c.id = i.cluster_id
		WHERE i.id = ? AND i.retired_at IS NULL`, instanceID).Scan(
		&in.ID, &in.NodeID, &in.NodeName, &in.NodeAddress, &in.Vendor,
		&in.DisplayName, &in.SnapshotID, &in.Degraded, &in.DegradedReason,
		&in.ParseState, &in.ClusterID, &in.ClusterName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := loadInstance(ctx, db, in); err != nil {
		return nil, err
	}
	return in, nil
}

// inHoles builds the "?,?,...,?" placeholder list and matching args for a
// `column IN (...)` clause, the house pattern for a batched query (see
// store.DriftFindings).
func inHoles(ids []int64) (string, []any) {
	holes := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return holes, args
}

// loadFleet fills in every Inst in bySnapshot with its Listeners, Upstreams,
// Sites and Rules in seven queries total, each scoped by `snapshot_id IN (...)`
// across every Instance at once — the batched replacement for calling
// loadInstance once per Instance, which was seven queries EACH (topology.go
// previously issued 1 + 7N + 1 queries for an N-Instance fleet; this makes it
// 1 + 7 + 1 regardless of N).
func loadFleet(ctx context.Context, db *store.DB, bySnapshot map[int64]*Inst, ids []int64) error {
	holes, args := inHoles(ids)

	rows, err := db.R.QueryContext(ctx, `SELECT id, snapshot_id, address, port, tls, protocol, is_default
		FROM listener WHERE snapshot_id IN (`+holes+`) ORDER BY snapshot_id, ordinal`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		l := &Listener{}
		var snapID int64
		if err := rows.Scan(&l.ID, &snapID, &l.Address, &l.Port, &l.TLS, &l.Protocol, &l.IsDefault); err != nil {
			rows.Close()
			return err
		}
		if in := bySnapshot[snapID]; in != nil {
			in.Listeners[l.ID] = l
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rules, err := loadRulesBatch(ctx, db, holes, args)
	if err != nil {
		return err
	}
	for snapID, in := range bySnapshot {
		in.GlobalRules = rules.globalBySnapshot[snapID]
	}

	upstreamByID := map[int64]*Upstream{}
	ups, err := db.R.QueryContext(ctx, `SELECT id, snapshot_id, name, kind, balance_method
		FROM upstream WHERE snapshot_id IN (`+holes+`) ORDER BY snapshot_id, ordinal`, args...)
	if err != nil {
		return err
	}
	for ups.Next() {
		u := &Upstream{}
		var snapID int64
		if err := ups.Scan(&u.ID, &snapID, &u.Name, &u.Kind, &u.BalanceMethod); err != nil {
			ups.Close()
			return err
		}
		if in := bySnapshot[snapID]; in != nil {
			in.Upstreams[u.ID] = u
		}
		upstreamByID[u.ID] = u
	}
	ups.Close()
	if err := ups.Err(); err != nil {
		return err
	}

	mem, err := db.R.QueryContext(ctx, `SELECT id, upstream_id, host, COALESCE(port, 0),
		scheme, flags FROM upstream_member WHERE snapshot_id IN (`+holes+`) ORDER BY upstream_id, ordinal`,
		args...)
	if err != nil {
		return err
	}
	for mem.Next() {
		var upID int64
		m := &Member{}
		if err := mem.Scan(&m.ID, &upID, &m.Host, &m.Port, &m.Scheme, &m.Flags); err != nil {
			mem.Close()
			return err
		}
		if u := upstreamByID[upID]; u != nil {
			u.Members = append(u.Members, m)
		}
	}
	mem.Close()
	if err := mem.Err(); err != nil {
		return err
	}

	routesBySite := map[int64][]*Route{}
	rt, err := db.R.QueryContext(ctx, `SELECT id, site_id, parent_route_id, match_type, pattern,
		precedence_rank, specificity, ordinal, upstream_id, target_raw, is_terminal
		FROM route WHERE snapshot_id IN (`+holes+`) ORDER BY site_id, id`, args...)
	if err != nil {
		return err
	}
	// ordered keeps the query's order (site, then id) for the same reason
	// loadInstance's single-Instance version does: a map iteration would shuffle
	// sibling routes on every request, and routes are read in evaluation order.
	allRoutes := map[int64]*Route{}
	var ordered []*Route
	for rt.Next() {
		r := &Route{}
		if err := rt.Scan(&r.ID, &r.SiteID, &r.ParentID, &r.MatchType, &r.Pattern,
			&r.PrecedenceRank, &r.Specificity, &r.Ordinal, &r.UpstreamID, &r.TargetRaw,
			&r.IsTerminal); err != nil {
			rt.Close()
			return err
		}
		r.Rules = rules.byRoute[r.ID]
		allRoutes[r.ID] = r
		ordered = append(ordered, r)
	}
	rt.Close()
	if err := rt.Err(); err != nil {
		return err
	}
	for _, r := range ordered {
		if r.ParentID.Valid {
			if parent := allRoutes[r.ParentID.Int64]; parent != nil {
				parent.Children = append(parent.Children, r)
				continue
			}
		}
		routesBySite[r.SiteID] = append(routesBySite[r.SiteID], r)
	}

	siteByID := map[int64]*Site{}
	st, err := db.R.QueryContext(ctx, `SELECT id, snapshot_id, listener_ids, primary_name, kind, document_root
		FROM site WHERE snapshot_id IN (`+holes+`) ORDER BY snapshot_id, ordinal, id`, args...)
	if err != nil {
		return err
	}
	for st.Next() {
		s := &Site{}
		var snapID int64
		var listeners string
		if err := st.Scan(&s.ID, &snapID, &listeners, &s.PrimaryName, &s.Kind, &s.DocumentRoot); err != nil {
			st.Close()
			return err
		}
		in := bySnapshot[snapID]
		if in == nil {
			continue
		}
		s.InstanceID = in.ID
		json.Unmarshal([]byte(listeners), &s.ListenerIDs)
		s.Rules = rules.bySite[s.ID]
		s.Routes = routesBySite[s.ID]
		in.Sites = append(in.Sites, s)
		siteByID[s.ID] = s
	}
	st.Close()
	if err := st.Err(); err != nil {
		return err
	}

	names, err := db.R.QueryContext(ctx, `SELECT sn.site_id, sn.name, sn.match_kind
		FROM site_name sn JOIN site s ON s.id = sn.site_id
		WHERE s.snapshot_id IN (`+holes+`) ORDER BY sn.site_id, sn.ordinal`, args...)
	if err != nil {
		return err
	}
	for names.Next() {
		var siteID int64
		var n Name
		if err := names.Scan(&siteID, &n.Name, &n.MatchKind); err != nil {
			names.Close()
			return err
		}
		if s := siteByID[siteID]; s != nil {
			s.Names = append(s.Names, n)
		}
	}
	names.Close()
	return names.Err()
}

// loadInstance is Load's per-Instance path, kept for LoadInstance: seven queries
// scoped to one Snapshot, the same shape loadFleet batches across the whole fleet.
func loadInstance(ctx context.Context, db *store.DB, in *Inst) error {
	rows, err := db.R.QueryContext(ctx, `SELECT id, address, port, tls, protocol, is_default
		FROM listener WHERE snapshot_id = ? ORDER BY ordinal`, in.SnapshotID)
	if err != nil {
		return err
	}
	for rows.Next() {
		l := &Listener{}
		if err := rows.Scan(&l.ID, &l.Address, &l.Port, &l.TLS, &l.Protocol, &l.IsDefault); err != nil {
			rows.Close()
			return err
		}
		in.Listeners[l.ID] = l
	}
	rows.Close()

	rules, err := loadRules(ctx, db, in.SnapshotID)
	if err != nil {
		return err
	}
	in.GlobalRules = rules.global

	ups, err := db.R.QueryContext(ctx, `SELECT id, name, kind, balance_method
		FROM upstream WHERE snapshot_id = ? ORDER BY ordinal`, in.SnapshotID)
	if err != nil {
		return err
	}
	for ups.Next() {
		u := &Upstream{}
		if err := ups.Scan(&u.ID, &u.Name, &u.Kind, &u.BalanceMethod); err != nil {
			ups.Close()
			return err
		}
		in.Upstreams[u.ID] = u
	}
	ups.Close()

	mem, err := db.R.QueryContext(ctx, `SELECT id, upstream_id, host, COALESCE(port, 0),
		scheme, flags FROM upstream_member WHERE snapshot_id = ? ORDER BY upstream_id, ordinal`,
		in.SnapshotID)
	if err != nil {
		return err
	}
	for mem.Next() {
		var upID int64
		m := &Member{}
		if err := mem.Scan(&m.ID, &upID, &m.Host, &m.Port, &m.Scheme, &m.Flags); err != nil {
			mem.Close()
			return err
		}
		if u := in.Upstreams[upID]; u != nil {
			u.Members = append(u.Members, m)
		}
	}
	mem.Close()

	routesBySite := map[int64][]*Route{}
	rt, err := db.R.QueryContext(ctx, `SELECT id, site_id, parent_route_id, match_type, pattern,
		precedence_rank, specificity, ordinal, upstream_id, target_raw, is_terminal
		FROM route WHERE snapshot_id = ? ORDER BY site_id, id`, in.SnapshotID)
	if err != nil {
		return err
	}
	// ordered keeps the query's order (site, then id) because a map iteration would
	// shuffle sibling routes on every request, and routes are read in evaluation
	// order — a tie between two ranks must not resolve differently per page load.
	allRoutes := map[int64]*Route{}
	var ordered []*Route
	for rt.Next() {
		r := &Route{}
		if err := rt.Scan(&r.ID, &r.SiteID, &r.ParentID, &r.MatchType, &r.Pattern,
			&r.PrecedenceRank, &r.Specificity, &r.Ordinal, &r.UpstreamID, &r.TargetRaw,
			&r.IsTerminal); err != nil {
			rt.Close()
			return err
		}
		r.Rules = rules.byRoute[r.ID]
		allRoutes[r.ID] = r
		ordered = append(ordered, r)
	}
	rt.Close()
	for _, r := range ordered {
		if r.ParentID.Valid {
			if parent := allRoutes[r.ParentID.Int64]; parent != nil {
				parent.Children = append(parent.Children, r)
				continue
			}
		}
		routesBySite[r.SiteID] = append(routesBySite[r.SiteID], r)
	}

	st, err := db.R.QueryContext(ctx, `SELECT id, listener_ids, primary_name, kind, document_root
		FROM site WHERE snapshot_id = ? ORDER BY ordinal, id`, in.SnapshotID)
	if err != nil {
		return err
	}
	for st.Next() {
		s := &Site{InstanceID: in.ID}
		var listeners string
		if err := st.Scan(&s.ID, &listeners, &s.PrimaryName, &s.Kind, &s.DocumentRoot); err != nil {
			st.Close()
			return err
		}
		json.Unmarshal([]byte(listeners), &s.ListenerIDs)
		s.Rules = rules.bySite[s.ID]
		s.Routes = routesBySite[s.ID]
		in.Sites = append(in.Sites, s)
	}
	st.Close()

	names, err := db.R.QueryContext(ctx, `SELECT sn.site_id, sn.name, sn.match_kind
		FROM site_name sn JOIN site s ON s.id = sn.site_id
		WHERE s.snapshot_id = ? ORDER BY sn.ordinal`, in.SnapshotID)
	if err != nil {
		return err
	}
	byID := map[int64]*Site{}
	for _, s := range in.Sites {
		byID[s.ID] = s
	}
	for names.Next() {
		var siteID int64
		var n Name
		if err := names.Scan(&siteID, &n.Name, &n.MatchKind); err != nil {
			names.Close()
			return err
		}
		if s := byID[siteID]; s != nil {
			s.Names = append(s.Names, n)
		}
	}
	names.Close()
	return nil
}

type ruleIndex struct {
	global  []*Rule
	bySite  map[int64][]*Rule
	byRoute map[int64][]*Rule
}

func loadRules(ctx context.Context, db *store.DB, snapshotID int64) (*ruleIndex, error) {
	idx := &ruleIndex{bySite: map[int64][]*Rule{}, byRoute: map[int64][]*Rule{}}
	rows, err := db.R.QueryContext(ctx, `SELECT r.id, r.scope_kind, r.scope_id, r.directive,
		r.action_class, r.args, r.raw_text, r.shadowed, r.shadowed_by, f.path,
		r.prov_byte_start, f.id, r.snapshot_id
		FROM rule r JOIN snapshot_file f ON f.id = r.prov_file_id
		WHERE r.snapshot_id = ? ORDER BY r.id`, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r := &Rule{}
		if err := rows.Scan(&r.ID, &r.ScopeKind, &r.ScopeID, &r.Directive, &r.ActionClass,
			&r.Args, &r.Raw, &r.Shadowed, &r.ShadowedBy, &r.Path, &r.ByteStart,
			&r.FileID, &r.SnapshotID); err != nil {
			return nil, err
		}
		switch {
		case r.ScopeKind == "site" && r.ScopeID.Valid:
			idx.bySite[r.ScopeID.Int64] = append(idx.bySite[r.ScopeID.Int64], r)
		case r.ScopeKind == "route" && r.ScopeID.Valid:
			idx.byRoute[r.ScopeID.Int64] = append(idx.byRoute[r.ScopeID.Int64], r)
		default:
			idx.global = append(idx.global, r)
		}
	}
	return idx, rows.Err()
}

// ruleIndexBatch is loadRules's fleet-wide counterpart: bySite and byRoute stay flat
// maps because a site or route ID is unique across the whole table, but a rule with
// no site or route scope (an Instance-level directive) only carries its Snapshot's
// ID, so those have to stay bucketed per Snapshot to land on the right Inst.
type ruleIndexBatch struct {
	globalBySnapshot map[int64][]*Rule
	bySite           map[int64][]*Rule
	byRoute          map[int64][]*Rule
}

func loadRulesBatch(ctx context.Context, db *store.DB, holes string, args []any) (*ruleIndexBatch, error) {
	idx := &ruleIndexBatch{globalBySnapshot: map[int64][]*Rule{},
		bySite: map[int64][]*Rule{}, byRoute: map[int64][]*Rule{}}
	rows, err := db.R.QueryContext(ctx, `SELECT r.id, r.scope_kind, r.scope_id, r.directive,
		r.action_class, r.args, r.raw_text, r.shadowed, r.shadowed_by, f.path,
		r.prov_byte_start, f.id, r.snapshot_id
		FROM rule r JOIN snapshot_file f ON f.id = r.prov_file_id
		WHERE r.snapshot_id IN (`+holes+`) ORDER BY r.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r := &Rule{}
		if err := rows.Scan(&r.ID, &r.ScopeKind, &r.ScopeID, &r.Directive, &r.ActionClass,
			&r.Args, &r.Raw, &r.Shadowed, &r.ShadowedBy, &r.Path, &r.ByteStart,
			&r.FileID, &r.SnapshotID); err != nil {
			return nil, err
		}
		switch {
		case r.ScopeKind == "site" && r.ScopeID.Valid:
			idx.bySite[r.ScopeID.Int64] = append(idx.bySite[r.ScopeID.Int64], r)
		case r.ScopeKind == "route" && r.ScopeID.Valid:
			idx.byRoute[r.ScopeID.Int64] = append(idx.byRoute[r.ScopeID.Int64], r)
		default:
			idx.globalBySnapshot[r.SnapshotID] = append(idx.globalBySnapshot[r.SnapshotID], r)
		}
	}
	return idx, rows.Err()
}

// Resolve answers "which addresses does nodeID's own `getent hosts` say this name
// has?" (ADR-0010) — never the control plane's own resolver, and never another
// Node's answer for the same name: split-horizon DNS means that would just be a
// different, equally valid, wrong answer.
func (t *Topology) Resolve(nodeID int64, host string) []string {
	if ip := net.ParseIP(host); ip != nil {
		return []string{host}
	}
	return t.DNS[nodeID][host]
}

// InstancesAt finds the Instances reachable at a member's host, by name or by any
// address that name resolves to **on nodeID**, the Node that holds the Upstream
// naming it.
func (t *Topology) InstancesAt(nodeID int64, host string) []*Inst {
	seen := map[int64]bool{}
	var out []*Inst
	add := func(key string) {
		for _, in := range t.byAddress[strings.ToLower(key)] {
			if !seen[in.ID] {
				seen[in.ID] = true
				out = append(out, in)
			}
		}
	}
	add(host)
	add(hostOf(host))
	for _, addr := range t.Resolve(nodeID, host) {
		add(addr)
	}
	return out
}

// hostOf strips a domain suffix, so a Node registered as `web02` still matches an
// upstream that names it `web02.corp.example`.
func hostOf(name string) string {
	if h, _, ok := strings.Cut(name, "."); ok {
		return h
	}
	return name
}
