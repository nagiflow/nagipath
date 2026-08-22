#!/bin/sh
# Start sshd, then hand over to the vendor process, so a lab node looks like a
# managed host: something serving traffic, reachable over SSH, nothing else.
set -eu

mkdir -p /run/sshd

# One host key per container, kept in the /etc/ssh volume. Without the volume a
# `docker compose up` would hand out a new key every time and invalidate the
# approval an operator just gave.
ssh-keygen -A >/dev/null 2>&1

if ! grep -q '^# nagipath lab' /etc/ssh/sshd_config; then
  cat >> /etc/ssh/sshd_config <<'EOF'

# nagipath lab
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
UseDNS no
EOF
fi

# LAB_SUDO=no drops the grant entirely. That host then has to fall back to
# reading files as an unprivileged user, which is the degraded path nagipath has
# to report honestly rather than quietly present as a complete snapshot.
if [ "${LAB_SUDO:-yes}" != yes ]; then
  rm -f /etc/sudoers.d/nagipath
fi

# A root-only include, written here rather than bind-mounted so the mode belongs
# to the container and not to a file in the repository. The vendor process reads
# it as root; an unprivileged collection cannot, and has to say which file.
if [ -n "${LAB_ROOT_ONLY:-}" ]; then
  cat > "$LAB_ROOT_ONLY" <<'EOF'
# nagipath lab: mode 0600, owned by root.
# nginx reads this. A collection without sudo cannot, and must name it as
# unreadable instead of reporting a complete snapshot.
client_max_body_size 32m;
EOF
  chown root:root "$LAB_ROOT_ONLY"
  chmod 600 "$LAB_ROOT_ONLY"
fi

/usr/sbin/sshd
exec "$@"
