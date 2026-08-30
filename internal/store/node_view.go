package store

import (
	"context"
	"database/sql"
)

// NodeListRow is one row in the /nodes table, with aggregated instance data.
type NodeListRow struct {
	Node
	// Vendor, Version, Cluster: mixed when the node runs multiple distinct processes.
	Vendor        sql.NullString
	Version       sql.NullString
	Cluster       sql.NullString
	ProcessCount  int
	Listeners    sql.NullString // comma-separated port list (NULL when no listeners)
	LastCaptured sql.NullString
	// The last collection attempt lands in the embedded Node's LastCollection and
	// LastStatus. A second pair of fields here meant one of the two was always
	// empty, and the template read the empty one — every node showed PENDING.
}

// CollectionState is the word the State badge says, and the same word /nodes and
// the node page both use. A node with no collection is "pending" rather than
// blank: nothing has been observed yet, which is a state and not a gap.
func (r NodeListRow) CollectionState() string {
	if !r.LastCollection.Valid || !r.LastStatus.Valid {
		return "pending"
	}
	return r.LastStatus.String
}

// NodesAggregated returns every non-retired Node with aggregated process data.
// The aggregates are computed per column in ONE query rather than per row.
func (db *DB) NodesAggregated(ctx context.Context) ([]NodeListRow, error) {
	// Vendor, version and cluster: when a node has a mix, show "mixed".
	// Listeners: the set of ports the node listens on, across all processes.
	// State: worst of the node's processes.
	query := `
		SELECT
			n.id, n.address, n.ssh_port, n.display_name, n.ssh_username, n.credential_id,
			n.bastion_node_id, n.os_family, n.sudo_available, n.enabled, n.source, n.notes,
			n.consecutive_failures, n.first_seen_at, n.retired_at,
			(SELECT COUNT(*) FROM instance WHERE node_id = n.id AND retired_at IS NULL),
			(SELECT COUNT(*) FROM host_key WHERE node_id = n.id AND state = 'pending'),
			(SELECT CASE
				WHEN COUNT(DISTINCT vendor) = 1 THEN MIN(vendor)
				WHEN COUNT(DISTINCT vendor) > 1 THEN 'mixed'
				ELSE NULL
			END FROM instance WHERE node_id = n.id AND retired_at IS NULL),
			(SELECT CASE
				WHEN COUNT(DISTINCT version) = 1 THEN MIN(version)
				WHEN COUNT(DISTINCT version) > 1 THEN 'mixed'
				ELSE NULL
			END FROM instance WHERE node_id = n.id AND retired_at IS NULL),
			(SELECT CASE
				WHEN COUNT(DISTINCT cluster_id) = 1 THEN
					(SELECT name FROM cluster c WHERE c.id = (SELECT cluster_id FROM instance WHERE node_id = n.id AND retired_at IS NULL AND cluster_id IS NOT NULL LIMIT 1))
				WHEN COUNT(DISTINCT cluster_id) > 1 THEN 'mixed'
				ELSE NULL
			END FROM instance WHERE node_id = n.id AND retired_at IS NULL),
			(SELECT GROUP_CONCAT(DISTINCT CAST(l.port AS TEXT))
			 FROM listener l
			 JOIN snapshot s ON l.snapshot_id = s.id
			 JOIN instance i ON s.instance_id = i.id
			 WHERE i.node_id = n.id AND s.is_current = 1
			 ORDER BY l.port),
			(SELECT MAX(s.captured_at)
			 FROM snapshot s
			 JOIN instance i ON s.instance_id = i.id
			 WHERE i.node_id = n.id AND s.is_current = 1),
			(SELECT MAX(started_at) FROM collection WHERE node_id = n.id),
			(SELECT status FROM collection WHERE node_id = n.id ORDER BY started_at DESC LIMIT 1)
		FROM node n
		WHERE n.retired_at IS NULL
		ORDER BY n.display_name`

	rows, err := db.R.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []NodeListRow
	for rows.Next() {
		var r NodeListRow
		err := rows.Scan(
			&r.ID, &r.Address, &r.SSHPort, &r.DisplayName, &r.SSHUsername,
			&r.CredentialID, &r.BastionNodeID, &r.OSFamily, &r.SudoAvailable, &r.Enabled,
			&r.Source, &r.Notes, &r.ConsecutiveFailures, &r.FirstSeenAt, &r.RetiredAt,
			&r.ProcessCount, &r.PendingHostKeys,
			&r.Vendor, &r.Version, &r.Cluster, &r.Listeners,
			&r.LastCaptured, &r.LastCollection, &r.LastStatus,
		)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// NodeProcesses returns all processes (instances) for a given node.
func (db *DB) NodeProcesses(ctx context.Context, nodeID int64) ([]Instance, error) {
	query := `
		SELECT
			i.id, i.node_id, i.cluster_id, i.vendor, i.natural_key, i.display_name,
			i.version, i.binary_path, i.config_root, i.main_config_path, i.build_flags,
			i.service_manager, i.unit_name, i.detected_pid, i.first_seen_at, i.last_seen_at,
			i.retired_at,
			(SELECT COUNT(*) FROM site WHERE snapshot_id = (SELECT id FROM snapshot WHERE instance_id = i.id AND is_current = 1 LIMIT 1)) AS site_count,
			(SELECT COUNT(*) FROM route WHERE snapshot_id = (SELECT id FROM snapshot WHERE instance_id = i.id AND is_current = 1 LIMIT 1)) AS route_count,
			-- certificate_id, not subject_cn: the subject lives on certificate, and one
			-- cert bound to four listeners is one certificate to keep renewed.
			(SELECT COUNT(DISTINCT certificate_id) FROM certificate_binding WHERE snapshot_id = (SELECT id FROM snapshot WHERE instance_id = i.id AND is_current = 1 LIMIT 1)) AS cert_count,
			(SELECT captured_at FROM snapshot WHERE instance_id = i.id AND is_current = 1 LIMIT 1) AS last_captured,
			-- COALESCE, not the bare aggregate: GROUP_CONCAT over no listeners is
			-- NULL, and a process that listens on nothing is the normal state of one
			-- that has not been collected yet.
			COALESCE((SELECT GROUP_CONCAT(DISTINCT CAST(l.port AS TEXT)) FROM listener l WHERE l.snapshot_id = (SELECT id FROM snapshot WHERE instance_id = i.id AND is_current = 1 LIMIT 1)), '') AS ports,
			COALESCE((SELECT c.name FROM cluster c WHERE c.id = i.cluster_id), '') AS cluster_name
		FROM instance i
		WHERE i.node_id = ? AND i.retired_at IS NULL
		ORDER BY i.display_name`

	rows, err := db.R.QueryContext(ctx, query, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Instance
	for rows.Next() {
		var i Instance
		err := rows.Scan(
			&i.ID, &i.NodeID, &i.ClusterID, &i.Vendor, &i.NaturalKey, &i.DisplayName,
			&i.Version, &i.BinaryPath, &i.ConfigRoot, &i.MainConfigPath, &i.BuildFlags,
			&i.ServiceManager, &i.UnitName, &i.DetectedPID, &i.FirstSeenAt, &i.LastSeenAt,
			&i.RetiredAt,
			&i.SiteCount, &i.RouteCount, &i.CertCount, &i.LastCaptured, &i.Ports,
			&i.ClusterName,
		)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// NodeStats returns stats for the overview tab of a node detail page.
type NodeStats struct {
	Sites       int
	Routes      int
	Upstreams   int
	CertCount   int
	DriftCount  int
	SitesByPort map[string]int // port → count
}

func (db *DB) NodeStats(ctx context.Context, instanceID int64) (NodeStats, error) {
	var s NodeStats
	s.SitesByPort = make(map[string]int)

	// Get current snapshot
	var snapshotID int64
	err := db.R.QueryRowContext(ctx,
		`SELECT id FROM snapshot WHERE instance_id = ? AND is_current = 1 LIMIT 1`,
		instanceID).Scan(&snapshotID)
	if err != nil {
		return s, err
	}

	// Count sites, routes, upstreams
	err = db.R.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM site WHERE snapshot_id = ?),
			(SELECT COUNT(*) FROM route WHERE snapshot_id = ?),
			(SELECT COUNT(*) FROM upstream WHERE snapshot_id = ?),
			(SELECT COUNT(DISTINCT certificate_id) FROM certificate_binding WHERE snapshot_id = ?)`,
		snapshotID, snapshotID, snapshotID, snapshotID).Scan(
		&s.Sites, &s.Routes, &s.Upstreams, &s.CertCount)

	return s, err
}

// UpstreamPool is a pool with its members and resolution state.
type UpstreamPool struct {
	ID            int64
	Name          string
	Kind          string
	BalanceMethod string
	MemberCount   int
	UsedBy        string // first route using this pool
	Resolution    string // "resolved", "partial", "external"
	State         string // "RESOLVED", "PARTIAL", "EXTERNAL"
}

// UpstreamMember is one member with its mapped node.
type UpstreamMember struct {
	Host     string
	Port     int
	Weight   int
	Flags    string
	Role     string // "primary", "backup"
	NodeID   sql.NullInt64
	NodeName sql.NullString
}

// NodeUpstreams returns all upstream pools for a given snapshot.
func (db *DB) NodeUpstreams(ctx context.Context, snapshotID int64) ([]UpstreamPool, error) {
	query := `
		SELECT
			u.id, u.name, u.kind, u.balance_method,
			(SELECT COUNT(*) FROM upstream_member WHERE upstream_id = u.id) AS member_count,
			(SELECT COUNT(*) FROM upstream_member um
			 LEFT JOIN node n ON um.host = n.address
			 WHERE um.upstream_id = u.id AND n.id IS NULL AND um.host NOT LIKE '%:%') AS unresolved_count,
			(SELECT r.pattern FROM route r WHERE r.upstream_id = u.id LIMIT 1) AS used_by
		FROM upstream u
		WHERE u.snapshot_id = ?
		ORDER BY u.name`

	rows, err := db.R.QueryContext(ctx, query, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []UpstreamPool
	for rows.Next() {
		var p UpstreamPool
		var unresolvedCount int
		var usedBy sql.NullString
		err := rows.Scan(&p.ID, &p.Name, &p.Kind, &p.BalanceMethod,
			&p.MemberCount, &unresolvedCount, &usedBy)
		if err != nil {
			return nil, err
		}

		if usedBy.Valid {
			p.UsedBy = usedBy.String
		}

		// Determine resolution state
		if unresolvedCount == 0 && p.MemberCount > 0 {
			p.Resolution = "all in inventory"
			p.State = "RESOLVED"
		} else if unresolvedCount == p.MemberCount {
			p.Resolution = "outside inventory"
			p.State = "EXTERNAL"
		} else {
			p.Resolution = "partial resolution"
			p.State = "PARTIAL"
		}

		out = append(out, p)
	}
	return out, rows.Err()
}

// UpstreamMembers returns all members of an upstream pool with their mapped nodes.
func (db *DB) UpstreamMembers(ctx context.Context, upstreamID int64) ([]UpstreamMember, error) {
	query := `
		SELECT
			-- Both columns are nullable and normally null: a member with no explicit
			-- port takes the scheme default, and one with no explicit weight takes the
			-- balancer's. Zero is how the template says "the config did not name one".
			um.host, COALESCE(um.port, 0), COALESCE(um.weight, 0), um.flags,
			n.id, n.display_name
		FROM upstream_member um
		LEFT JOIN node n ON um.host = n.address
		WHERE um.upstream_id = ?
		ORDER BY um.ordinal`

	rows, err := db.R.QueryContext(ctx, query, upstreamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []UpstreamMember
	for rows.Next() {
		var m UpstreamMember
		err := rows.Scan(&m.Host, &m.Port, &m.Weight, &m.Flags, &m.NodeID, &m.NodeName)
		if err != nil {
			return nil, err
		}

		// Determine role from flags
		if m.Flags == "backup" {
			m.Role = "backup"
		} else {
			m.Role = "primary"
		}

		out = append(out, m)
	}
	return out, rows.Err()
}
