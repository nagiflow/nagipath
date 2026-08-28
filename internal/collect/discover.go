// Package collect turns one Node into Instances and Snapshots.
//
// Discovery order is deliberate: running processes first, then service units,
// then binaries on PATH. A process is proof that something is serving; a unit
// file is proof that something is meant to be; a binary on PATH is only a hint.
// The strongest available evidence wins, and the weaker sources only add
// Instances the stronger ones missed.
package collect

import (
	"context"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/nagiflow/nagipath/internal/sshx"
)

// Found is one candidate Instance, before anything has been captured from it.
type Found struct {
	Vendor      string // nginx | apache | haproxy
	Binary      string
	ConfigPaths []string // explicit -c / -f arguments, in order
	PID         int
	Unit        string
	Evidence    string // process | unit | path
	Active      bool   // process owns a socket reported by ss
}

// NaturalKey identifies an Instance across restarts and PID changes. The config
// path is the identity: two nginx masters on one host are different Instances
// exactly when they serve different configurations.
func (f Found) NaturalKey() string {
	if len(f.ConfigPaths) > 0 {
		return strings.Join(f.ConfigPaths, ",")
	}
	if f.Unit != "" {
		return f.Unit
	}
	return f.Binary
}

func Discover(ctx context.Context, ex sshx.Executor) ([]Found, error) {
	var found []Found
	seen := map[string]bool{}
	add := func(f Found) {
		key := f.Vendor + "|" + f.NaturalKey()
		if seen[key] {
			return
		}
		seen[key] = true
		found = append(found, f)
	}

	// Locate the binaries first, because a process title is usually a bare name.
	var bins []string
	if res, err := ex.Run(ctx, sshx.Which("nginx", "httpd", "apache2", "haproxy")); err == nil {
		bins = uniqueLines(res.Stdout)
	}

	var processes []Found
	if res, err := ex.Run(ctx, sshx.ProcList()); err == nil {
		processes = fromProcesses(res.Stdout)
		activePIDs := activeSocketPIDs(ctx, ex)
		activeVendors := map[string]bool{}
		for i := range processes {
			if activePIDs[processes[i].PID] {
				processes[i].Active = true
				activeVendors[processes[i].Vendor] = true
			}
		}
		for _, f := range processes {
			// Once ss identifies a serving process for a vendor, an additional
			// process record is a stopped or non-listening configuration.
			if len(activePIDs) > 0 && activeVendors[f.Vendor] && !f.Active {
				continue
			}
			f.Binary = absBinary(f.Binary, bins)
			add(f)
		}
	} else {
		return nil, err
	}

	if res, err := ex.Run(ctx, sshx.SystemdUnits()); err == nil {
		for _, f := range fromUnits(res.Stdout) {
			add(f)
		}
	}

	// A binary with no process and no unit still gets an Instance: an nginx that is
	// installed and stopped is a fact an operator wants to see, not an absence.
	//
	// One per vendor, and only where nothing stronger was found. Path evidence
	// carries no config path, so it can never be the *second* instance of a vendor
	// in any useful way — it would just be the running one under another name,
	// which is a duplicate row for a single serving process.
	strong := map[string]bool{}
	for _, f := range found {
		strong[f.Vendor] = true
	}
	for _, bin := range bins {
		v := vendorOf(bin)
		if v == "" || strong[v] {
			continue
		}
		strong[v] = true
		add(Found{Vendor: v, Binary: bin, Evidence: "path"})
	}
	return found, nil
}

var socketPIDPattern = regexp.MustCompile(`pid=(\d+)`)

func activeSocketPIDs(ctx context.Context, ex sshx.Executor) map[int]bool {
	res, err := ex.Run(ctx, sshx.SocketList())
	if err != nil {
		return nil
	}
	active := map[int]bool{}
	for _, match := range socketPIDPattern.FindAllStringSubmatch(res.Stdout, -1) {
		pid, err := strconv.Atoi(match[1])
		if err == nil && pid > 0 {
			active[pid] = true
		}
	}
	return active
}

// absBinary turns the bare name in a process title into the path we can actually
// run. A non-interactive SSH session's PATH is not the daemon's PATH, so invoking
// `httpd` verbatim is how a source-built or containerised server reports itself as
// "command not found" and takes its whole configuration down with it.
func absBinary(bin string, bins []string) string {
	if bin == "" || strings.HasPrefix(bin, "/") {
		return bin
	}
	for _, cand := range bins {
		if path.Base(cand) == path.Base(bin) {
			return cand
		}
	}
	return bin
}

func uniqueLines(out string) []string {
	var list []string
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		v := strings.TrimSpace(line)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		list = append(list, v)
	}
	return list
}

// fromProcesses reads the /proc walk. Lines look like:
//
//	1234\tnginx: master process /usr/sbin/nginx -c /etc/nginx/nginx.conf
//	5678\t/usr/sbin/httpd -DFOREGROUND
//
// Worker processes are skipped: they share the master's configuration and would
// otherwise multiply one Instance into a dozen.
func fromProcesses(out string) []Found {
	var found []Found
	for _, line := range strings.Split(out, "\n") {
		pidStr, cmd, ok := strings.Cut(strings.TrimRight(line, " \t\r"), "\t")
		if !ok {
			continue
		}
		pid, _ := strconv.Atoi(pidStr)
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		lower := strings.ToLower(cmd)

		switch {
		case strings.HasPrefix(lower, "nginx:"):
			if !strings.Contains(lower, "master process") {
				continue // worker / cache manager
			}
			f := Found{Vendor: "nginx", PID: pid, Evidence: "process"}
			// nginx rewrites argv, so the master's title carries the real command.
			fields := strings.Fields(strings.TrimPrefix(cmd, "nginx: master process "))
			if len(fields) > 0 {
				f.Binary = fields[0]
			}
			f.ConfigPaths = flagValues(fields, "-c")
			found = append(found, f)
		default:
			fields := strings.Fields(cmd)
			if len(fields) == 0 {
				continue
			}
			vendor := vendorOf(fields[0])
			if vendor == "" {
				continue
			}
			f := Found{Vendor: vendor, Binary: fields[0], PID: pid, Evidence: "process"}
			switch vendor {
			case "haproxy":
				f.ConfigPaths = flagValues(fields, "-f")
			case "apache":
				f.ConfigPaths = flagValues(fields, "-f")
			case "nginx":
				f.ConfigPaths = flagValues(fields, "-c")
			}
			found = append(found, f)
		}
	}
	return found
}

// fromUnits reads `systemctl list-units --plain --no-legend`, whose first column
// is the unit name.
func fromUnits(out string) []Found {
	var found []Found
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		unit := strings.TrimSuffix(fields[0], ".service")
		var vendor string
		switch unit {
		case "nginx":
			vendor = "nginx"
		case "httpd", "apache2":
			vendor = "apache"
		case "haproxy":
			vendor = "haproxy"
		default:
			continue
		}
		found = append(found, Found{Vendor: vendor, Unit: fields[0], Evidence: "unit"})
	}
	return found
}

func vendorOf(bin string) string {
	switch path.Base(strings.TrimSpace(bin)) {
	case "nginx":
		return "nginx"
	case "httpd", "apache2", "httpd.worker", "httpd.event":
		return "apache"
	case "haproxy":
		return "haproxy"
	}
	return ""
}

// flagValues collects every value of a repeatable flag, handling both `-f x` and
// `-fx`. HAProxy is commonly started with several -f arguments.
func flagValues(fields []string, flag string) []string {
	var out []string
	for i, f := range fields {
		switch {
		case f == flag && i+1 < len(fields):
			out = append(out, fields[i+1])
		case len(f) > len(flag) && strings.HasPrefix(f, flag):
			out = append(out, f[len(flag):])
		}
	}
	return out
}
