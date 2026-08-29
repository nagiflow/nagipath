package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
)

// getCertificates ports internal/web/certificates.go's certificates(): same
// filters (?expires=, ?issuer=, ?cluster=, ?include_cas=1) and CSV export.
func (s *Server) getCertificates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	expires := q.Get("expires")
	issuer := q.Get("issuer")
	cluster := q.Get("cluster")
	includeCAs := q.Get("include_cas") == "1"

	list, err := s.DB.Certificates(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	list = s.filterCertificates(ctx, list, expires, issuer, cluster, includeCAs)

	if q.Get("export") == "csv" {
		out := [][]string{{"subject", "fingerprint_sha256", "issuer", "not_before", "not_after", "bindings", "nodes", "serves"}}
		for _, cert := range list {
			out = append(out, []string{cert.SubjectCN, cert.Fingerprint, cert.IssuerDN,
				cert.NotBefore, cert.NotAfter, strconv.Itoa(cert.Bindings), cert.Hosts,
				strings.Join(cert.Serves, ",")})
		}
		s.writeCSV(w, r, "certificates", out)
		return
	}

	clusters, _ := s.DB.Clusters(ctx)
	resp := &pb.CertificatesListResponse{
		Expires: expires, Issuer: issuer, Cluster: cluster, IncludeCas: includeCAs,
		Issuers: distinctIssuers(list), Summary: buildCertSummary(list),
	}
	for _, cl := range clusters {
		resp.Clusters = append(resp.Clusters, &pb.DriftClusterOption{Id: cl.ID, Name: cl.Name, Members: int32(cl.Members)})
	}
	for _, c := range list {
		resp.List = append(resp.List, &pb.CertificateListItem{
			Id: c.ID, Fingerprint: c.Fingerprint, SubjectCn: c.SubjectCN, IssuerDn: c.IssuerDN,
			NotBefore: c.NotBefore, NotAfter: c.NotAfter, IsCa: c.IsCA, Bindings: int32(c.Bindings),
			Hosts: c.Hosts, Serves: c.Serves,
		})
	}
	writeProto(w, http.StatusOK, resp)
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

// getCertificate ports internal/web/certificates.go's certificateDetail().
func (s *Server) getCertificate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	certID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)

	cert, err := s.DB.CertificateByID(ctx, certID)
	if err != nil {
		apiError(w, http.StatusNotFound, "not_found", "No such certificate.")
		return
	}
	bindings, _ := s.DB.CertBindings(ctx, certID)
	filePaths := buildFilePathStats(bindings)

	activeTab := r.URL.Query().Get("tab")
	if activeTab == "" {
		activeTab = "overview"
	}

	resp := &pb.CertificateDetailResponse{
		Cert: &pb.Certificate{
			Id: cert.ID, Fingerprint: cert.Fingerprint, SubjectCn: cert.SubjectCN, SubjectDn: cert.SubjectDN,
			Sans: cert.SANs, IssuerDn: cert.IssuerDN, Serial: cert.Serial, NotBefore: cert.NotBefore,
			NotAfter: cert.NotAfter, KeyAlgorithm: cert.KeyAlgorithm, KeyBits: int32(cert.KeyBits.Int64),
			SigAlgorithm: cert.SigAlgorithm, SelfSigned: cert.SelfSigned, IsCa: cert.IsCA,
		},
		ActiveTab: activeTab, BindingCount: int32(len(bindings)), FileCount: int32(len(filePaths)),
	}
	for _, b := range bindings {
		resp.Bindings = append(resp.Bindings, &pb.CertBinding{
			InstanceId: b.InstanceID, Instance: b.Instance, Node: b.Node, ClusterName: b.ClusterName,
			SnapshotId: b.SnapshotID, FileId: b.FileID, FilePath: b.FilePath, SiteNames: b.SiteNames,
			Port: int32(b.Port), CombinedPem: b.CombinedPEM,
		})
	}
	for _, fp := range filePaths {
		resp.FilePaths = append(resp.FilePaths, &pb.CertFilePath{Path: fp.Path, NodeCount: int32(fp.NodeCount), BundleType: fp.BundleType})
	}

	writeProto(w, http.StatusOK, resp)
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
