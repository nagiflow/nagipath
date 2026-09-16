package sshx

import (
	"fmt"
	"strings"
	"time"
)

// ID is a closed enum. Nothing runs on a managed host unless its ID appears in
// `allowed` below, and every command line is built by a constructor in this file
// with every operator- or host-supplied value shell-quoted. That combination is
// the injection boundary: a caller cannot express a command this file does not
// already know how to build.
//
// Most commands here are read-only. The three that are not — FileBackup,
// FileWrite and ServiceRestart — are operator-initiated, admin-only and
// audited; they are the whole of nagipath's write surface on a managed host.
type ID string

const (
	CmdOSRelease   ID = "os.release"
	CmdUname       ID = "os.uname"
	CmdSudoCheck   ID = "os.sudo_check"
	CmdProcList    ID = "proc.list"
	CmdSocketList  ID = "socket.list"
	CmdSystemdList ID = "systemd.units"
	CmdWhich       ID = "os.which"

	CmdNginxVersion ID = "nginx.version"
	CmdNginxDump    ID = "nginx.dump"
	CmdNginxTest    ID = "nginx.test"

	CmdHTTPDVersion  ID = "httpd.version"
	CmdHTTPDVhosts   ID = "httpd.dump_vhosts"
	CmdHTTPDModules  ID = "httpd.dump_modules"
	CmdHTTPDIncludes ID = "httpd.dump_includes"

	CmdHAProxyVersion ID = "haproxy.version"
	CmdHAProxyCheck   ID = "haproxy.check"

	CmdFileRead   ID = "fs.read"
	CmdFileStat   ID = "fs.stat"
	CmdFindConf   ID = "fs.find_conf"
	CmdFileBackup ID = "fs.backup"
	CmdFileWrite  ID = "fs.write"

	CmdServiceRestart ID = "service.restart"

	CmdGetentHosts ID = "net.getent_hosts"
	CmdX509        ID = "tls.x509_metadata"
	CmdLogTail     ID = "log.tail"
)

var allowed = map[ID]bool{
	CmdOSRelease: true, CmdUname: true, CmdSudoCheck: true, CmdProcList: true, CmdSocketList: true,
	CmdSystemdList: true, CmdWhich: true,
	CmdNginxVersion: true, CmdNginxDump: true, CmdNginxTest: true,
	CmdHTTPDVersion: true, CmdHTTPDVhosts: true, CmdHTTPDModules: true, CmdHTTPDIncludes: true,
	CmdHAProxyVersion: true, CmdHAProxyCheck: true,
	CmdFileRead: true, CmdFileStat: true, CmdFindConf: true,
	CmdFileBackup: true, CmdFileWrite: true, CmdServiceRestart: true,
	CmdGetentHosts: true, CmdX509: true, CmdLogTail: true,
}

// Command is a validated, fully rendered command line plus the metadata the
// executor needs. Construct it only through the helpers below.
type Command struct {
	ID   ID
	Line string
	Sudo bool
	// TolerateExit says a non-zero exit is data, not failure. `nginx -t` on a
	// broken config and `getent hosts` on an unresolvable name both matter.
	TolerateExit bool
	// Stdin is fed to the command's standard input. Only the write path uses
	// it: the file body never appears on a command line, so no quoting of it
	// is needed and no length limit of the remote shell applies.
	Stdin string
	// Timeout overrides the client's own when set. The restart path needs it —
	// a web server that drains connections on stop can outlast the timeout every
	// read-only command is sized for — and so do the config dumps, which make
	// the server parse the whole tree (see configDump).
	Timeout time.Duration
	// SudoRetry says: if this fails as the login user and sudo is available, run
	// it again with sudo. `nginx -T` is the case that matters — unprivileged it
	// aborts on the pid file before printing a single line of configuration, so
	// without the retry the authoritative dump is silently replaced by a guess.
	SudoRetry bool
}

func (c Command) validate() error {
	if !allowed[c.ID] {
		return fmt.Errorf("command id %q is not in the allowed set", c.ID)
	}
	if c.Line == "" {
		return fmt.Errorf("command %s has an empty line", c.ID)
	}
	// A sudo command is handed to sudo as an argv, not to a shell, so that the
	// sudoers grant can name individual binaries. Anything with a pipe, a
	// redirect or a separator in it would need `sh -c` and therefore a grant
	// equivalent to root, so it is rejected here rather than silently widened.
	// A restart command is the one line an operator writes rather than this
	// file, so it gets the shell-operator check whether or not it runs under
	// sudo — `systemctl restart nginx; rm -rf /` is not a service name.
	if c.Sudo || c.ID == CmdServiceRestart {
		for _, op := range []string{"|", ";", "&", ">", "<", "$(", "`", "\n"} {
			if strings.Contains(c.Line, op) {
				return fmt.Errorf("command %s uses %q and cannot run under sudo", c.ID, op)
			}
		}
	}
	return nil
}

// q single-quotes a value for POSIX sh. Single quotes disable every expansion,
// so the only character needing care is the single quote itself.
func q(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func OSRelease() Command {
	return Command{ID: CmdOSRelease, Line: "cat /etc/os-release", TolerateExit: true}
}

func Uname() Command {
	return Command{ID: CmdUname, Line: "uname -sr"}
}

// SudoCheck asks sudo what this user may run without a password. `sudo -n true`
// would be wrong: `true` is not in the deliberately narrow sudoers grant this
// product documents, while `-l` needs no grant of its own.
func SudoCheck() Command {
	return Command{ID: CmdSudoCheck, Line: "sudo -n -l", TolerateExit: true}
}

// ProcList walks /proc rather than shelling out to ps, because container images
// routinely ship without procps and a missing ps would look like an empty host.
func ProcList() Command {
	return Command{ID: CmdProcList, TolerateExit: true, Line: `for p in /proc/[0-9]*; do ` +
		`[ -r "$p/cmdline" ] || continue; ` +
		`printf '%s\t' "${p#/proc/}"; tr '\0' ' ' < "$p/cmdline"; echo; done`}
}

// SocketList reports listening TCP sockets and owning PIDs when ss can see them.
// It is intentionally best-effort: some systems hide process ownership from an
// unprivileged SSH user, in which case discovery keeps its existing behavior.
func SocketList() Command {
	return Command{ID: CmdSocketList, TolerateExit: true, Line: "ss -ltnpH"}
}

func SystemdUnits() Command {
	return Command{ID: CmdSystemdList, TolerateExit: true,
		Line: "systemctl list-units --type=service --all --no-pager --plain --no-legend"}
}

// WhichDirs are probed in addition to PATH. A non-interactive SSH session gets a
// minimal PATH, so a server installed outside it — httpd under /usr/local/apache2
// on a source build, which is also what the upstream container images do — would
// otherwise look like it is not installed at all.
var WhichDirs = []string{
	"/usr/sbin", "/usr/local/sbin", "/usr/bin", "/usr/local/bin", "/sbin",
	"/usr/local/nginx/sbin", "/opt/nginx/sbin",
	"/usr/local/apache2/bin", "/opt/apache2/bin",
	"/usr/local/haproxy/sbin",
}

func Which(binaries ...string) Command {
	parts := make([]string, len(binaries))
	for i, b := range binaries {
		parts[i] = q(b)
	}
	dirs := make([]string, len(WhichDirs))
	for i, d := range WhichDirs {
		dirs[i] = q(d)
	}
	return Command{ID: CmdWhich, TolerateExit: true,
		Line: "for b in " + strings.Join(parts, " ") + `; do command -v "$b" || true; ` +
			"for d in " + strings.Join(dirs, " ") + `; do ` +
			`[ -x "$d/$b" ] && echo "$d/$b"; done; done; true`}
}

// The vendor commands carry no `2>&1`: a redirection would make them ineligible
// for sudo (see Command.validate), and the sudo escalation is the difference
// between the real configuration and a guess. Stderr comes back separately in
// Result, so callers read both streams instead — which they must anyway, since
// nginx prints -V to stderr and -T to stdout.
func NginxVersion(bin string) Command {
	return Command{ID: CmdNginxVersion, Line: q(bin) + " -V", TolerateExit: true}
}

// NginxDump is the config authority (ADR-0004): `nginx -T` prints the complete
// effective configuration with every include resolved. conf may be empty to use
// the binary's compiled-in default.
func NginxDump(bin, conf string) Command {
	line := q(bin) + " -T"
	if conf != "" {
		line += " -c " + q(conf)
	}
	return configDump(CmdNginxDump, line)
}

func NginxTest(bin, conf string) Command {
	line := q(bin) + " -t"
	if conf != "" {
		line += " -c " + q(conf)
	}
	return configDump(CmdNginxTest, line)
}

// configDump is every command that makes the server itself read its whole
// configuration: `nginx -T`, `httpd -t -D DUMP_*`, `haproxy -c`. On an estate
// with thousands of vhosts that parse is minutes of work — mod_ssl reads every
// certificate, and httpd resolves a ServerName-less vhost through DNS — so the
// timeout the read-only commands are sized for cuts the authoritative dump off
// and the collector silently falls back to guessing at the file tree.
func configDump(id ID, line string) Command {
	return Command{ID: id, Line: line, TolerateExit: true, SudoRetry: true,
		Timeout: 3 * time.Minute}
}

func HTTPDVersion(bin string) Command {
	return Command{ID: CmdHTTPDVersion, Line: q(bin) + " -V", TolerateExit: true}
}

func httpdDump(id ID, bin, conf, define string) Command {
	line := q(bin) + " -t -D " + define
	if conf != "" {
		line += " -f " + q(conf)
	}
	return configDump(id, line)
}

func HTTPDVhosts(bin, conf string) Command {
	return httpdDump(CmdHTTPDVhosts, bin, conf, "DUMP_VHOSTS")
}
func HTTPDModules(bin, conf string) Command {
	return httpdDump(CmdHTTPDModules, bin, conf, "DUMP_MODULES")
}
func HTTPDIncludes(bin, conf string) Command {
	return httpdDump(CmdHTTPDIncludes, bin, conf, "DUMP_INCLUDES")
}

func HAProxyVersion(bin string) Command {
	return Command{ID: CmdHAProxyVersion, Line: q(bin) + " -vv", TolerateExit: true}
}

func HAProxyCheck(bin, conf string) Command {
	return configDump(CmdHAProxyCheck, q(bin)+" -c -f "+q(conf))
}

// FileRead reads at most max+1 bytes so the caller can tell "exactly at the cap"
// from "truncated at the cap".
func FileRead(path string, max int64, sudo bool) Command {
	return Command{ID: CmdFileRead, Sudo: sudo, TolerateExit: true,
		Line: fmt.Sprintf("head -c %d -- %s", max+1, q(path))}
}

func FileStat(path string, sudo bool) Command {
	return Command{ID: CmdFileStat, Sudo: sudo, TolerateExit: true,
		Line: "stat -c '%s %Y %U %a' -- " + q(path)}
}

// FindConf is the fallback walk, used only when the vendor dump is unavailable.
// Depth is bounded so a symlink into / does not turn a collection into a crawl.
//
// FindConfMax is applied by the caller rather than by a `| head`, because a pipe
// would make this command ineligible for sudo (see Command.validate).
func FindConf(root string, sudo bool) Command {
	return Command{ID: CmdFindConf, Sudo: sudo, TolerateExit: true,
		Line: "find " + q(root) + " -maxdepth 4 -type f " +
			`\( -name '*.conf' -o -name '*.cfg' -o -name 'nginx.conf' -o -name 'httpd.conf' \) ` +
			"-print"}
}

// FindConfMax caps how many paths the fallback walk will consider.
const FindConfMax = 500

func GetentHosts(name string) Command {
	return Command{ID: CmdGetentHosts, TolerateExit: true, Line: "getent hosts " + q(name)}
}

// X509 extracts certificate metadata on the target. Key material is never
// transferred: only these fields come back.
func X509(path string, sudo bool) Command {
	return Command{ID: CmdX509, Sudo: sudo, TolerateExit: true,
		// -pubkey prints the public key as PEM, which is what tells an operator
		// "RSA 2048" or "ECDSA 256". Without it the certificate list had a Key
		// column that was empty on every row, because nothing populated it. It is
		// the public half only: no private key material is ever read (ADR-0009).
		Line: "openssl x509 -noout -subject -issuer -serial -dates " +
			"-fingerprint -sha256 -ext subjectAltName -pubkey -in " + q(path)}
}

func LogTail(path string, bytes int64, sudo bool) Command {
	return Command{ID: CmdLogTail, Sudo: sudo, TolerateExit: true,
		Line: fmt.Sprintf("tail -c %d -- %s", bytes, q(path))}
}

// FileBackup copies a file aside before FileWrite overwrites it. -p keeps the
// mode, owner and timestamps, so the copy is a usable rollback and not just a
// record that something used to be there.
func FileBackup(path, dest string, sudo bool) Command {
	return Command{ID: CmdFileBackup, Sudo: sudo, TolerateExit: true,
		Line: "cp -p -- " + q(path) + " " + q(dest)}
}

// FileWrite overwrites path with body. dd rather than tee: tee echoes the whole
// file back over the connection, and a redirect would need `sh -c` and so make
// the command ineligible for sudo (see Command.validate). dd truncates in
// place, which keeps the file's inode, owner and mode.
func FileWrite(path, body string, sudo bool) Command {
	return Command{ID: CmdFileWrite, Sudo: sudo, Stdin: body, TolerateExit: true,
		Line: "dd status=none of=" + q(path)}
}

// ServiceRestart runs the operator's own restart line — the one place a command
// is not built from a constructor's parts, because no closed enum can know how
// a given fleet restarts a given process. validate() still rejects anything
// with a shell operator in it, so the line stays a single command with
// arguments and a per-binary sudoers grant remains expressible.
func ServiceRestart(line string, sudo bool) Command {
	return Command{ID: CmdServiceRestart, Sudo: sudo, TolerateExit: true,
		Timeout: 60 * time.Second, Line: strings.TrimSpace(line)}
}
