// Package sshx is the only place in nagipath that talks to a managed host.
//
// Three invariants:
//   - Every command comes from the closed enum in command.go.
//   - No command runs until the Node's SSH host key has been explicitly
//     approved — by an operator, or, only for a node's first-ever key and
//     only when the tofu_enabled setting is on, automatically (see
//     store.CheckHostKey). A key that would replace an already-approved one
//     is never auto-approved either way.
//   - The only writes are the operator-initiated ones: WriteFile (which always
//     leaves a backup beside the file it replaces) and a restart of a single
//     Instance. Both are admin-only and audited by the caller. Nothing else is
//     written, and no shell outlives a single command.
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
	WriteFile(ctx context.Context, path, body string) (backup string, err error)
	Restart(ctx context.Context, line string) (Result, error)
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
	conn         *ssh.Client
	parents      []*ssh.Client
	node         store.Node
	timeout      time.Duration
	sudoPassword string // set only for username/password credentials
}

// A password credential offers both password and keyboard-interactive: sshd
// with PAM (RHEL's default) advertises only keyboard-interactive, and OpenSSH
// silently answers its prompts with the password — which is why a node that
// accepts `ssh user@host` still failed here with "no supported methods
// remain [none password]". Same one secret either way, so this is not a
// fallback across credentials: at most two attempts, no lockout surprise.
func passwordAuth(password string) []ssh.AuthMethod {
	return []ssh.AuthMethod{
		ssh.Password(password),
		ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range answers {
				answers[i] = password
			}
			return answers, nil
		}),
	}
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
	kind, err := d.DB.CredentialKind(ctx, credID)
	if err != nil {
		return nil, err
	}
	var credUser, sudoPassword string
	var auth []ssh.AuthMethod
	switch kind {
	case "private_key", "ssh_certificate":
		signerUser, signer, err := d.DB.Signer(ctx, d.Master, credID)
		if err != nil {
			return nil, err
		}
		credUser, auth = signerUser, []ssh.AuthMethod{ssh.PublicKeys(signer)}
	case "username_password":
		passwordUser, password, err := d.DB.SSHPassword(ctx, d.Master, credID)
		if err != nil {
			return nil, err
		}
		credUser, auth, sudoPassword = passwordUser, passwordAuth(password), password
	case "kerberos":
		return nil, fmt.Errorf("credential %d uses Kerberos; configure a GSSAPI provider before assigning it to a node", credID)
	case "cyberark":
		return nil, fmt.Errorf("credential %d is a CyberArk reference; configure a CyberArk provider before assigning it to a node", credID)
	default:
		return nil, fmt.Errorf("credential %d has unsupported authentication type %q", credID, kind)
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
		Auth:            auth,
		HostKeyCallback: d.hostKeyCallback(ctx, nodeID),
		Timeout:         timeout,
		// The selected method is one stored credential. The connector never
		// falls back across credentials, which prevents surprising account
		// lockouts — passwordAuth's two methods are the same one secret.
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
	return &Client{conn: conn, parents: parents, node: node, timeout: timeout, sudoPassword: sudoPassword}, nil
}

// hostKeyCallback delegates the whole trust decision to store.CheckHostKey:
// by default an unknown key is recorded as pending and the connection is
// refused (the refusal is the feature) — only when tofu_enabled is on does a
// node's first-ever key get auto-approved instead.
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
	var sudoInput string
	if cmd.Sudo {
		// SudoCheck establishes this fact, so it is the one command permitted
		// through before the node's sudo capability is known.
		if !c.node.SudoAvailable && cmd.ID != CmdSudoCheck {
			return Result{}, fmt.Errorf("command %s needs sudo, which is not available on %s",
				cmd.ID, c.node.DisplayName)
		}
		// No `sh -c` wrapper. A per-command sudoers rule can never match a shell
		// invocation, so wrapping would force the grant to be `sh -c *` — which is
		// root, and would make the narrow grant the product documents a fiction.
		// validate() guarantees the line is a single command with no shell operators.
		line, sudoInput = c.sudoCommand(line)
	}

	sess, err := c.conn.NewSession()
	if err != nil {
		return Result{}, err
	}
	defer sess.Close()

	var out, errBuf bytes.Buffer
	sess.Stdout = &out
	sess.Stderr = &errBuf
	// sudo -S reads exactly one line (the password) and the command it runs
	// then gets the rest of the stream, which is how a file body reaches dd
	// under sudo without ever touching a command line.
	if sudoInput != "" || cmd.Stdin != "" {
		sess.Stdin = strings.NewReader(sudoInput + cmd.Stdin)
	}

	done := make(chan error, 1)
	go func() { done <- sess.Run(line) }()

	timeout := c.timeout
	if cmd.Timeout > 0 {
		timeout = cmd.Timeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		sess.Signal(ssh.SIGKILL)
		return Result{}, ctx.Err()
	case <-timer.C:
		sess.Signal(ssh.SIGKILL)
		return Result{}, fmt.Errorf("command %s timed out after %s", cmd.ID, timeout)
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

// sudoCommand keeps a password out of the remote command line. It is kept
// separate from run so the credential-policy boundary can be tested without a
// live SSH server.
func (c *Client) sudoCommand(line string) (command, stdin string) {
	if c.sudoPassword != "" {
		return "sudo -S -p '' " + line, c.sudoPassword + "\n"
	}
	return "sudo -n " + line, ""
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

// WriteFile replaces path with body, after copying the existing file aside.
// The backup path is returned so the caller can tell the operator where the
// previous content went; it is empty when the file did not exist yet.
//
// sudo is used whenever the node has it: the files this edits are root-owned on
// every fleet that matters, and a write that fails halfway is not a risk here —
// dd cannot open the file at all without permission, so a refused write leaves
// it untouched.
func (c *Client) WriteFile(ctx context.Context, path, body string) (backup string, err error) {
	sudo := c.node.SudoAvailable
	if stat, err := c.Run(ctx, FileStat(path, sudo)); err == nil && stat.ExitCode == 0 {
		backup = path + ".nagipath-" + time.Now().UTC().Format("20060102T150405Z") + ".bak"
		res, err := c.Run(ctx, FileBackup(path, backup, sudo))
		if err != nil {
			return "", err
		}
		if res.ExitCode != 0 {
			return "", fmt.Errorf("back up %s: %s", path, firstLine(res.Stderr))
		}
	}
	res, err := c.Run(ctx, FileWrite(path, body, sudo))
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("write %s: %s", path, firstLine(res.Stderr))
	}
	return backup, nil
}

// Restart runs one operator-supplied restart line. The exit code and both
// streams come back as data: a restart that fails is something the operator
// has to read, not an error to flatten into a message.
func (c *Client) Restart(ctx context.Context, line string) (Result, error) {
	return c.Run(ctx, ServiceRestart(line, c.node.SudoAvailable))
}

// Check is the cheapest proof that the connection works and tells us the two
// facts every later command depends on: the OS family and whether sudo is
// available. Password-backed credentials authenticate sudo using the same
// sealed secret; key and certificate credentials still require NOPASSWD.
func (c *Client) Check(ctx context.Context) error {
	un, err := c.Run(ctx, Uname())
	if err != nil {
		return err
	}
	osFamily := "unknown"
	if strings.HasPrefix(strings.ToLower(un.Stdout), "linux") {
		osFamily = "linux"
	}
	var sudoRes Result
	if c.sudoPassword != "" {
		// A password-backed credential cannot use `sudo -n -l`: that command
		// intentionally refuses to read stdin. Prove elevation with a harmless
		// command instead, then later commands use the same stdin-only path.
		sudoRes, err = c.run(ctx, Command{ID: CmdSudoCheck, Line: "true", Sudo: true, TolerateExit: true})
	} else {
		sudoRes, err = c.Run(ctx, SudoCheck())
	}
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
