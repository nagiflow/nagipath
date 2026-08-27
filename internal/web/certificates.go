package web

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nagiflow/nagipath/internal/store"
)

type certFilters struct {
	Expires    string
	Issuer     string
	Cluster    string
	IncludeCAs bool
}

type certsListData struct {
	List     []store.CertificateView
	Sel      *store.CertificateView
	Bindings []store.CertBinding
	Trace    string
	Filters  certFilters
	Issuers  []string
	Clusters []store.Cluster
	Summary  string
}

type certDetailData struct {
	Cert         store.Certificate
	Bindings     []store.CertBinding
	FilePaths    []certFilePath
	ActiveTab    string
	BindingCount int
	FileCount    int
}

type certFilePath struct {
	Path       string
	NodeCount  int
	BundleType string
}

func (s *Server) certificates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	filters := certFilters{
		Expires:    r.URL.Query().Get("expires"),
		Issuer:     r.URL.Query().Get("issuer"),
		Cluster:    r.URL.Query().Get("cluster"),
		IncludeCAs: r.URL.Query().Get("include_cas") == "1",
	}

	list, err := s.DB.Certificates(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	// Apply filters
	list = s.filterCertificates(ctx, list, filters)

	// Get distinct issuers and clusters for filter dropdowns
	issuers := distinctIssuers(list)
	clusters, _ := s.DB.Clusters(ctx)

	d := certsListData{
		List:     list,
		Filters:  filters,
		Issuers:  issuers,
		Clusters: clusters,
		Summary:  buildCertSummary(list),
	}

	if r.URL.Query().Get("export") == "csv" {
		rows := [][]string{{"subject", "fingerprint_sha256", "issuer", "not_before", "not_after", "bindings", "nodes", "serves"}}
		for _, cert := range list {
			rows = append(rows, []string{cert.SubjectCN, cert.Fingerprint, cert.IssuerDN,
				cert.NotBefore, cert.NotAfter, strconv.Itoa(cert.Bindings), cert.Hosts,
				strings.Join(cert.Serves, ",")})
		}
		s.writeCSV(w, r, "certificates", rows)
		return
	}

	s.render(w, r, "certificates.html", "Certificates", d)
}

func (s *Server) filterCertificates(ctx context.Context, list []store.CertificateView, f certFilters) []store.CertificateView {
	var filtered []store.CertificateView

	// Hoist cluster filtering: get the set of certificate IDs in the cluster once
	var certsInCluster map[int64]bool
	if f.Cluster != "" {
		clusterID, _ := strconv.ParseInt(f.Cluster, 10, 64)
		if clusterID > 0 {
			certsInCluster = s.certsInCluster(ctx, clusterID)
		}
	}

	for _, cert := range list {
		// Filter by CA status
		if !f.IncludeCAs && cert.IsCA {
			continue
		}

		// Filter by expiry window
		if f.Expires != "" {
			t, err := time.Parse(time.RFC3339, cert.NotAfter)
			if err != nil {
				continue
			}
			days := int(time.Until(t).Hours() / 24)

			skip := false
			switch f.Expires {
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

		// Filter by issuer
		if f.Issuer != "" && !strings.Contains(strings.ToLower(cert.IssuerDN), strings.ToLower(f.Issuer)) {
			continue
		}

		// Filter by cluster: cert passes if it's in the set
		if certsInCluster != nil && !certsInCluster[cert.ID] {
			continue
		}

		filtered = append(filtered, cert)
	}

	return filtered
}

// certsInCluster returns the set of certificate IDs that have ≥1 binding in the cluster
func (s *Server) certsInCluster(ctx context.Context, clusterID int64) map[int64]bool {
	// Get cluster name once
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

	// Query all certificate bindings for instances in this cluster (by cluster name)
	// and return the set of certificate IDs
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
	expired, d7, d30 := 0, 0, 0
	expiredB, d7B, d30B := 0, 0, 0

	for _, cert := range list {
		t, err := time.Parse(time.RFC3339, cert.NotAfter)
		if err != nil {
			continue
		}
		days := int(time.Until(t).Hours() / 24)

		if days < 0 {
			expired++
			expiredB += cert.Bindings
		} else if days <= 7 {
			d7++
			d7B += cert.Bindings
		} else if days <= 30 {
			d30++
			d30B += cert.Bindings
		}
	}

	parts := []string{}
	if d30 > 0 {
		word := "certificate"
		if d30 != 1 {
			word = "certificates"
		}
		verb := "expires"
		if d30 != 1 {
			verb = "expire"
		}
		parts = append(parts, fmt.Sprintf("%d %s %s within 30 days", d30, word, verb))
	}
	if expired > 0 {
		parts = append(parts, strconv.Itoa(expired)+" expired")
	}

	if len(parts) == 0 {
		return "All certificates valid"
	}
	return strings.Join(parts, " · ")
}

func (s *Server) certificateDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	certID := idOf(r, "id")

	cert, err := s.DB.CertificateByID(ctx, certID)
	if err != nil {
		s.notFound(w, r)
		return
	}

	bindings, _ := s.DB.CertBindings(ctx, certID)

	// Get distinct file paths
	filePaths := s.buildFilePathStats(bindings)

	activeTab := r.URL.Query().Get("tab")
	if activeTab == "" {
		activeTab = "overview"
	}

	d := certDetailData{
		Cert:         cert,
		Bindings:     bindings,
		FilePaths:    filePaths,
		ActiveTab:    activeTab,
		BindingCount: len(bindings),
		FileCount:    len(filePaths),
	}

	s.render(w, r, "certificate.html", cert.SubjectCN, d)
}

func (s *Server) buildFilePathStats(bindings []store.CertBinding) []certFilePath {
	paths := map[string]*certFilePath{}

	for _, b := range bindings {
		p := paths[b.FilePath]
		if p == nil {
			bundleType := "leaf only"
			// Use the real combined_pem field from the binding
			if b.CombinedPEM {
				bundleType = "leaf + chain"
			}
			p = &certFilePath{Path: b.FilePath, BundleType: bundleType}
			paths[b.FilePath] = p
		}
		p.NodeCount++
	}

	var out []certFilePath
	for _, p := range paths {
		out = append(out, *p)
	}
	return out
}
