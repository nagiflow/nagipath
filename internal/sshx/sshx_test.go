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
