package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nagiflow/nagipath/internal/store"
)

// getCertificates and getCertificate moved to certificateservice.go as
// CertificateService's ListCertificates and GetCertificate RPCs
// (docs/adr/0018, proto/nagipath/api/v1/certificates.proto). The helpers
// below stay here: shared with certificateservice.go and getCertificatesCSV.

// getCertificatesCSV: GET /certificates?export=csv is a formatted download,
// not RPC-shaped data (gateway.go's gatewayOrCSV, wired in api.go).
func (s *Server) getCertificatesCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	list, err := s.DB.Certificates(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	list = s.filterCertificates(ctx, list, q.Get("expires"), q.Get("issuer"), q.Get("cluster"), q.Get("include_cas") == "1")

	out := [][]string{{"subject", "fingerprint_sha256", "issuer", "not_before", "not_after", "bindings", "nodes", "serves"}}
	for _, cert := range list {
		out = append(out, []string{cert.SubjectCN, cert.Fingerprint, cert.IssuerDN,
			cert.NotBefore, cert.NotAfter, strconv.Itoa(cert.Bindings), cert.Hosts,
			strings.Join(cert.Serves, ",")})
	}
	s.writeCSV(w, r, "certificates", out)
}

func (s *Server) filterCertificates(ctx context.Context, list []store.CertificateView, expires, issuer, cluster string, includeCAs bool) []store.CertificateView {
	var certsInCluster map[int64]bool
	if cluster != "" {
		clusterID, _ := strconv.ParseInt(cluster, 10, 64)
		if clusterID > 0 {
			certsInCluster = s.certsInCluster(ctx, clusterID)
		}
	}

	var filtered []store.CertificateView
	for _, cert := range list {
		if !includeCAs && cert.IsCA {
			continue
		}
		if expires != "" {
			t, err := time.Parse(time.RFC3339, cert.NotAfter)
			if err != nil {
				continue
			}
			days := int(time.Until(t).Hours() / 24)
			skip := false
			switch expires {
			case "expired":
				skip = days >= 0
			case "7d":
				skip = days < 0 || days > 7
			case "30d":
				skip = days < 0 || days > 30
			case "90d":
				skip = days < 0 || days > 90
			}
			if skip {
				continue
			}
		}
		if issuer != "" && !strings.Contains(strings.ToLower(cert.IssuerDN), strings.ToLower(issuer)) {
			continue
		}
		if certsInCluster != nil && !certsInCluster[cert.ID] {
			continue
		}
		filtered = append(filtered, cert)
	}
	return filtered
}

// certsInCluster returns the set of certificate IDs that have >=1 binding in
// the cluster, unchanged from internal/web/certificates.go.
func (s *Server) certsInCluster(ctx context.Context, clusterID int64) map[int64]bool {
	clusters, err := s.DB.Clusters(ctx)
	if err != nil {
		return nil
	}
	var clusterName string
	for _, c := range clusters {
		if c.ID == clusterID {
			clusterName = c.Name
			break
		}
	}
	if clusterName == "" {
		return nil
	}

	rows, err := s.DB.R.QueryContext(ctx, `SELECT DISTINCT b.certificate_id
		FROM certificate_binding b
		JOIN snapshot s ON s.id = b.snapshot_id AND s.is_current = 1
		JOIN instance i ON i.id = b.instance_id
		JOIN cluster c ON c.id = i.cluster_id
		WHERE c.name = ?`, clusterName)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := make(map[int64]bool)
	for rows.Next() {
		var certID int64
		if err := rows.Scan(&certID); err == nil {
			result[certID] = true
		}
	}
	return result
}

func distinctIssuers(list []store.CertificateView) []string {
	seen := map[string]bool{}
	var out []string
	for _, cert := range list {
		if !seen[cert.IssuerDN] {
			seen[cert.IssuerDN] = true
			out = append(out, cert.IssuerDN)
		}
	}
	return out
}

func buildCertSummary(list []store.CertificateView) string {
	expired, d30 := 0, 0
	for _, cert := range list {
		t, err := time.Parse(time.RFC3339, cert.NotAfter)
		if err != nil {
			continue
		}
		days := int(time.Until(t).Hours() / 24)
		if days < 0 {
			expired++
		} else if days <= 30 {
			d30++
		}
	}
	var parts []string
	if d30 > 0 {
		word, verb := "certificate", "expires"
		if d30 != 1 {
			word, verb = "certificates", "expire"
		}
		parts = append(parts, strconv.Itoa(d30)+" "+word+" "+verb+" within 30 days")
	}
	if expired > 0 {
		parts = append(parts, strconv.Itoa(expired)+" expired")
	}
	if len(parts) == 0 {
		return "All certificates valid"
	}
	return strings.Join(parts, " · ")
}

type certFilePath struct {
	Path       string
	NodeCount  int
	BundleType string
}

func buildFilePathStats(bindings []store.CertBinding) []certFilePath {
	paths := map[string]*certFilePath{}
	var order []string
	for _, b := range bindings {
		p := paths[b.FilePath]
		if p == nil {
			bundleType := "leaf only"
			if b.CombinedPEM {
				bundleType = "leaf + chain"
			}
			p = &certFilePath{Path: b.FilePath, BundleType: bundleType}
			paths[b.FilePath] = p
			order = append(order, b.FilePath)
		}
		p.NodeCount++
	}
	out := make([]certFilePath, 0, len(order))
	for _, path := range order {
		out = append(out, *paths[path])
	}
	return out
}
