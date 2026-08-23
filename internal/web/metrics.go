package web

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/nagiflow/nagipath/internal/store"
)

// readyz probes the two things a load balancer actually needs to know before
// sending traffic: the database answers queries, and every embedded migration
// has actually run. Cheap on purpose — this is hit every few seconds.
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.DB.Ping(ctx); err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, "database unreachable: %v", err)
		return
	}
	applied, err := s.DB.AppliedMigrations(ctx)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, "database unreachable: %v", err)
		return
	}
	expected, err := store.ExpectedMigrationCount()
	if err != nil || len(applied) != expected {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, "migrations pending: %d applied, %d expected", len(applied), expected)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

// metrics serves a hand-written Prometheus text-exposition document (see
// ponytail comment in internal/store/store.go for why this codebase hand-writes
// rather than pulling in prometheus/client_golang for one endpoint).
//
// Off by default: with no -metrics-token/NAGIPATH_METRICS_TOKEN configured this
// 404s, so fleet-internal counts are never exposed until an operator
// deliberately turns the endpoint on.
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	if s.MetricsToken == "" {
		http.NotFound(w, r)
		return
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(s.MetricsToken)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	var b strings.Builder

	nodes, _ := s.DB.NodeCount(ctx)
	writeGauge(&b, "nagipath_nodes_total", "Non-retired Nodes.", float64(nodes))

	instances, _ := s.DB.InstanceCount(ctx)
	writeGauge(&b, "nagipath_instances_total", "Non-retired Instances.", float64(instances))

	statusCounts, _ := s.DB.CollectionStatusCounts(ctx)
	writeHeader(&b, "nagipath_collections_total", "counter", "Collections, by final status.")
	// Fixed, known order rather than map iteration order, so scrapes diff cleanly.
	for _, status := range []string{"succeeded", "degraded", "failed", "running"} {
		fmt.Fprintf(&b, "nagipath_collections_total{status=%q} %d\n", status, statusCounts[status])
	}

	durSum, durCount, _ := s.DB.CollectionDurationStats(ctx)
	writeHeader(&b, "nagipath_collection_duration_ms_sum", "gauge",
		"Sum of duration_ms across finished Collections (Prometheus summary convention).")
	fmt.Fprintf(&b, "nagipath_collection_duration_ms_sum %d\n", durSum)
	writeHeader(&b, "nagipath_collection_duration_ms_count", "gauge",
		"Count of finished Collections with a recorded duration.")
	fmt.Fprintf(&b, "nagipath_collection_duration_ms_count %d\n", durCount)

	if fi, err := os.Stat(s.DB.Path); err == nil {
		writeGauge(&b, "nagipath_database_bytes", "Size of the SQLite database file.", float64(fi.Size()))
	}

	blobCount, bytesRaw, bytesZstd, _ := s.DB.BlobStats(ctx)
	writeGauge(&b, "nagipath_blob_count", "Distinct content-addressed blobs stored.", float64(blobCount))
	writeGauge(&b, "nagipath_blob_bytes_raw_total", "Total uncompressed bytes across all blobs.", float64(bytesRaw))
	writeGauge(&b, "nagipath_blob_bytes_zstd_total", "Total zstd-compressed bytes across all blobs.", float64(bytesZstd))

	probes, _ := s.DB.ProbeCount(ctx)
	writeHeader(&b, "nagipath_probes_total", "counter", "Probes ever sent.")
	fmt.Fprintf(&b, "nagipath_probes_total %d\n", probes)

	quarantineThreshold := s.DB.SettingInt(ctx, "quarantine_after_failures")
	quarantined, _ := s.DB.QuarantinedNodeCount(ctx, quarantineThreshold)
	writeGauge(&b, "nagipath_nodes_quarantined_total",
		"Non-retired Nodes whose consecutive collection failures have reached quarantine_after_failures.",
		float64(quarantined))

	// the job queue is schema-only, unimplemented — nothing real to report yet
	// (the job table has zero Go references).

	writeHeader(&b, "nagipath_http_requests_total", "counter", "HTTP requests served, process lifetime.")
	fmt.Fprintf(&b, "nagipath_http_requests_total %d\n", s.httpRequests.Load())
	writeHeader(&b, "nagipath_http_request_duration_ms_sum", "counter",
		"Sum of HTTP request durations in milliseconds, process lifetime (summary convention).")
	fmt.Fprintf(&b, "nagipath_http_request_duration_ms_sum %d\n", s.httpRequestDurMSSum.Load())
	// _count intentionally reuses the same total: one counter incremented once
	// per request, so the two are always equal — recorded as its own metric line
	// only for the summary-style name a scraper expects to find.
	writeHeader(&b, "nagipath_http_request_duration_ms_count", "counter",
		"Count of HTTP requests included in the duration sum, process lifetime.")
	fmt.Fprintf(&b, "nagipath_http_request_duration_ms_count %d\n", s.httpRequests.Load())

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.Write([]byte(b.String()))
}

func writeGauge(b *strings.Builder, name, help string, value float64) {
	writeHeader(b, name, "gauge", help)
	fmt.Fprintf(b, "%s %s\n", name, strconv.FormatFloat(value, 'f', -1, 64))
}

func writeHeader(b *strings.Builder, name, typ, help string) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}
