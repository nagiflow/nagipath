#!/bin/sh
# Generate the lab's throwaway SSH key and certificates.
#
# Nothing here is committed: a private key in a repository is a private key on
# every laptop that clones it, lab or not. Run this once before `docker compose
# up`; it is idempotent and skips anything that already exists.
set -eu
cd "$(dirname "$0")"

mkdir -p keys certs

if [ ! -f keys/lab_key ]; then
  ssh-keygen -t ed25519 -N '' -C 'nagipath lab' -f keys/lab_key >/dev/null
  echo "keys/lab_key"
fi

# A password only good enough for a lab, which is the only place it is used.
if [ ! -f keys/admin-password ]; then
  printf 'nagipath-lab-admin\n' > keys/admin-password
  chmod 600 keys/admin-password
  echo "keys/admin-password"
fi

# shop.example.com, valid for 20 days, so the dashboard's expiring-certificate
# panel has something real in it on the day the lab is started.
if [ ! -f certs/shop.pem ]; then
  openssl req -x509 -newkey rsa:2048 -nodes -days 20 \
    -keyout certs/shop.key -out certs/shop.crt \
    -subj '/CN=shop.example.com/O=nagipath lab' \
    -addext 'subjectAltName=DNS:shop.example.com,DNS:www.example.com' >/dev/null 2>&1
  # haproxy wants certificate and key in one file.
  cat certs/shop.crt certs/shop.key > certs/shop.pem
  echo "certs/shop.pem (expires in 20 days)"
fi

# Issued for other.example.com but installed on admin.example.com. nginx starts
# without complaint; only an inventory notices.
if [ ! -f certs/admin.crt ]; then
  openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
    -keyout certs/admin.key -out certs/admin.crt \
    -subj '/CN=other.example.com/O=nagipath lab' \
    -addext 'subjectAltName=DNS:other.example.com' >/dev/null 2>&1
  echo "certs/admin.crt (SAN does not match admin.example.com)"
fi

# app01's internal TLS listener: three days left, and issued for a name it is not
# served for. Nothing external watches the origin tier, so this is the expiry that
# takes out the edge-to-origin leg while the public certificate is still valid.
if [ ! -f certs/app01-internal.crt ]; then
  openssl req -x509 -newkey rsa:2048 -nodes -days 3 \
    -keyout certs/app01-internal.key -out certs/app01-internal.crt \
    -subj '/CN=app01.internal.example.com/O=nagipath lab' \
    -addext 'subjectAltName=DNS:app01.internal.example.com' >/dev/null 2>&1
  echo "certs/app01-internal.crt (expires in 3 days; served for origin.example.com)"
fi

chmod 644 certs/*.crt
chmod 600 certs/*.key certs/shop.pem
echo "lab material ready"
