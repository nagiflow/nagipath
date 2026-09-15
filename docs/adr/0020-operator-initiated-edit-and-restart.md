# Editing a file and restarting a process are operator-initiated, and the only writes

[ADR-0002](0002-read-only-v1.md) says nagipath "writes nothing to any managed host" and defers
editing to v2 with the mechanism undecided. This decides it, and narrowly.

Two write actions exist, both admin-only, both requiring an operator to press a button on a
specific file or a specific Instance:

- **Edit a file.** The editor reads the file off the Node over the same SSH connection the
  collector uses — the live bytes, not the Snapshot's copy of them, because writing back a capture
  would silently revert whatever changed since the last Collection. The previous content is copied
  to a timestamped `.nagipath-<ts>.bak` beside the file before the write, and the write itself is
  `dd` fed from stdin, so the body never appears on a command line and the file keeps its inode,
  owner and mode.
- **Restart an Instance.** The command is the operator's, shown and editable before anything runs,
  and stored on the Instance for next time. nagipath derives a default only from a systemd unit it
  actually discovered (`systemctl restart <unit>`); it never guesses one from the Vendor name,
  because "nginx" is not reliably the unit name and a wrong guess restarts the wrong process.

What ADR-0002 rejected is still rejected: no change sets, no orchestration across a Cluster, no
canary batches, no rolling deployment, no rollback engine, nothing scheduled or automatic. A write
touches one file on one Node, or restarts one Instance, and only because a person asked for it.

## Consequences

The claim "nagipath needs no write credentials at all" no longer holds for a deployment that uses
these two actions. The credential and its sudoers grant decide what is writable — nagipath keeps no
second allowlist of editable paths, because a second list would drift from the first and the first
is the one the host enforces. A deployment that wants the old guarantee back gets it by giving the
collector's credential no write access, which fails these two actions and nothing else.

The restart line is the one command not built by a constructor in `internal/sshx/command.go`. It is
still validated there: anything containing a shell operator (`|`, `;`, `&`, redirects, substitution)
is refused, so the line stays a single command with arguments and a per-binary sudoers grant remains
expressible.

Both actions are audited with the actor, the target and the outcome (`node.file.write`,
`instance.restart`), including the backup path and the command that ran. Neither runs the Vendor's
own config test: an edit is applied when the operator restarts, and that is where a bad config
surfaces, in the Vendor's own words.
