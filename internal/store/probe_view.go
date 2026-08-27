package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

// ProbeView is the full detail of one stored Probe for the GET /trace/probe screen.
type ProbeView struct {
	ProbeID          int64
	TraceID          sql.NullInt64
	ActorUsername    string
	Method           string
	URL              string
	MaxRedirects     int
	CorrelationToken string
	OriginHost       string
	RequestedAt      string
	Status           int
	DurationMS       int64
	RedirectCount    int
	ServerHeader     string
	ViaHeader        string
	Result           string
	// LogCapableCount is how many hops on the trace have log paths.
	LogCapableCount int
	// VerifiedCount is how many hops reached "verified" confidence.
	VerifiedCount int
	// StillInferred is the count of hops that stayed inferred.
	StillInferred int
	// RetentionDays is from settings.
	RetentionDays int
}

// ProbeHopEvidence is one hop's before/after from a stored probe.
type ProbeHopEvidence struct {
	HopOrdinal   int
	NodeName     string
	Vendor       string
	Evidence     string
	Before       string
	After        string
	HasGap       bool
	GapVendor    string
	GapNote      string
	GapDirective string
}

// ProbeLogLine is one matched access log line from a probe.
type ProbeLogLine struct {
	NodeName string
	LogPath  string
	RawLine  string
}

// LoadProbeView reads one stored probe by id for the GET screen.
func (db *DB) LoadProbeView(ctx context.Context, id int64) (*ProbeView, error) {
	if id <= 0 {
		return nil, nil
	}
	var p ProbeView
	var redirectChain string
	var responseHeaders string
	err := db.R.QueryRowContext(ctx, `SELECT p.id, p.trace_id, p.method, p.url,
		p.correlation_token, p.origin_host, p.requested_at,
		COALESCE(p.status_code, 0), COALESCE(p.duration_ms, 0),
		COALESCE(p.redirect_chain, '[]'), COALESCE(p.response_headers, '{}'),
		p.result, COALESCE(u.username, '')
		FROM probe p LEFT JOIN app_user u ON u.id = p.actor_user_id
		WHERE p.id = ?`, id).Scan(&p.ProbeID, &p.TraceID, &p.Method, &p.URL,
		&p.CorrelationToken, &p.OriginHost, &p.RequestedAt,
		&p.Status, &p.DurationMS, &redirectChain, &responseHeaders,
		&p.Result, &p.ActorUsername)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Count redirects
	var redirects []string
	json.Unmarshal([]byte(redirectChain), &redirects)
	p.RedirectCount = len(redirects)

	// Extract server and via headers
	var headers map[string]string
	json.Unmarshal([]byte(responseHeaders), &headers)
	p.ServerHeader = headers["Server"]
	p.ViaHeader = headers["Via"]

	p.RetentionDays = db.SettingInt(ctx, "probe_retention_days")

	// Count log-capable hops, verified hops, and still-inferred from evidence
	if p.TraceID.Valid {
		db.R.QueryRowContext(ctx, `SELECT COUNT(DISTINCT h.id)
			FROM hop h JOIN instance i ON i.id = h.instance_id
			WHERE h.trace_id = ? AND h.is_external = 0
			  AND json_array_length(COALESCE(i.access_log_paths, '[]')) > 0`,
			p.TraceID.Int64).Scan(&p.LogCapableCount)

		db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM probe_evidence
			WHERE probe_id = ? AND grants = 'verified'`, id).Scan(&p.VerifiedCount)

		db.R.QueryRowContext(ctx, `SELECT COUNT(DISTINCT instance_id)
			FROM probe_evidence
			WHERE probe_id = ? AND kind IN ('access_log_absent', 'access_log_error')`,
			id).Scan(&p.StillInferred)
	}

	return &p, nil
}

// ProbeHopEvidenceRows reads the before/after for each hop from a stored probe.
func (db *DB) ProbeHopEvidenceRows(ctx context.Context, probeID int64) ([]ProbeHopEvidence, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT DISTINCT
		COALESCE(h.ordinal, -1),
		COALESCE(n.display_name, i.display_name, ''),
		COALESCE(i.vendor, ''),
		COALESCE(json_extract(e.parsed_fields, '$.prior_confidence'), 'inferred'),
		MAX(CASE WHEN e.grants = 'verified' THEN 'verified'
		         WHEN e.grants = 'observed_effect' THEN 'observed_effect'
		         ELSE 'inferred' END)
		FROM probe_evidence e
		LEFT JOIN hop h ON h.id = e.hop_id
		LEFT JOIN instance i ON i.id = e.instance_id
		LEFT JOIN node n ON n.id = i.node_id
		WHERE e.probe_id = ?
		GROUP BY e.instance_id, h.ordinal
		ORDER BY h.ordinal`, probeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProbeHopEvidence
	for rows.Next() {
		var h ProbeHopEvidence
		if err := rows.Scan(&h.HopOrdinal, &h.NodeName, &h.Vendor, &h.Before, &h.After); err != nil {
			return nil, err
		}

		// Determine evidence text
		switch {
		case h.After == "verified":
			h.Evidence = "access log · correlation token"
		case h.After == "observed_effect":
			h.Evidence = "response header"
		default:
			h.Evidence = "log format gap"
		}

		out = append(out, h)
	}
	return out, rows.Err()
}

// ProbeLogLines reads the matched access log lines from a stored probe.
func (db *DB) ProbeLogLines(ctx context.Context, probeID int64) ([]ProbeLogLine, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT
		COALESCE(n.display_name, i.display_name, ''),
		e.log_path,
		e.raw_evidence
		FROM probe_evidence e
		LEFT JOIN instance i ON i.id = e.instance_id
		LEFT JOIN node n ON n.id = i.node_id
		WHERE e.probe_id = ? AND e.kind = 'access_log_line'
		ORDER BY e.id`, probeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProbeLogLine
	for rows.Next() {
		var l ProbeLogLine
		if err := rows.Scan(&l.NodeName, &l.LogPath, &l.RawLine); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ProbeLogFormatGaps reads the log format gaps from a stored probe.
func (db *DB) ProbeLogFormatGaps(ctx context.Context, probeID int64, traceID int64) ([]ProbeHopEvidence, error) {
	if traceID == 0 {
		return nil, nil
	}

	// Get instances from trace hops
	rows, err := db.R.QueryContext(ctx, `SELECT DISTINCT
		i.id, i.display_name, i.vendor
		FROM hop h
		JOIN instance i ON i.id = h.instance_id
		WHERE h.trace_id = ? AND h.is_external = 0`, traceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProbeHopEvidence
	for rows.Next() {
		var instanceID int64
		var instance, vendor string
		if err := rows.Scan(&instanceID, &instance, &vendor); err != nil {
			return nil, err
		}

		// Check if there's evidence showing this hop stayed inferred due to log format
		var hasGap bool
		db.R.QueryRowContext(ctx, `SELECT 1 FROM probe_evidence
			WHERE probe_id = ? AND instance_id = ?
			  AND kind IN ('access_log_absent', 'access_log_error')
			LIMIT 1`, probeID, instanceID).Scan(&hasGap)

		if hasGap {
			// Get the gap details - reuse probe.logFormatGap logic
			var directives []string
			dRows, err := db.R.QueryContext(ctx, `SELECT raw_text FROM rule
				WHERE instance_id = ? AND lower(directive) IN ('log_format','logformat','option')
				  AND snapshot_id = (SELECT id FROM snapshot WHERE instance_id = ? AND is_current = 1)`,
				instanceID, instanceID)
			if err == nil {
				for dRows.Next() {
					var raw string
					if dRows.Scan(&raw) == nil {
						directives = append(directives, raw)
					}
				}
				dRows.Close()
			}

			h := ProbeHopEvidence{
				NodeName: instance,
				Vendor:   vendor,
				HasGap:   true,
			}

			// Simplified gap detection matching probe.logFormatGap
			all := ""
			for _, d := range directives {
				all += d + "\n"
			}
			switch vendor {
			case "nginx":
				h.GapNote = "log_format lacks $server_name, $upstream_addr, or token field"
				h.GapDirective = `log_format nagipath '$remote_addr - $host [$time_local] "$request" '
                   '$status $body_bytes_sent $server_name $upstream_addr '
                   '"$http_user_agent" $http_x_nagipath_probe';
access_log /var/log/nginx/access.log nagipath;`
			case "apache":
				h.GapNote = "LogFormat lacks %v or token field"
				h.GapDirective = `LogFormat "%h %v %l %u %t \"%r\" %>s %b \"%{User-Agent}i\" %{X-Nagipath-Probe}i" nagipath
CustomLog /var/log/apache2/access.log nagipath`
			}

			if h.GapDirective != "" {
				out = append(out, h)
			}
		}
	}
	return out, nil
}
