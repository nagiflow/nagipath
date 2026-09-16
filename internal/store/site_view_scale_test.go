package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// seedSites builds a fleet of nodes x sites with routes, aliases and certs.
func seedSites(t testing.TB, db *DB, nodes, sitesPerNode, routesPerSite int) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ex := func(q string, a ...any) {
		if _, err := tx.ExecContext(ctx, q, a...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	ex(`INSERT INTO blob(sha256, bytes_raw, bytes_zstd, content, created_at) VALUES('x',1,1,x'00',?)`, now)
	ex(`INSERT INTO cluster(id, name, created_at) VALUES(1,'c1',?)`, now)
	for n := 1; n <= nodes; n++ {
		ex(`INSERT INTO node(id, display_name, address, source, first_seen_at)
		    VALUES(?,?,?,'manual',?)`, n, fmt.Sprintf("node%d", n), fmt.Sprintf("10.0.0.%d", n), now)
		ex(`INSERT INTO instance(id, node_id, cluster_id, vendor, natural_key, display_name, first_seen_at, last_seen_at)
		    VALUES(?,?,1,'nginx',?,?,?,?)`, n, n, "nginx", "nginx", now, now)
		ex(`INSERT INTO collection(id, node_id, trigger, started_at, status)
		    VALUES(?,?,'manual',?,'succeeded')`, n, n, now)
		ex(`INSERT INTO snapshot(id, collection_id, instance_id, captured_at, config_source, degraded,
		    content_sha256, file_count, bytes_raw, parse_state, parser_version, is_current)
		    VALUES(?,?,?,?,'vendor_dump',0,'x',1,1,'parsed',1,1)`, n, n, n, now)
		ex(`INSERT INTO snapshot_file(id, snapshot_id, kind, path, blob_sha256, bytes_raw)
		    VALUES(?,?,'config_file','/etc/nginx.conf','x',1)`, n, n)
		ex(`INSERT INTO listener(id, snapshot_id, instance_id, parser_version, natural_key, ordinal,
		    prov_file_id, prov_byte_start, prov_byte_end, address, port, tls, protocol, is_default, raw_text)
		    VALUES(?,?,?,1,'l',0,?,0,0,'0.0.0.0',443,1,'http2',0,'listen 443')`, n, n, n, n)
		ex(`INSERT INTO certificate(id, fingerprint_sha256, subject_cn, sans, issuer_dn,
		    not_before, not_after, first_seen_at, last_seen_at)
		    VALUES(?,?,'cn.example.com','[]','CN=ca',?,?,?,?)`,
			n, fmt.Sprintf("fp%d", n), now, time.Now().AddDate(1, 0, 0).UTC().Format(time.RFC3339), now, now)
	}
	siteID, routeID, upID, memID, nameID, cbID := 0, 0, 0, 0, 0, 0
	for n := 1; n <= nodes; n++ {
		for s := 1; s <= sitesPerNode; s++ {
			siteID++
			host := fmt.Sprintf("site%d.example.com", s)
			ex(`INSERT INTO site(id, snapshot_id, instance_id, parser_version, natural_key, ordinal,
			    prov_file_id, prov_byte_start, prov_byte_end, listener_ids, primary_name, kind, raw_text)
			    VALUES(?,?,?,1,?,?,?,0,0,?,?,'nginx_server',?)`,
				siteID, n, n, host, s, n, fmt.Sprintf("[%d]", n), host,
				fmt.Sprintf("server { server_name %s; }", host))
			for a := 1; a <= 2; a++ {
				nameID++
				ex(`INSERT INTO site_name(id, site_id, name, match_kind, ordinal) VALUES(?,?,?,'exact',?)`,
					nameID, siteID, fmt.Sprintf("alias%d.site%d.example.com", a, s), a)
			}
			upID++
			ex(`INSERT INTO upstream(id, snapshot_id, instance_id, parser_version, natural_key, ordinal,
			    prov_file_id, prov_byte_start, prov_byte_end, name, kind, raw_text)
			    VALUES(?,?,?,1,?,?,?,0,0,?,'nginx_upstream','upstream')`, upID, n, n, fmt.Sprintf("u%d", upID), s, n, fmt.Sprintf("pool%d", s))
			for m := 1; m <= 3; m++ {
				memID++
				ex(`INSERT INTO upstream_member(id, snapshot_id, instance_id, parser_version, natural_key, ordinal,
				    prov_file_id, prov_byte_start, prov_byte_end, upstream_id, host, port, scheme, raw_text)
				    VALUES(?,?,?,1,?,?,?,0,0,?,?,8080,'http','server')`, memID, n, n, fmt.Sprintf("m%d", memID), m, n, upID, fmt.Sprintf("10.1.0.%d", m))
			}
			for r := 1; r <= routesPerSite; r++ {
				routeID++
				ex(`INSERT INTO route(id, snapshot_id, instance_id, parser_version, natural_key, ordinal,
				    prov_file_id, prov_byte_start, prov_byte_end, site_id, match_type, pattern,
				    precedence_rank, specificity, upstream_id, target_raw, is_terminal, raw_text)
				    VALUES(?,?,?,1,?,?,?,0,0,?,'prefix',?,1,1,?,'proxy_pass',0,'location')`,
					routeID, n, n, fmt.Sprintf("r%d", routeID), r, n, siteID, fmt.Sprintf("/path%d", r), upID)
			}
			cbID++
			ex(`INSERT INTO certificate_binding(id, certificate_id, instance_id, snapshot_id, listener_id,
			    site_id, file_path, observed_at) VALUES(?,?,?,?,?,?,?,?)`, cbID, n, n, n, n, siteID, fmt.Sprintf("/c%d.pem", siteID), now)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func benchDB(t testing.TB) *DB {
	db, err := Open(filepath.Join(t.TempDir(), "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestSiteListAggregates pins the per-hostname aggregates of the rewritten
// SiteListWithVariants query: every fact is now counted in its own CTE instead
// of de-duplicated out of one cartesian join, which is exactly what could
// silently start double-counting.
func TestSiteListAggregates(t *testing.T) {
	db := benchDB(t)
	const nodes, sites, routes = 5, 50, 3
	seedSites(t, db, nodes, sites, routes)

	start := time.Now()
	rows, _, err := db.SiteListWithVariants(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("SiteListWithVariants over %d hostnames on %d nodes: %v", sites, nodes, time.Since(start))

	if len(rows) != sites {
		t.Fatalf("got %d rows, want %d", len(rows), sites)
	}
	r := rows[0]
	if r.Nodes != nodes {
		t.Errorf("Nodes = %d, want %d", r.Nodes, nodes)
	}
	if r.Routes != nodes*routes {
		t.Errorf("Routes = %d, want %d", r.Routes, nodes*routes)
	}
	if r.Variants != 1 {
		t.Errorf("Variants = %d, want 1", r.Variants)
	}
	if r.AliasCount != 2 || len(r.Aliases) != 2 {
		t.Errorf("AliasCount = %d, Aliases = %v, want 2", r.AliasCount, r.Aliases)
	}
	if r.ListenerSummary != "0.0.0.0:443 ssl http2" {
		t.Errorf("ListenerSummary = %q", r.ListenerSummary)
	}
	if r.CertSubject != "cn.example.com" {
		t.Errorf("CertSubject = %q", r.CertSubject)
	}
	if r.State != "OK" {
		t.Errorf("State = %q, want OK", r.State)
	}
}

// TestSitesStatsAggregates pins the header tiles, whose query was likewise
// rewritten from several scans of one CTE into a single grouped pass.
func TestSitesStatsAggregates(t *testing.T) {
	db := benchDB(t)
	const nodes, sites = 5, 50
	seedSites(t, db, nodes, sites, 3)

	got, err := db.SitesStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := SiteStats{Hostnames: sites, Nodes: nodes, VariantHosts: 0, TLSTerminated: sites}
	if got != want {
		t.Errorf("SitesStats = %+v, want %+v", got, want)
	}
}
