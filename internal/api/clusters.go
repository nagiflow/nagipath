package api

// getClusters and postRenameCluster moved to clusterservice.go as
// ClusterService's ListClusters and RenameCluster RPCs (docs/adr/0018,
// proto/nagipath/api/v1/clusters.proto) — routed by their google.api.http
// options instead of a net/http registration here.
