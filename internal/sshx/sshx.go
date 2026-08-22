// Package sshx is the only place in nagipath that talks to a managed host.
//
// Three invariants:
//   - Every command comes from the closed enum in command.go.
//   - No command runs until the Node's SSH host key has been explicitly
//     approved by an operator. There is no trust-on-first-use.
//   - Nothing is written to the target. There is no upload path, no `-w`, no
//     shell that outlives a single command.
package sshx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nagiflow/nagipath/internal/keys"
	"github.com/nagiflow/nagipath/internal/store"
)

const maxBastionHops = 4

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Executor is the seam the collector, prober and tests share. The fake in
// fake.go replays fixtures; the real one runs SSH.
type Executor interface {
	Run(ctx context.Context, c Command) (Result, error)
	ReadFile(ctx context.Context, path string, max int64, sudo bool) (data []byte, truncated bool, err error)
	Check(ctx context.Context) error
	Close() error
}

type Dialer struct {
	DB      *store.DB
	Master  *keys.Master
	Timeout time.Duration
}

// Client is a live SSH connection to one Node, plus the bastion chain that got
// us there. Closing it closes the whole chain, newest first.
type Client struct {
	conn    *ssh.Client
	parents []*ssh.Client
	node    store.Node
	timeout time.Duration
}

func (d *Dialer) Connect(ctx context.Context, nodeID int64) (*Client, error) {
	return d.connect(ctx, nodeID, 0)
}

func (d *Dialer) connect(ctx context.Context, nodeID int64, depth int) (*Client, error) {
	if depth > maxBastionHops {
		return nil, fmt.Errorf("bastion chain exceeds %d hops; refusing to continue", maxBastionHops)
	}
	node, err := d.DB.Node(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if !node.Enabled {
		return nil, fmt.Errorf("node %s is disabled", node.DisplayName)
	}

	credID, err := d.DB.ResolveCredential(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("node %s: %w", node.DisplayName, err)
	}
	credUser, signer, err := d.DB.Signer(ctx, d.Master, credID)
	if err != nil {
		return nil, err
	}
	user := credUser
	if node.SSHUsername.Valid && node.SSHUsername.String != "" {
		user = node.SSHUsername.String
	}

	timeout := d.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: d.hostKeyCallback(ctx, nodeID),
		Timeout:         timeout,
		// Key-based and certificate authentication only. Password auth is not
		// offered anywhere in this package.
	}

	addr := net.JoinHostPort(node.Address, strconv.Itoa(node.SSHPort))

	var parents []*ssh.Client
	var conn *ssh.Client
	if node.BastionNodeID.Valid {
		bastion, err := d.connect(ctx, node.BastionNodeID.Int64, depth+1)
		if err != nil {
			return nil, fmt.Errorf("via bastion: %w", err)
		}
		parents = append(bastion.parents, bastion.conn)
		tunnel, err := bastion.conn.DialContext(ctx, "tcp", addr)
		if err != nil {
			closeAll(parents)
			return nil, fmt.Errorf("tunnel to %s: %w", addr, err)
		}
		c, chans, reqs, err := ssh.NewClientConn(tunnel, addr, cfg)
		if err != nil {
			tunnel.Close()
			closeAll(parents)
			return nil, err
		}
		conn = ssh.NewClient(c, chans, reqs)
	} else {
		dialer := net.Dialer{Timeout: timeout}
		raw, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("dial %s: %w", addr, err)
		}
		c, chans, reqs, err := ssh.NewClientConn(raw, addr, cfg)
		if err != nil {
			raw.Close()
			return nil, err
		}
		conn = ssh.NewClient(c, chans, reqs)
	}
	return &Client{conn: conn, parents: parents, node: node, timeout: timeout}, nil
}

// hostKeyCallback records an unknown key as pending and refuses the connection.
// The refusal is the feature.
func (d *Dialer) hostKeyCallback(ctx context.Context, nodeID int64) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		return d.DB.CheckHostKey(ctx, nodeID, key.Type(),
			strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))),
			ssh.FingerprintSHA256(key))
	}
}

func closeAll(cs []*ssh.Client) {
	for i := len(cs) - 1; i >= 0; i-- {
		cs[i].Close()
	}
}

func (c *Client) Close() error {
	err := c.conn.Close()
	closeAll(c.parents)
	return err
}

func (c *Client) Node() store.Node { return c.node }

func (c *Client) Run(ctx context.Context, cmd Command) (Result, error) {
	res, err := c.run(ctx, cmd)
	// The same escalation ReadFile does, for the vendor dumps: try as the login
	// user first, and only reach for sudo when that was not enough. A dump that
	// failed for want of privilege would otherwise be indistinguishable from a
	// host with no configuration.
	if err == nil && res.ExitCode != 0 && cmd.SudoRetry && !cmd.Sudo && c.node.SudoAvailable {
		cmd.Sudo = true
		if sudoRes, sudoErr := c.run(ctx, cmd); sudoErr == nil && sudoRes.ExitCode == 0 {
			return sudoRes, nil
		}
	}
	return res, err
}

func (c *Client) run(ctx context.Context, cmd Command) (Result, error) {
	if err := cmd.validate(); err != nil {
		return Result{}, err
	}
	line := cmd.Line
	if cmd.Sudo {
		if !c.node.SudoAvailable {
			return Result{}, fmt.Errorf("command %s needs sudo, which is not available on %s",
				cmd.ID, c.node.DisplayName)
		}
		// No `sh -c` wrapper. A per-command sudoers rule can never match a shell
		// invocation, so wrapping would force the grant to be `sh -c *` — which is
		// root, and would make the narrow grant the product documents a fiction.
		// validate() guarantees the line is a single command with no shell operators.
		line = "sudo -n " + line
	}

	sess, err := c.conn.NewSession()
	if err != nil {
		return Result{}, err
	}
	defer sess.Close()

	var out, errBuf bytes.Buffer
	sess.Stdout = &out
	sess.Stderr = &errBuf

	done := make(chan error, 1)
	go func() { done <- sess.Run(line) }()

	timer := time.NewTimer(c.timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		sess.Signal(ssh.SIGKILL)
		return Result{}, ctx.Err()
	case <-timer.C:
		sess.Signal(ssh.SIGKILL)
		return Result{}, fmt.Errorf("command %s timed out after %s", cmd.ID, c.timeout)
	case err := <-done:
		res := Result{Stdout: out.String(), Stderr: errBuf.String()}
		var ee *ssh.ExitError
		switch {
		case err == nil:
		case errors.As(err, &ee):
			res.ExitCode = ee.ExitStatus()
			if !cmd.TolerateExit {
				return res, fmt.Errorf("command %s exited %d: %s",
					cmd.ID, res.ExitCode, firstLine(res.Stderr))
			}
		default:
			return res, err
		}
		return res, nil
	}
}

// ReadFile reads a config file, retrying with sudo when a plain read is denied.
// The retry exists because customer fleets routinely have root-only include
// files, and reporting them as absent would silently understate a config.
func (c *Client) ReadFile(ctx context.Context, path string, max int64, sudo bool) ([]byte, bool, error) {
	res, err := c.Run(ctx, FileRead(path, max, sudo))
	if err != nil {
		return nil, false, err
	}
	if res.ExitCode != 0 && !sudo && c.node.SudoAvailable {
		res, err = c.Run(ctx, FileRead(path, max, true))
		if err != nil {
			return nil, false, err
		}
	}
	if res.ExitCode != 0 {
		return nil, false, fmt.Errorf("read %s: %s", path, firstLine(res.Stderr))
	}
	data := []byte(res.Stdout)
	if int64(len(data)) > max {
		return data[:max], true, nil
	}
	return data, false, nil
}

// Check is the cheapest proof that the connection works and tells us the two
// facts every later command depends on: the OS family and whether sudo is
// available without a password.
func (c *Client) Check(ctx context.Context) error {
	un, err := c.Run(ctx, Uname())
	if err != nil {
		return err
	}
	osFamily := "unknown"
	if strings.HasPrefix(strings.ToLower(un.Stdout), "linux") {
		osFamily = "linux"
	}
	sudoRes, err := c.Run(ctx, SudoCheck())
	if err != nil {
		return err
	}
	c.node.SudoAvailable = sudoRes.ExitCode == 0
	c.node.OSFamily = osFamily
	return nil
}

// Facts reports what Check learned, so the caller can persist it.
func (c *Client) Facts() (osFamily string, sudo bool) {
	return c.node.OSFamily, c.node.SudoAvailable
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	if s == "" {
		return "no error output"
	}
	return s
}
