package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// settingsService implements pb.SettingsServiceServer
// (proto/nagipath/api/v1/settings.proto). Ports every settings.go handler
// except getAuditCSV, which stays a plain http.HandlerFunc (gateway.go's
// gatewayOrCSV, wired in api.go).
type settingsService struct {
	pb.UnimplementedSettingsServiceServer
	s *Server
}

func (c *settingsService) GetCredentials(ctx context.Context, req *pb.GetCredentialsRequest) (*pb.CredentialsResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	kinds, err := s.DB.CredentialAuthKinds(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	list, err := s.DB.CredentialsWithUsage(ctx, req.Type)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	resp := &pb.CredentialsResponse{AuthKinds: kinds}
	for _, cr := range list {
		resp.Credentials = append(resp.Credentials, &pb.CredentialItem{
			Id: cr.ID, Name: cr.Name, Username: cr.Username, AuthKind: cr.AuthKind,
			PublicKey: cr.PublicKey, Fingerprint: cr.Fingerprint, ExternalRef: cr.ExternalRef,
			CreatedAt: cr.CreatedAt, NodeCount: int32(cr.NodeCount), LastUsed: cr.LastUsed,
		})
	}
	return resp, nil
}

func (c *settingsService) AddCredential(ctx context.Context, req *pb.AddCredentialRequest) (*pb.IdResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "A credential name is required.")
	}
	kind := req.AuthKind
	if kind == "" {
		kind = "private_key"
	}
	u := userOf(ctx)
	var (
		id  int64
		err error
	)
	switch kind {
	case "private_key", "ssh_certificate":
		if req.PrivateKey == "" {
			return nil, status.Error(codes.InvalidArgument, "A private key is required.")
		}
		if kind == "ssh_certificate" && req.Certificate == "" {
			return nil, status.Error(codes.InvalidArgument, "An SSH certificate is required.")
		}
		id, err = s.DB.CreateCredential(ctx, s.Master, req.Name, req.Username, kind, req.PrivateKey,
			req.Passphrase, req.Certificate, &u.ID)
	case "username_password", "kerberos":
		id, err = s.DB.CreatePasswordCredential(ctx, s.Master, req.Name, req.Username, kind,
			req.Password, req.ExternalRef, &u.ID)
	case "cyberark":
		id, err = s.DB.CreateCyberArkCredential(ctx, req.Name, req.Username, req.ExternalRef, &u.ID)
	default:
		return nil, status.Error(codes.InvalidArgument, "Choose a supported credential type.")
	}
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	_ = s.DB.Audit(ctx, &u.ID, "credential.create", "credential", &id, req.Name)
	return &pb.IdResponse{Ok: true, Id: id}, nil
}

// UpdateCredential is the rotate/fix-a-typo path: same fields as AddCredential
// minus the kind, with every secret optional (blank keeps what is sealed).
func (c *settingsService) UpdateCredential(ctx context.Context, req *pb.UpdateCredentialRequest) (*pb.Ok, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	u := userOf(ctx)
	if err := c.s.DB.UpdateCredential(ctx, c.s.Master, req.Id, req.Name, req.Username,
		req.PrivateKey, req.Passphrase, req.Certificate, req.Password, req.ExternalRef); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	_ = c.s.DB.Audit(ctx, &u.ID, "credential.update", "credential", &req.Id, req.Name)
	return &pb.Ok{Ok: true}, nil
}

func (c *settingsService) GetHostKeys(ctx context.Context, req *pb.GetHostKeysRequest) (*pb.HostKeysResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	stats, err := s.DB.HostKeyStats(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	keys, err := s.DB.AllHostKeys(ctx, req.State, req.Cluster)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	resp := &pb.HostKeysResponse{
		Stats: &pb.HostKeyStatsPB{Pending: int32(stats.Pending), Changed: int32(stats.Changed),
			Approved: int32(stats.Approved), Algorithms: map[string]int32{}},
		TofuEnabled: s.DB.SettingBool(ctx, "tofu_enabled"),
	}
	for algo, n := range stats.Algorithms {
		resp.Stats.Algorithms[algo] = int32(n)
	}
	for _, k := range keys {
		resp.Keys = append(resp.Keys, &pb.PendingHostKey{
			Key: &pb.HostKey{Id: k.ID, Algorithm: k.Algorithm,
				Fingerprint: k.Fingerprint, State: k.State, FirstSeenAt: k.FirstSeenAt},
			NodeName: k.NodeName, NodeAddress: k.NodeAddress, Previous: k.Previous,
			Cluster: k.Cluster,
		})
	}
	if clusters, err := s.DB.Clusters(ctx); err == nil {
		for _, cl := range clusters {
			resp.Clusters = append(resp.Clusters, cl.Name)
		}
	}
	return resp, nil
}

// SetHostKeyPolicy is the only setting on this page an operator can actually
// change (docs/frontend/settings.md's "no trust-on-first-use toggle" claim no
// longer holds — this is that toggle, off by default). "Refuse collection on
// key change" stays fixed: a key that would replace an already-approved one
// is always decided manually, tofu_enabled or not (store.CheckHostKey).
func (c *settingsService) SetHostKeyPolicy(ctx context.Context, req *pb.SetHostKeyPolicyRequest) (*pb.Ok, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	u := userOf(ctx)
	value := "0"
	if req.TofuEnabled {
		value = "1"
	}
	if err := s.DB.SetSetting(ctx, "tofu_enabled", value, &u.ID); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	_ = s.DB.Audit(ctx, &u.ID, "hostkey_policy.update", "setting", nil, "tofu_enabled="+value)
	return &pb.Ok{Ok: true}, nil
}

func (c *settingsService) GetMasterKey(ctx context.Context, _ *pb.Empty) (*pb.MasterKeyResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	counts, err := s.DB.MasterKeyEncryptedCounts(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	resp := &pb.MasterKeyResponse{
		Path: s.Master.Path, CredentialsEncrypted: int32(counts.Credentials),
		ApiKeysEncrypted: int32(counts.APIKeys), JobLogsEncrypted: int32(counts.JobLogs),
	}
	if filepath.IsAbs(s.Master.Path) {
		resp.DataDir = filepath.Dir(s.Master.Path)
	}
	return resp, nil
}

func (c *settingsService) GetRetention(ctx context.Context, _ *pb.Empty) (*pb.RetentionResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	stats, err := s.DB.RetentionStats(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	resp := &pb.RetentionResponse{
		SnapshotDays:   int32(s.DB.SettingInt(ctx, "snapshot_retention_days")),
		MinPerInstance: int32(s.DB.SettingInt(ctx, "snapshot_retention_min_per_instance")),
		JobLogDays:     int32(s.DB.SettingInt(ctx, "job_log_retention_days")),
		ProbeDays:      int32(s.DB.SettingInt(ctx, "probe_retention_days")),
		AuditDays:      int32(s.DB.SettingInt(ctx, "audit_retention_days")),
		Stats: &pb.RetentionStatsPB{
			IndexSizeBytes: stats.IndexSizeBytes, Snapshots: int32(stats.Snapshots),
			JobLogs: int32(stats.JobLogs), Traces: int32(stats.Traces),
		},
	}
	if p := stats.LastPrune; p != nil {
		resp.Stats.LastPrune = &pb.PruneHistoryPB{
			RanAt: p.RanAt, DurationSeconds: p.Duration, FreedBytes: p.Freed,
			Examined: &pb.PruneCountsPB{Snapshots: int32(p.Examined.Snapshots), JobLogs: int32(p.Examined.JobLogs),
				Traces: int32(p.Examined.Traces), AuditEvents: int32(p.Examined.AuditEvents)},
			Deleted: &pb.PrunedCountsPB{Snapshots: p.Deleted.Snapshots, JobLogs: p.Deleted.JobLogs,
				Probes: p.Deleted.Probes, AuditEvents: p.Deleted.AuditEvents, Blobs: p.Deleted.Blobs},
			Kept: &pb.PruneCountsPB{Snapshots: int32(p.Kept.Snapshots), JobLogs: int32(p.Kept.JobLogs),
				Traces: int32(p.Kept.Traces), AuditEvents: int32(p.Kept.AuditEvents)},
		}
	}
	return resp, nil
}

func (c *settingsService) SetRetention(ctx context.Context, req *pb.SetRetentionRequest) (*pb.Ok, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	u := userOf(ctx)
	for _, f := range []struct {
		val *int32
		key string
		max int32
	}{
		{req.SnapshotDays, "snapshot_retention_days", 3650},
		{req.MinPerInstance, "snapshot_retention_min_per_instance", 1000},
		{req.JobLogDays, "job_log_retention_days", 3650},
		{req.ProbeDays, "probe_retention_days", 3650},
		{req.AuditDays, "audit_retention_days", 3650},
	} {
		if f.val == nil {
			continue
		}
		if *f.val < 0 || *f.val > f.max {
			return nil, status.Error(codes.InvalidArgument,
				"Each window must be a whole number of days between 0 and "+strconv.Itoa(int(f.max))+".")
		}
		if err := s.DB.SetSetting(ctx, f.key, strconv.Itoa(int(*f.val)), &u.ID); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	_ = s.DB.Audit(ctx, &u.ID, "retention.update", "setting", nil, "retention windows")
	return &pb.Ok{Ok: true}, nil
}

func (c *settingsService) RunRetention(ctx context.Context, _ *pb.Empty) (*pb.RunRetentionResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	u := userOf(ctx)
	start := time.Now()
	result, err := s.DB.Prune(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	dur := time.Since(start).Seconds()
	history := store.PruneHistory{RanAt: time.Now().UTC().Format(time.RFC3339), Duration: dur, Deleted: result}
	_ = s.DB.SavePruneHistory(ctx, history, &u.ID)
	_ = s.DB.Audit(ctx, &u.ID, "retention.run", "setting", nil, "manual prune")
	return &pb.RunRetentionResponse{
		Ok: true, Snapshots: result.Snapshots, JobLogs: result.JobLogs,
		Probes: result.Probes, AuditEvents: result.AuditEvents, Blobs: result.Blobs,
	}, nil
}

func (c *settingsService) GetCollectionDefaults(ctx context.Context, _ *pb.Empty) (*pb.CollectionDefaultsResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	creds, err := s.DB.Credentials(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	defaultCred, _ := strconv.ParseInt(s.DB.Setting(ctx, "default_credential_id"), 10, 64)
	resp := &pb.CollectionDefaultsResponse{
		DefaultCredential: defaultCred,
		IntervalMinutes:   int32(s.DB.SettingInt(ctx, "collection_interval_seconds") / 60),
		JitterSeconds:     int32(s.DB.SettingInt(ctx, "collection_jitter_seconds")),
		SshWorkers:        int32(s.DB.SettingInt(ctx, "ssh_workers")),
		CommandTimeout:    int32(s.DB.SettingInt(ctx, "ssh_command_timeout_seconds")),
		MaxFiles:          int32(s.DB.SettingInt(ctx, "snapshot_max_files")),
		ProbeRedirects:    int32(s.DB.SettingInt(ctx, "probe_max_redirects")),
		ProbeLookback:     int32(s.DB.SettingInt(ctx, "probe_log_lookback_seconds")),
	}
	for _, cr := range creds {
		resp.Credentials = append(resp.Credentials, &pb.CredentialItem{
			Id: cr.ID, Name: cr.Name, Username: cr.Username, AuthKind: cr.AuthKind, CreatedAt: cr.CreatedAt,
		})
	}
	return resp, nil
}

func (c *settingsService) SetCollectionDefaults(ctx context.Context, req *pb.SetCollectionDefaultsRequest) (*pb.Ok, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	u := userOf(ctx)
	for _, f := range []struct {
		val      *int32
		key      string
		min, max int32
		mult     int32
	}{
		{req.IntervalMinutes, "collection_interval_seconds", 1, 10080, 60},
		{req.JitterSeconds, "collection_jitter_seconds", 0, 3600, 1},
		{req.SshWorkers, "ssh_workers", 1, 64, 1},
		{req.CommandTimeout, "ssh_command_timeout_seconds", 5, 600, 1},
		{req.MaxFiles, "snapshot_max_files", 10, 100000, 1},
		{req.ProbeRedirects, "probe_max_redirects", 0, 20, 1},
		{req.ProbeLookback, "probe_log_lookback_seconds", 10, 3600, 1},
	} {
		if f.val == nil {
			continue
		}
		if *f.val < f.min || *f.val > f.max {
			return nil, status.Error(codes.InvalidArgument, f.key+" is outside its allowed range.")
		}
		if err := s.DB.SetSetting(ctx, f.key, strconv.Itoa(int(*f.val*f.mult)), &u.ID); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	if req.DefaultCredential != nil {
		if *req.DefaultCredential < 0 {
			return nil, status.Error(codes.InvalidArgument, "Default credential is invalid.")
		}
		if err := s.DB.SetSetting(ctx, "default_credential_id", strconv.FormatInt(*req.DefaultCredential, 10), &u.ID); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	_ = s.DB.Audit(ctx, &u.ID, "collection.defaults.update", "setting", nil, "collection defaults")
	return &pb.Ok{Ok: true}, nil
}

func (c *settingsService) GetUsers(ctx context.Context, _ *pb.Empty) (*pb.UsersResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	list, err := c.s.DB.Users(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	resp := &pb.UsersResponse{}
	for _, u := range list {
		item := &pb.UserItem{Id: u.ID, Username: u.Username, Role: u.Role, DisplayName: u.DisplayName,
			MustChangePassword: u.MustChangePassword, CreatedAt: u.CreatedAt, Disabled: u.Disabled}
		if u.LastLoginAt.Valid {
			item.LastLoginAt = u.LastLoginAt.String
		}
		resp.Users = append(resp.Users, item)
	}
	return resp, nil
}

func (c *settingsService) AddUser(ctx context.Context, req *pb.AddUserRequest) (*pb.IdResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	if req.Username == "" || len(req.Password) < 12 {
		return nil, status.Error(codes.InvalidArgument, "A username and a password of at least 12 characters are required.")
	}
	if req.Password != req.Confirm {
		return nil, status.Error(codes.InvalidArgument, "The two passwords do not match.")
	}
	if req.Role != "admin" && req.Role != "viewer" {
		return nil, status.Error(codes.InvalidArgument, "Role must be admin or viewer.")
	}
	actor := userOf(ctx)
	id, err := c.s.DB.CreateUser(ctx, req.Username, req.Password, req.Role, req.Username, req.MustChange)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	_ = c.s.DB.Audit(ctx, &actor.ID, "user.create", "user", &id, req.Username)
	return &pb.IdResponse{Ok: true, Id: id}, nil
}

// setUserDisabled backs both DisableUser and EnableUser — the only
// difference between them is the boolean they set.
func (c *settingsService) setUserDisabled(ctx context.Context, id int64, disabled bool) (*pb.SetUserDisabledResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	action, word := "user.enable", "enabled"
	if disabled {
		action, word = "user.disable", "disabled"
	}
	actor := userOf(ctx)
	target, err := s.DB.User(ctx, id)
	if err != nil {
		return nil, status.Error(codes.NotFound, "User not found.")
	}
	if disabled && target.IsAdmin() && !target.Disabled {
		n, err := s.DB.EnabledAdminCount(ctx, id)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		if n == 0 {
			return nil, status.Error(codes.InvalidArgument, "Cannot disable the last enabled admin account.")
		}
	}
	if err := s.DB.SetUserDisabled(ctx, id, disabled); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	_ = s.DB.Audit(ctx, &actor.ID, action, "user", &id, target.Username)
	return &pb.SetUserDisabledResponse{Ok: true, Status: word}, nil
}

func (c *settingsService) DisableUser(ctx context.Context, req *pb.UserIdRequest) (*pb.SetUserDisabledResponse, error) {
	return c.setUserDisabled(ctx, req.Id, true)
}

func (c *settingsService) EnableUser(ctx context.Context, req *pb.UserIdRequest) (*pb.SetUserDisabledResponse, error) {
	return c.setUserDisabled(ctx, req.Id, false)
}

func (c *settingsService) GetAudit(ctx context.Context, req *pb.GetAuditRequest) (*pb.AuditResponse, error) {
	s := c.s
	filter := auditFilterFrom(req.Actor, req.Action, req.Range, int(req.Page), int(req.PerPage))
	result, err := s.DB.AuditEventsFiltered(ctx, filter)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	actions, err := s.DB.AuditActions(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	resp := &pb.AuditResponse{Total: int32(result.Total), TotalPages: int32(result.TotalPages),
		Page: int32(result.Page), PerPage: int32(result.PerPage), Actions: actions}
	for _, e := range result.Events {
		resp.Events = append(resp.Events, &pb.AuditEventItem{At: e.At, ActorLabel: e.ActorLabel, Action: e.Action,
			TargetKind: e.TargetKind, TargetLabel: e.TargetLabel, Outcome: e.Outcome, Detail: e.Detail, SourceIp: e.SourceIP})
	}
	return resp, nil
}

func (c *settingsService) GetApiKeys(ctx context.Context, req *pb.GetApiKeysRequest) (*pb.ApiKeysResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	keys, err := c.s.DB.APITokensFiltered(ctx, req.State)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	resp := &pb.ApiKeysResponse{}
	for _, k := range keys {
		item := &pb.ApiKeyItem{Id: k.ID, Name: k.Name, Username: k.Username, CreatedAt: k.CreatedAt, Prefix: k.Prefix}
		if k.LastUsedAt.Valid {
			item.LastUsedAt = k.LastUsedAt.String
		}
		if k.ExpiresAt.Valid {
			item.ExpiresAt = k.ExpiresAt.String
		}
		if k.RevokedAt.Valid {
			item.RevokedAt = k.RevokedAt.String
		}
		resp.Keys = append(resp.Keys, item)
	}
	return resp, nil
}

func (c *settingsService) CreateApiKey(ctx context.Context, req *pb.CreateApiKeyRequest) (*pb.CreateApiKeyResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "A key name is required.")
	}
	if req.ExpiresDays < 1 || req.ExpiresDays > 3650 {
		return nil, status.Error(codes.InvalidArgument, "Expiry must be between 1 and 3650 days.")
	}
	actor := userOf(ctx)
	id, token, err := c.s.DB.CreateAPIToken(ctx, req.Name, actor.ID, time.Duration(req.ExpiresDays)*24*time.Hour)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	_ = c.s.DB.Audit(ctx, &actor.ID, "api_token.create", "api_token", &id, req.Name)
	return &pb.CreateApiKeyResponse{Ok: true, Id: id, Token: token}, nil
}

func (c *settingsService) RevokeApiKey(ctx context.Context, req *pb.ApiKeyIdRequest) (*pb.Ok, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	actor := userOf(ctx)
	if err := c.s.DB.RevokeAPIToken(ctx, req.Id); err != nil {
		return nil, status.Error(codes.NotFound, "API key not found or already revoked.")
	}
	_ = c.s.DB.Audit(ctx, &actor.ID, "api_token.revoke", "api_token", &req.Id, "")
	return &pb.Ok{Ok: true}, nil
}

func (c *settingsService) GetLicense(ctx context.Context, _ *pb.Empty) (*pb.LicenseResponse, error) {
	s := c.s
	lic := s.CurrentLicense()
	licStatus, message := s.LicenseStatus(ctx)
	n, err := s.DB.NodeCount(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	state, err := s.DB.LicenseState(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	resp := &pb.LicenseResponse{Status: string(licStatus), Message: message, NodeCount: int32(n)}
	if lic != nil {
		resp.Loaded = true
		resp.Customer, resp.Edition, resp.NodeCeiling = lic.Customer, lic.Edition, int32(lic.NodeCeiling)
		resp.Expiry = lic.Expiry.Format(time.RFC3339)
	}
	if state != nil {
		resp.State = &pb.LicenseStatePB{Customer: state.Customer, Edition: state.Edition,
			NodeCeiling: int32(state.NodeCeiling), ExpiresAt: state.ExpiresAt,
			SignatureValid: state.SignatureValid, LastEvaluatedAt: state.LastEvaluatedAt,
			InstalledByUsername: state.InstalledByUsername}
	}
	return resp, nil
}

func (c *settingsService) InstallLicense(ctx context.Context, req *pb.InstallLicenseRequest) (*pb.Ok, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	u := userOf(ctx)
	lic, err := c.s.InstallLicense(ctx, []byte(req.LicenseText), &u.ID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// Customer name only — never the raw license text or its signature.
	_ = c.s.DB.Audit(ctx, &u.ID, "license.install", "license", nil, lic.Customer)
	return &pb.Ok{Ok: true}, nil
}

func (c *settingsService) GetDiagnostics(ctx context.Context, _ *pb.Empty) (*pb.DiagnosticsResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	started, listenAddr, tlsEnabled := s.Diag()
	resp := &pb.DiagnosticsResponse{
		Version: Version, GoVersion: runtime.Version(), UptimeSeconds: int64(time.Since(started).Round(time.Second).Seconds()),
		DbPath: s.DB.Path, ListenAddr: listenAddr, TlsEnabled: tlsEnabled, DemoMode: s.DemoMode,
		MasterKeyPath: s.Master.Path,
	}
	if fi, err := os.Stat(s.DB.Path); err == nil {
		resp.DbSizeBytes = fi.Size()
	}
	if fi, err := os.Stat(s.Master.Path); err == nil {
		resp.MasterKeyPresent = true
		resp.MasterKeyMode = fmt.Sprintf("%#o", fi.Mode().Perm())
	}
	licStatus, message := s.LicenseStatus(ctx)
	resp.LicenseStatus, resp.LicenseMessage = string(licStatus), message
	applied, err := s.DB.AppliedMigrations(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	resp.MigrationsApplied = int32(len(applied))
	expected, _ := store.ExpectedMigrationCount()
	resp.MigrationsExpected = int32(expected)
	return resp, nil
}
