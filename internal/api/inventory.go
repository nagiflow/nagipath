package api

import (
	"fmt"
	"strconv"
	"strings"
)

// Inventory import is the only bulk way nodes enter nagipath, and it is still not
// discovery: an inventory is a list somebody wrote down. Anything that would expand
// into hosts nobody named — a CIDR, a dashed range, Ansible's `web[01:20]` pattern —
// is refused with a reason rather than expanded, because expanding it is scanning
// with extra steps (ADR-0003).

type invHost struct {
	Name    string
	Address string
	Port    int
	User    string
	Groups  []string
}

// parseInventory accepts a pasted list, an Ansible INI inventory or an Ansible YAML
// inventory. It returns the hosts it understood and one line per thing it refused,
// because a silent drop in an import of 200 hosts is how a node goes uncollected
// for a month.
func parseInventory(text string) (hosts []invHost, refused []string) {
	switch {
	case looksYAML(text):
		hosts, refused = parseYAMLInventory(text)
	case looksINI(text):
		hosts, refused = parseINIInventory(text)
	default:
		hosts, refused = parsePlainList(text)
	}

	var out []invHost
	seen := map[string]bool{}
	for _, h := range hosts {
		addr := h.Address
		if addr == "" {
			addr = h.Name
		}
		if why := rejectRange(addr); why != "" {
			refused = append(refused, addr+": "+why)
			continue
		}
		if h.Port == 0 {
			h.Port = 22
		}
		h.Address = addr
		if h.Name == "" {
			h.Name = addr
		}
		if seen[strings.ToLower(addr)] {
			continue
		}
		seen[strings.ToLower(addr)] = true
		out = append(out, h)
	}
	return out, refused
}

// rejectRange names why an address is not one host. The same test guards the
// single-host form; both exist because both are places a range could get in.
func rejectRange(addr string) string {
	switch {
	case addr == "":
		return "empty"
	case strings.Contains(addr, "/"):
		return "nagipath does not scan networks; add one host at a time"
	case strings.ContainsAny(addr, "[]"):
		return "a host pattern expands into hosts nobody named; list them instead"
	case strings.Contains(addr, "*") || strings.Contains(addr, "?"):
		return "a wildcard is not a host"
	// Digits, dots and a dash and nothing else: `10.90.4.10-40` and
	// `10.90.4.10-10.90.4.40` are both ranges. A hostname like `lb-01.example.com`
	// has letters, so it is untouched.
	case strings.Contains(addr, "-") && strings.IndexFunc(addr, func(r rune) bool {
		return !(r >= '0' && r <= '9') && r != '.' && r != '-'
	}) < 0:
		return "an address range is a scan; add one host at a time"
	}
	return ""
}

func looksINI(text string) bool {
	for line := range strings.Lines(text) {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			return true
		}
	}
	return false
}

func looksYAML(text string) bool {
	for line := range strings.Lines(text) {
		t := strings.TrimSpace(line)
		if t == "hosts:" || strings.HasPrefix(t, "all:") || strings.HasPrefix(t, "children:") {
			return true
		}
	}
	return false
}

// parsePlainList reads one host per line, in the shapes an operator pastes out of a
// wiki page: `host`, `host:2222`, `user@host`, `user@host:2222`.
func parsePlainList(text string) (hosts []invHost, refused []string) {
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		h, orig := invHost{}, line
		if user, rest, ok := strings.Cut(line, "@"); ok {
			h.User, line = user, rest
		}
		if host, port, ok := strings.Cut(line, ":"); ok {
			if n, err := strconv.Atoi(port); err == nil {
				h.Port = n
				line = host
			}
		}
		// `user@` with nothing after it leaves no address at all. This is pasted text,
		// so it has to be refused rather than indexed into.
		fields := strings.Fields(line)
		if len(fields) == 0 {
			refused = append(refused, orig+": no host in this line")
			continue
		}
		h.Address = fields[0]
		hosts = append(hosts, h)
	}
	return hosts, refused
}

func parseINIInventory(text string) (hosts []invHost, refused []string) {
	group := ""
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			group = strings.Trim(line, "[]")
			continue
		}
		// `[web:vars]` and `[web:children]` describe groups, not hosts.
		if strings.Contains(group, ":") {
			continue
		}
		fields := strings.Fields(line)
		h := invHost{Name: fields[0]}
		if group != "" && group != "all" {
			h.Groups = []string{group}
		}
		for _, f := range fields[1:] {
			k, v, ok := strings.Cut(f, "=")
			if !ok {
				continue
			}
			applyInvVar(&h, k, strings.Trim(v, `"'`))
		}
		hosts = append(hosts, h)
	}
	return hosts, refused
}

// parseYAMLInventory reads the subset of YAML an Ansible inventory actually uses:
// mappings, two-space nesting, no anchors and no flow style.
//
// ponytail: indentation tracking, not a YAML parser — nagipath has no YAML
// dependency and adding one for this would be the largest new attack surface in the
// product. Anything this cannot read is reported as unreadable, not guessed at.
func parseYAMLInventory(text string) (hosts []invHost, refused []string) {
	var (
		hostsIndent = -1 // indent of the `hosts:` key we are inside
		hostIndent  = -1 // indent of the hostname keys under it
		current     *invHost
	)
	flush := func() {
		if current != nil {
			hosts = append(hosts, *current)
			current = nil
		}
	}
	for raw := range strings.Lines(text) {
		line := strings.TrimRight(raw, "\r\n")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			refused = append(refused, trimmed+": list form is not supported; use a mapping of hostnames")
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		key, value, _ := strings.Cut(trimmed, ":")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(strings.Trim(strings.TrimSpace(value), `"'`))

		if key == "hosts" && value == "" {
			flush()
			hostsIndent, hostIndent = indent, -1
			continue
		}
		if hostsIndent < 0 || indent <= hostsIndent {
			// Left the hosts mapping: back in groups, vars or children.
			flush()
			hostsIndent, hostIndent = -1, -1
			continue
		}
		if hostIndent < 0 {
			hostIndent = indent
		}
		switch {
		case indent == hostIndent:
			flush()
			current = &invHost{Name: key}
		case current != nil && indent > hostIndent:
			applyInvVar(current, key, value)
		}
	}
	flush()
	return hosts, refused
}

func applyInvVar(h *invHost, key, value string) {
	switch key {
	case "ansible_host", "ansible_ssh_host":
		h.Address = value
	case "ansible_port", "ansible_ssh_port":
		h.Port, _ = strconv.Atoi(value)
	case "ansible_user", "ansible_ssh_user":
		h.User = value
	}
}

func (h invHost) label() string {
	if h.Port != 22 {
		return fmt.Sprintf("%s (%s:%d)", h.Name, h.Address, h.Port)
	}
	if h.Name != h.Address {
		return fmt.Sprintf("%s (%s)", h.Name, h.Address)
	}
	return h.Address
}

// postImportNodes moved to nodeservice.go as NodeService's ImportNodes RPC
// (docs/adr/0018, proto/nagipath/api/v1/nodes.proto).
