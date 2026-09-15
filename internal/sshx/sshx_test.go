package sshx

import "testing"

func TestSudoCommandUsesPasswordOnlyForPasswordCredentials(t *testing.T) {
	passwordClient := Client{sudoPassword: "not-in-command"}
	line, stdin := passwordClient.sudoCommand("cat /etc/nginx/nginx.conf")
	if line != "sudo -S -p '' cat /etc/nginx/nginx.conf" {
		t.Fatalf("password sudo command = %q", line)
	}
	if stdin != "not-in-command\n" {
		t.Fatalf("password sudo stdin = %q", stdin)
	}

	keyClient := Client{}
	line, stdin = keyClient.sudoCommand("cat /etc/nginx/nginx.conf")
	if line != "sudo -n cat /etc/nginx/nginx.conf" || stdin != "" {
		t.Fatalf("key sudo command = (%q, %q), want non-interactive sudo with no stdin", line, stdin)
	}
}

// The write path's whole safety story is validate(): a restart line is the one
// command an operator writes, so anything that would need a shell must not pass
// — with or without sudo — and a path with a quote in it must stay one argument.
func TestWriteAndRestartCommandsValidate(t *testing.T) {
	for _, line := range []string{
		"systemctl restart nginx; rm -rf /",
		"systemctl restart nginx && reboot",
		"echo $(id) > /tmp/x",
	} {
		if err := ServiceRestart(line, false).validate(); err == nil {
			t.Fatalf("restart %q was accepted; shell operators must be refused", line)
		}
	}
	if err := ServiceRestart("systemctl restart nginx", true).validate(); err != nil {
		t.Fatalf("plain restart refused: %v", err)
	}

	w := FileWrite("/etc/nginx/it's.conf", "worker_processes 1;\n", true)
	if err := w.validate(); err != nil {
		t.Fatalf("write refused: %v", err)
	}
	if w.Line != `dd status=none of='/etc/nginx/it'\''s.conf'` {
		t.Fatalf("write line = %q", w.Line)
	}
	if w.Stdin != "worker_processes 1;\n" {
		t.Fatalf("body must travel on stdin, not the command line: %q", w.Line)
	}
}
