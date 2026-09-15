package api

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// nodeService implements pb.NodeServiceServer (proto/nagipath/api/v1/nodes.proto).
// Ports every nodes.go handler and postImportNodes (formerly inventory.go);
// nodeBuild and its toProto/toPBNode/... conversions, and the per-tab
// loaders, stay in nodes.go — inventory.go keeps its parsing helpers.
type nodeService struct {
	pb.UnimplementedNodeServiceServer
	s *Server
}

func (c *nodeService) ListNodes(ctx context.Context, req *pb.ListNodesRequest) (*pb.NodesListResponse, error) {
	s := c.s
	nodes, err := s.DB.NodesAggregated(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}

	q := strings.TrimSpace(req.Q)
	if q != "" {
		nodes = filterNodeRows(nodes, q)
	}

	total := len(nodes)
	threshold := s.DB.SettingInt(ctx, "quarantine_after_failures")
	resp := &pb.NodesListResponse{Query: q, Total: int32(total), Threshold: int32(threshold)}
	for _, n := range nodes {
		resp.Pending += int32(n.PendingHostKeys)
		if store.Quarantined(n.ConsecutiveFailures, threshold) {
			resp.Quarantined++
		}
		if !n.LastCollection.Valid {
			resp.NeverCollected++
		}
	}
	if total > listCap {
		nodes = nodes[:listCap]
	}
	for _, n := range nodes {
		resp.Nodes = append(resp.Nodes, &pb.NodeListRow{
			Id: n.ID, Address: n.Address, SshPort: int32(n.SSHPort), DisplayName: n.DisplayName,
			OsFamily: n.OSFamily, Enabled: n.Enabled, ConsecutiveFailures: int32(n.ConsecutiveFailures),
			InstanceCount: int32(n.InstanceCount), PendingHostKeys: int32(n.PendingHostKeys),
			LastCollection: n.LastCollection.String, LastStatus: n.LastStatus.String,
			Vendor: n.Vendor.String, Version: n.Version.String, Cluster: n.Cluster.String,
			ProcessCount: int32(n.ProcessCount), Listeners: n.Listeners.String, LastCaptured: n.LastCaptured.String,
		})
	}
	return resp, nil
}

func (c *nodeService) AddNode(ctx context.Context, req *pb.AddNodeRequest) (*pb.IdResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	address := strings.TrimSpace(req.Address)
	if address == "" {
		return nil, status.Error(codes.InvalidArgument, "An address is required.")
	}
	if strings.Contains(address, "/") || strings.Contains(address, "-") && strings.Count(address, ".") == 3 {
		return nil, status.Error(codes.InvalidArgument, "nagipath does not scan networks; add one host at a time.")
	}
	port := 22
	if req.Port > 0 {
		port = int(req.Port)
	}
	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		name = address
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = "nagipath"
	}
	var credID *int64
	if req.CredentialId > 0 {
		credID = &req.CredentialId
	}
	var bastionID *int64
	if req.BastionNodeId > 0 {
		bastionID = &req.BastionNodeId
	}
	u := userOf(ctx)
	id, err := c.s.DB.AddNode(ctx, address, port, name, username, credID, bastionID, "manual", &u.ID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &pb.IdResponse{Ok: true, Id: id}, nil
}

// ImportNodes is bulk entry, not discovery: every host in the text was named
// by a person, and anything that would expand into hosts nobody named is
// refused (inventory.go's parseInventory).
func (c *nodeService) ImportNodes(ctx context.Context, req *pb.ImportNodesRequest) (*pb.ImportNodesResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	u := userOf(ctx)
	hosts, refused := parseInventory(req.Inventory)
	if len(hosts) == 0 && len(refused) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no hosts in that text")
	}
	if req.CredentialId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "select a credential before adding nodes")
	}
	credID := req.CredentialId
	var bastionID *int64
	if req.BastionNodeId > 0 {
		bastionID = &req.BastionNodeId
	}

	var added []*pb.ImportedNode
	for _, h := range hosts {
		id, err := s.DB.AddNode(ctx, h.Address, h.Port, h.Name, h.User, &credID, bastionID, "ansible_inventory", &u.ID)
		if err != nil {
			refused = append(refused, h.label()+": "+err.Error())
			continue
		}
		added = append(added, &pb.ImportedNode{
			Id:          id,
			DisplayName: h.Name,
			Address:     h.Address,
			SshPort:     int32(h.Port),
		})
	}
	s.DB.AuditDetail(ctx, &u.ID, "node.import", "node", nil,
		fmt.Sprintf("%d added, %d refused", len(added), len(refused)),
		map[string]any{"added": len(added), "refused": refused}, "success", remoteAddrOf(ctx))

	return &pb.ImportNodesResponse{Added: added, Refused: refused}, nil
}

// GetNode ports internal/web/nodes.go's nodeDetail() and its per-tab
// loaders, unchanged in structure: which process is selected, which tab's
// data is loaded, the collection-running flag React polls on.
func (c *nodeService) GetNode(ctx context.Context, req *pb.GetNodeRequest) (*pb.NodeDetailResponse, error) {
	s := c.s
	nodeID, tab := req.Id, req.Tab

	n, err := s.DB.Node(ctx, nodeID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "No such node.")
	}

	nb := nodeBuild{Node: n, Tab: tab, Threshold: s.DB.SettingInt(ctx, "quarantine_after_failures")}
	if userOf(ctx).IsAdmin() {
		nb.Credentials, _ = s.DB.Credentials(ctx)
	}
	nb.HostKeys, _ = s.DB.HostKeys(ctx, nodeID)

	allPending, _ := s.DB.PendingHostKeys(ctx)
	for _, k := range allPending {
		if k.NodeID == nodeID {
			nb.PendingKeys = append(nb.PendingKeys, k)
		}
	}

	nb.Instances, err = s.DB.NodeProcesses(ctx, nodeID)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}

	if n.CredentialID.Valid {
		creds, _ := s.DB.Credentials(ctx)
		for _, cr := range creds {
			if cr.ID == n.CredentialID.Int64 {
				nb.CredentialName = cr.Name
				break
			}
		}
	}
	if n.BastionNodeID.Valid {
		if bastion, err := s.DB.Node(ctx, n.BastionNodeID.Int64); err == nil {
			nb.BastionName = bastion.DisplayName
		}
	}

	processID := req.Process
	if processID == 0 && len(nb.Instances) > 0 {
		processID = nb.Instances[0].ID
	}
	for i := range nb.Instances {
		if nb.Instances[i].ID == processID {
			nb.Selected = &nb.Instances[i]
			break
		}
	}
	if nb.Selected == nil && len(nb.Instances) > 0 {
		nb.Selected = &nb.Instances[0]
	}

	if nb.Selected != nil {
		snap, err := s.DB.CurrentSnapshot(ctx, nb.Selected.ID)
		if err == nil {
			nb.Snapshot = snap
			nb.Stats, err = s.DB.NodeStats(ctx, nb.Selected.ID)
			if err != nil {
				return nil, status.Error(codes.Unavailable, err.Error())
			}

			if tab == "routes" || tab == "sites" || tab == "" {
				inst, err := trace.LoadInstance(ctx, s.DB, nb.Selected.ID)
				if err == nil && inst != nil {
					nb.Inst = inst
				}
			}

			var tabErr error
			switch tab {
			case "routes":
				loadRoutesTab(&nb, req.Site, req.Route)
			case "upstreams":
				tabErr = s.loadUpstreamsTab(ctx, &nb, snap.ID, req.Pool)
			case "certificates":
				tabErr = s.loadCertificatesTab(ctx, &nb, snap.ID)
			case "files":
				tabErr = s.loadFilesTab(ctx, &nb, snap.ID, req.File, req.B)
			case "drift":
				tabErr = s.loadDriftTab(ctx, &nb)
			}
			if tabErr != nil {
				return nil, status.Error(codes.Unavailable, tabErr.Error())
			}
		}
	}

	base := fmt.Sprintf("/nodes/%d", nodeID)
	nb.Tabs = []nodeTab{{Label: "Overview", Href: base, On: tab == ""}}
	if nb.Selected != nil {
		process := fmt.Sprintf("?process=%d", nb.Selected.ID)
		nb.Tabs = []nodeTab{
			{Label: "Overview", Href: base + process, On: tab == ""},
			{Label: "Sites", Href: base + "/sites" + process, Count: nb.Selected.SiteCount, On: tab == "sites"},
			{Label: "Routes", Href: base + "/routes" + process, Count: nb.Selected.RouteCount, On: tab == "routes"},
			{Label: "Upstreams", Href: base + "/upstreams" + process, On: tab == "upstreams"},
			{Label: "Certificates", Href: base + "/certificates" + process, Count: nb.Selected.CertCount, On: tab == "certificates"},
			{Label: "Config files", Href: base + "/files" + process, On: tab == "files"},
			{Label: "Drift", Href: base + "/drift" + process, On: tab == "drift"},
		}
	}

	cols, _ := s.DB.Collections(ctx, 10)
	for _, col := range cols {
		if col.NodeID == nodeID && col.Status == "running" {
			nb.Running = true
			break
		}
	}

	return nb.toProto(), nil
}

func (c *nodeService) CollectNode(ctx context.Context, req *pb.NodeIdRequest) (*pb.Ok, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	id := req.Id
	u := userOf(ctx)
	s.DB.Audit(ctx, &u.ID, "collection.start", "node", &id, "")
	actor := u.ID
	go func() {
		collectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := s.Collector.Node(collectCtx, id, "manual", &actor); err != nil {
			s.Log.Warn("collection failed", "node", id, "err", err)
		}
	}()
	// ponytail: this loses the 202-vs-200 distinction api_conventions.md
	// documents for a long-running mutation — grpc-gateway's success path is
	// always 200. Not worth a custom ForwardResponseOption for one endpoint;
	// nothing (this SPA included) branches on 202 specifically.
	return &pb.Ok{Ok: true}, nil
}

func (c *nodeService) DeleteNode(ctx context.Context, req *pb.NodeIdRequest) (*pb.Ok, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	u := userOf(ctx)
	if err := c.s.DB.DeleteNode(ctx, req.Id, &u.ID); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &pb.Ok{Ok: true}, nil
}

func (c *nodeService) ChangeNodeCredential(ctx context.Context, req *pb.ChangeNodeCredentialRequest) (*pb.Ok, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	if req.CredentialId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "Select a credential.")
	}
	u := userOf(ctx)
	if err := c.s.DB.SetNodeCredential(ctx, req.Id, req.CredentialId, &u.ID); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &pb.Ok{Ok: true}, nil
}

// ChangeNodeBastion sets or clears the node this node's connections tunnel
// through — the same underlying node.bastion_node_id sshx.go's dialer already
// follows, just not settable from the API until now.
func (c *nodeService) ChangeNodeBastion(ctx context.Context, req *pb.ChangeNodeBastionRequest) (*pb.Ok, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	var bastionID *int64
	if req.BastionNodeId > 0 {
		bastionID = &req.BastionNodeId
	}
	u := userOf(ctx)
	if err := c.s.DB.SetNodeBastion(ctx, req.Id, bastionID, &u.ID); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &pb.Ok{Ok: true}, nil
}

func (c *nodeService) DecideHostKey(ctx context.Context, req *pb.DecideHostKeyRequest) (*pb.DecideHostKeyResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	u := userOf(ctx)
	approve := req.Decision == "approve"
	if err := c.s.DB.DecideHostKey(ctx, req.Id, approve, &u.ID); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &pb.DecideHostKeyResponse{Ok: true, Approved: approve}, nil
}

// TestNodeConnection opens the same connection the collector would (and runs
// its cheapest check, Uname + a sudo probe) without collecting anything —
// Import inventory's step 2 and a node's own "Retry" both call this rather
// than running a full collection just to prove a host is reachable.
func (c *nodeService) TestNodeConnection(ctx context.Context, req *pb.NodeIdRequest) (*pb.TestConnectionResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	start := time.Now()
	host, err := s.Collector.Dialer.Connect(ctx, req.Id)
	if err != nil {
		resp := &pb.TestConnectionResponse{Status: "failed", Error: err.Error()}
		// CheckHostKey (sshx's HostKeyCallback) records an unrecognized key as
		// pending as a side effect of the failed dial above, so the row is
		// already there to read back — no separate write from this handler.
		pending, _ := s.DB.PendingHostKeys(ctx)
		for _, k := range pending {
			if k.NodeID != req.Id {
				continue
			}
			resp.Status, resp.Error = "host_key_pending", ""
			resp.PendingKey = &pb.HostKey{
				Id: k.ID, NodeId: k.NodeID, Algorithm: k.Algorithm, Fingerprint: k.Fingerprint,
				State: k.State, FirstSeenAt: k.FirstSeenAt,
			}
			resp.PreviousFingerprint = k.Previous
			break
		}
		return resp, nil
	}
	defer host.Close()
	checkErr := host.Check(ctx)
	latency := int32(time.Since(start).Milliseconds())
	if checkErr != nil {
		return &pb.TestConnectionResponse{Status: "failed", Error: checkErr.Error(), LatencyMs: latency}, nil
	}
	osFamily, _ := host.Facts()
	return &pb.TestConnectionResponse{Status: "connected", LatencyMs: latency, OsFamily: osFamily}, nil
}

// maxLiveFileBytes caps both directions of the edit path. A config file is a
// text file an operator reads; anything past this is a log, a binary or a
// mistake, and reading it into a browser textarea helps nobody.
const maxLiveFileBytes = 1 << 20

// checkLivePath is the whole of the path policy: absolute, clean, no traversal.
// Which files are readable and writable at all is the node's own business —
// the SSH credential's permissions and its sudoers grant decide that, and
// nagipath deliberately does not keep a second allowlist that would drift from
// them.
func checkLivePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || !strings.HasPrefix(p, "/") || p != path.Clean(p) {
		return "", status.Error(codes.InvalidArgument, "Give an absolute, normalized file path.")
	}
	return p, nil
}

// GetNodeFile reads a file off the Node as it is now. The Snapshot viewer shows
// what a Collection captured; this is the other thing — the current bytes, which
// is what an edit has to be based on.
func (c *nodeService) GetNodeFile(ctx context.Context, req *pb.GetNodeFileRequest) (*pb.NodeFileResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	p, err := checkLivePath(req.Path)
	if err != nil {
		return nil, err
	}
	host, err := c.s.Collector.Dialer.Connect(ctx, req.NodeId)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	defer host.Close()
	body, truncated, err := host.ReadFile(ctx, p, maxLiveFileBytes, false)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return &pb.NodeFileResponse{Path: p, Body: string(body), Truncated: truncated}, nil
}

// WriteNodeFile replaces a file on the Node, leaving the previous content in a
// timestamped backup beside it. It deliberately does not run the vendor's own
// config test afterwards: the operator restarts the Instance when they are
// ready, and that is where a bad config surfaces, with the vendor's own words.
func (c *nodeService) WriteNodeFile(ctx context.Context, req *pb.WriteNodeFileRequest) (*pb.WriteNodeFileResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	p, err := checkLivePath(req.Path)
	if err != nil {
		return nil, err
	}
	if len(req.Body) > maxLiveFileBytes {
		return nil, status.Errorf(codes.InvalidArgument, "That file is larger than the %d KB edit limit.", maxLiveFileBytes/1024)
	}
	host, err := c.s.Collector.Dialer.Connect(ctx, req.NodeId)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	defer host.Close()

	u := userOf(ctx)
	backup, err := host.WriteFile(ctx, p, req.Body)
	if err != nil {
		c.s.DB.AuditDetail(ctx, &u.ID, "node.file.write", "node", &req.NodeId, p,
			map[string]any{"path": p, "error": err.Error()}, "failed", remoteAddrOf(ctx))
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	c.s.DB.AuditDetail(ctx, &u.ID, "node.file.write", "node", &req.NodeId, p,
		map[string]any{"path": p, "bytes": len(req.Body), "backup": backup}, "ok", remoteAddrOf(ctx))
	return &pb.WriteNodeFileResponse{Ok: true, BackupPath: backup}, nil
}

// effectiveRestartCommand is the operator's override, or the one default that
// can be derived honestly: a systemd unit nagipath already discovered. Nothing
// is guessed from the vendor name — "nginx" is not reliably the unit name, and
// a wrong guess restarts the wrong process.
func effectiveRestartCommand(in store.Instance) string {
	if cmd := strings.TrimSpace(in.RestartCommand); cmd != "" {
		return cmd
	}
	if in.ServiceManager == "systemd" && in.UnitName != "" {
		return "systemctl restart " + in.UnitName
	}
	return ""
}

// RestartInstance runs one restart line against the Node the Instance sits on.
// A non-zero exit is returned as data, not as an error: "systemctl says the
// unit failed" is the answer the operator came for.
func (c *nodeService) RestartInstance(ctx context.Context, req *pb.RestartInstanceRequest) (*pb.RestartInstanceResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	in, err := s.DB.Instance(ctx, req.InstanceId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "No such process.")
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		command = effectiveRestartCommand(in)
	}
	if command == "" {
		return nil, status.Error(codes.InvalidArgument,
			"No restart command for this process — nagipath only derives one for a systemd unit it discovered. Enter the command to run.")
	}
	u := userOf(ctx)
	if command != strings.TrimSpace(in.RestartCommand) {
		if err := s.DB.SetInstanceRestartCommand(ctx, in.ID, command, &u.ID); err != nil {
			return nil, status.Error(codes.Unavailable, err.Error())
		}
	}

	host, err := s.Collector.Dialer.Connect(ctx, in.NodeID)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	defer host.Close()
	res, err := host.Restart(ctx, command)
	if err != nil {
		s.DB.AuditDetail(ctx, &u.ID, "instance.restart", "instance", &in.ID, in.DisplayName,
			map[string]any{"command": command, "error": err.Error()}, "failed", remoteAddrOf(ctx))
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	outcome := "ok"
	if res.ExitCode != 0 {
		outcome = "failed"
	}
	s.DB.AuditDetail(ctx, &u.ID, "instance.restart", "instance", &in.ID, in.DisplayName,
		map[string]any{"command": command, "exit_code": res.ExitCode}, outcome, remoteAddrOf(ctx))
	return &pb.RestartInstanceResponse{
		Ok: res.ExitCode == 0, Command: command,
		Stdout: res.Stdout, Stderr: res.Stderr, ExitCode: int32(res.ExitCode),
	}, nil
}
