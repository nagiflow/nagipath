package api

import (
	"context"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// certificateService implements pb.CertificateServiceServer
// (proto/nagipath/api/v1/certificates.proto). Ports getCertificates/
// getCertificate (formerly certificates.go); filterCertificates,
// certsInCluster, distinctIssuers and buildCertSummary stay in
// certificates.go since getCertificatesCSV still needs them too.
type certificateService struct {
	pb.UnimplementedCertificateServiceServer
	s *Server
}

func (c *certificateService) ListCertificates(ctx context.Context, req *pb.ListCertificatesRequest) (*pb.CertificatesListResponse, error) {
	s := c.s
	list, err := s.DB.Certificates(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	list = s.filterCertificates(ctx, list, req.Expires, req.Issuer, req.Cluster, req.IncludeCas)

	clusters, _ := s.DB.Clusters(ctx)
	resp := &pb.CertificatesListResponse{
		Expires: req.Expires, Issuer: req.Issuer, Cluster: req.Cluster, IncludeCas: req.IncludeCas,
		Issuers: distinctIssuers(list), Summary: buildCertSummary(list),
	}
	for _, cl := range clusters {
		resp.Clusters = append(resp.Clusters, &pb.DriftClusterOption{Id: cl.ID, Name: cl.Name, Members: int32(cl.Members)})
	}
	for _, ct := range list {
		resp.List = append(resp.List, &pb.CertificateListItem{
			Id: ct.ID, Fingerprint: ct.Fingerprint, SubjectCn: ct.SubjectCN, IssuerDn: ct.IssuerDN,
			NotBefore: ct.NotBefore, NotAfter: ct.NotAfter, IsCa: ct.IsCA, Bindings: int32(ct.Bindings),
			Hosts: ct.Hosts, Serves: ct.Serves,
			Sans: ct.SANs, KeyAlgorithm: ct.KeyAlgorithm, KeyBits: int32(ct.KeyBits.Int64),
		})
	}
	return resp, nil
}

func (c *certificateService) GetCertificate(ctx context.Context, req *pb.GetCertificateRequest) (*pb.CertificateDetailResponse, error) {
	s := c.s
	cert, err := s.DB.CertificateByID(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.NotFound, "No such certificate.")
	}
	bindings, _ := s.DB.CertBindings(ctx, req.Id)
	filePaths := buildFilePathStats(bindings)

	activeTab := req.Tab
	if activeTab == "" {
		activeTab = "overview"
	}

	resp := &pb.CertificateDetailResponse{
		Cert: &pb.Certificate{
			Id: cert.ID, Fingerprint: cert.Fingerprint, SubjectCn: cert.SubjectCN, SubjectDn: cert.SubjectDN,
			Sans: cert.SANs, IssuerDn: cert.IssuerDN, Serial: cert.Serial, NotBefore: cert.NotBefore,
			NotAfter: cert.NotAfter, KeyAlgorithm: cert.KeyAlgorithm, KeyBits: int32(cert.KeyBits.Int64),
			SigAlgorithm: cert.SigAlgorithm, SelfSigned: cert.SelfSigned, IsCa: cert.IsCA,
			FirstSeen: cert.FirstSeenAt, LastSeen: cert.LastSeenAt,
		},
		ActiveTab: activeTab, BindingCount: int32(len(bindings)), FileCount: int32(len(filePaths)),
	}
	for _, b := range bindings {
		resp.Bindings = append(resp.Bindings, &pb.CertBinding{
			InstanceId: b.InstanceID, Instance: b.Instance, NodeId: b.NodeID, Node: b.Node, ClusterName: b.ClusterName,
			SnapshotId: b.SnapshotID, FileId: b.FileID, FilePath: b.FilePath, SiteNames: b.SiteNames,
			Port: int32(b.Port), CombinedPem: b.CombinedPEM,
		})
	}
	for _, fp := range filePaths {
		resp.FilePaths = append(resp.FilePaths, &pb.CertFilePath{Path: fp.Path, NodeCount: int32(fp.NodeCount), BundleType: fp.BundleType})
	}
	return resp, nil
}
