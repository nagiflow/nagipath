-- Per-Instance restart command. Empty means "derive it" (systemctl restart
-- <unit_name> when the Instance is systemd-managed); an operator override is
-- stored here verbatim and reused for every later restart of that Instance.
ALTER TABLE instance ADD COLUMN restart_command TEXT NOT NULL DEFAULT '';
