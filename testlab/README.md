# The lab

A four-node fleet across three vendors, with the failure modes nagipath exists to
find already built in. It is the fastest way to see the product do the thing it
claims to do, and the reference environment for developing against it.

```
                        ┌─ web02  10.90.4.20  nginx 1.24 ──┐
 lb01  :80 :443 ────────┤                                   ├─► app01  10.90.4.30  httpd 2.4
 10.90.4.10             └─ web05  10.90.4.25  nginx 1.24 ──┘         :8080  :8443
 haproxy 2.8                            │
 :8404 stats                            └──► 10.90.4.7  (nothing listens here)
 :9443 mTLS
 :5432 tcp
```

lb01's ports are published on the host as 8081 (HTTP), 8443 (HTTPS) and 8404
(stats); nagipath's UI is on 8080. Plain HTTP redirects, so use 8443.

## Layout

Each node's configuration is a tree, because that is how a real estate is laid
out and because it is the harder case for a collector:

```
lb01/haproxy.cfg              one file — haproxy has no `include`
lb01/maps/ lb01/errors/       data the config reads at request time
web02/nginx.conf              tuning and three `include` lines, nothing else
web02/conf.d/                 http-scope machinery: maps, zones, caches, upstreams
web02/snippets/               fragments included from inside locations, by many sites
web02/sites-enabled/          one file per Site
web05/…                       the same five paths, different contents in all of them
app01/httpd.conf              modules and global defaults
app01/conf.d/ sites-enabled/  everything that decides what happens to a request
```

`nginx -T` prints the whole tree as one stream and Apache does not print it at
all. Either way nagipath has to attribute every derived Rule back to the file and
byte range it came from — and `web02/snippets/proxy-defaults.conf` is included
from six locations, which is six Rules from one span of text.

## Running it

```sh
cd testlab
./gen.sh                 # throwaway SSH key + certificates, once
docker compose up -d --build
```

The `nagipath` container bind-mounts the repository and runs Air, so Go,
template, migration and static-source changes rebuild and restart only the
control plane. The fixture nodes and their persisted host keys remain running;
follow rebuild output with `docker compose logs -f nagipath`.

Then open <http://127.0.0.1:8080>. Sign in as `admin`, password
`nagipath-lab-admin` (it is in `keys/admin-password`; it is a lab).

The four nodes are already listed, with the lab credential attached. Nothing has
been collected yet, and nothing will be until you approve each host key:

```sh
docker compose exec nagipath nagipath collect all   # fetches host keys, collects nothing
```

That reports four pending host keys. Approve them at <http://127.0.0.1:8080/nodes>,
then collect for real:

```sh
docker compose exec nagipath nagipath collect all
```

This step is not a formality that could be automated away. There is no
trust-on-first-use path anywhere in the product, so a lab that skipped the
approval would be demonstrating something nagipath does not do.

## What to look at

**Trace `https://shop.example.com/api/v2/charge`** (`/trace`, or
`docker compose exec nagipath nagipath trace https://shop.example.com/api/v2/charge`).
The request path is three hops — lb01, web02, app01 — and the trace prints five,
because `be_app` has a second member and nagipath follows that branch too rather
than picking a winner it cannot know. Six things worth reading closely:

- **lb01 terminates TLS**; everything after it is plain HTTP. A trace that
  reported the inner hops as encrypted would be wrong in the direction that
  matters.
- **The path changes at web02.** `proxy_pass http://app_backend/` — note the
  trailing slash — strips the matched `/api/` prefix, so app01 receives
  `/v2/charge`. Drop the slash and it would receive `/api/v2/charge`. The trace
  names the rule and its byte offset in the file. `location /api-raw/` next to it
  omits the slash and proxies to the same pool, so the two routes differ only in
  the path the origin sees.
- **Two headers are discarded at web02.** `add_header X-Api-Version 2` inside
  `location /api/` throws away *both* server-level `add_header` directives,
  including `X-Frame-Options DENY`. Reading the file top to bottom suggests the
  opposite. This is the case most often missed by hand.
- **lb01 has three branches nagipath will not guess at.** `use_backend be_static if
  is_static`, `use_backend be_admin if is_admin` and `use_backend be_maintenance if
  edge_maint` depend on the request or on the running state, not on the
  configuration, so they are listed as undetermined rather than resolved. The trace
  follows `default_backend`.
- **app01 has four more, and they are rewrites.** Every `RewriteRule` in the vhost
  is preceded by a `RewriteCond` testing the Host header, the method or a request
  header. Apache ANDs those conditions, so one unevaluable condition makes the
  whole rule a branch — the trace quotes the conditions and leaves the path alone
  rather than reporting a rewrite that only some requests take. Including the one
  that would have sent the request to a completely different origin
  (`RewriteRule … [P,L]`, a proxy hop no `ProxyPass` line declares).
- **app01 ends the trace with `static_content`.** `/v2/charge` matches both a
  `<Directory>` and a `<Location>`; Apache merges sections in rank order and the
  last one wins, so the `<Location>` header is the one that survives. nginx would
  have taken the first match. Same request, opposite rule.

**Check it against the wire.** The lab publishes lb01, so the same request can be
made for real and the trace's claims checked one by one:

```sh
curl -sik -H 'Host: shop.example.com' https://127.0.0.1:8443/api/v2/charge
```

```
HTTP/2 200
x-request-id: 4c401b0e-76c4-4448-bc7e-3fe62ac1a878
x-frame-options: SAMEORIGIN
x-served-by: app01 (Location, rank 80)
x-api-version: 2
```

Four claims arriving as predicted, in four lines:

- `x-served-by` names the `<Location>` block, not the `<Directory>` one — Apache's
  merge order.
- `x-api-version: 2` is web02's location header, the one that did the discarding.
- `x-frame-options: SAMEORIGIN` is **app01's**, not web02's. web02's `DENY` was
  discarded before the response ever left it, and what reaches the client is the
  weaker value set three files away on another node by another vendor. A header
  being present is not evidence that the header you configured is the one in
  force.
- `x-request-id` was generated by lb01 and is in web02's and app01's access logs,
  which is what lets nagipath mark those Hops Verified rather than Inferred.

Repeat the request and roughly half fail — `502` or `504` depending on which
timeout expires first. That is web05 reaching for `10.90.4.7`, which is also what
the trace said would happen. A trace is a claim about a request; this is the
request.

Over plain HTTP the edge answers `301` instead, except for `/health`, which
`acl is_health` exempts. `http://127.0.0.1:8081/haproxy-up` is haproxy's own
`monitor-uri` and never reaches a backend at all.

**web05 has drifted** from web02, and none of it shows up in a health check:

| | web02 | web05 |
|---|---|---|
| `X-Frame-Options` | `DENY` (discarded downstream) | never set |
| Extra routes | — | `/debug/` |
| `app_backend` | `app01:8080` | `10.90.4.7:8443` |
| `ssl_protocols` | `TLSv1.2 TLSv1.3` | `TLSv1 TLSv1.1 TLSv1.2` |
| `server_tokens` | `off` | `on` |
| `worker_rlimit_nofile` | `65535` | `1024` |
| `log_format` | JSON, carries the request id | combined, no request id |
| `snippets/proxy-defaults.conf` | 8 headers forwarded | 6 — two changes behind |
| `auth_backend` | 2 members | 1, and it is `10.90.4.7` |
| `$api_pool` map | has a `canary` entry | has none |
| `set_real_ip_from` | present | missing |

Two of those compound rather than add. The missing `log_format` fields mean a
Probe against web05 cannot be correlated to a log line, so a Hop through it can
never be Verified — the drift in the observability is what makes the rest of the
drift harder to prove. And because `set_real_ip_from` is missing, web05 evaluates
`if ($internal_client = 0) { return 403; }` against lb01's address instead of the
client's, so the access control on `/api/v2/admin/` silently never fires on this
node and does on the other.

Trace `https://shop.example.com/api/v2/charge` and web05 appears as the second
member of `be_app`: its own hop, terminating in an unreachable upstream. That is
half the pool serving a security header and half not, which is what the inventory
is for.

**Routes nothing can resolve, on all three vendors.** These are reported as
undetermined with the reason, never guessed:

- lb01: `use_backend %[req.hdr(host),lower,map_str(/etc/haproxy/maps/host-backend.map,be_app)]`
  on `fe_partner` — the destination is a lookup in a data file, evaluated per
  request.
- web02: `proxy_pass http://$api_pool/` on `location /api-channel/` — the upstream
  name is a variable filled from a request header.
- app01: `ProxyPassMatch "^/tenant/([a-z0-9-]+)/(.*)$"` — the target is built from
  a capture, so there is no fixed address to record.

**app01 has three vhosts that are not in its configuration text.**
`sites-enabled/30-partners.conf` contains one `<VirtualHost>` and three `Use
PartnerVHost` lines, and mod_macro turns those into `acme`, `globex` and
`initech`. nagipath reports what it parsed and says the file uses a macro; it does
not expand it. Reimplementing Apache's macro processor and being subtly wrong
about which Sites exist is worse than declining to answer. `httpd -S` shows the
three, and comparing the two is the point.

**Two `IncludeOptional` lines in `httpd.conf` are silent when they match
nothing** — that is what the "Optional" means, and `httpd -t` still says `Syntax
OK` with an entire configuration tree unloaded. `conf.d/local-overrides/*.conf`
matches nothing on purpose. A collection that did not report the unmatched pattern
would leave an operator certain a policy was in force.

The paths in those `Include` lines are absolute, deliberately, and the comment
above them says why: a *relative* Include is resolved against `ServerRoot`
(`/usr/local/apache2`) and not against the directory `httpd.conf` is in
(`/usr/local/apache2/conf`). Getting that backwards finds nothing, and
`IncludeOptional` makes finding nothing silent. nagipath resolves against
`ServerRoot` and degrades the snapshot if a tree is reachable only the other way,
because a tree the running server is not loading is not part of the configuration.

**Almost everything on app01 is inside an `<IfModule>`,** which is how real Apache
configurations are written and where a naive parser loses most of them.
`<IfModule>`, `<IfDefine>` and `<IfVersion>` are decided when Apache reads the
file, so their contents are ordinary configuration — three security headers, a
`<Location>`, a whole set of `ProxyPass` lines and every `RewriteRule` in the vhost
are inside one here. nagipath hoists them and keeps the guard alongside, because
whether a module is loaded is a fact about the running server rather than about the
text. `<If>` is *not* hoisted: that one is evaluated per request, and flattening it
would turn a branch into a fact.

**web05's snapshot is degraded, on purpose.** It is the only node without the
sudoers grant, so its collection falls back to reading configuration files as an
unprivileged user — and `/etc/nginx/lab-tuning.conf` is mode 0600, owned by root.
`client_max_body_size` is set inside it, so the value actually in force on that
node is not in any file the collection can read. The snapshot says
`fallback_walk`, is marked degraded, and names the file. Compare it to web02,
which is `vendor_dump` from `nginx -T`.

**Three certificates, three problems**, on `/certificates` from the first
collection. Each row puts the names the certificate covers next to the names it
was bound to serve, which is the comparison that makes the last two visible:

- `shop.example.com` on lb01 is issued for 20 days, so the page shows it with
  under three weeks left.
- `admin.example.com` on web05 is served a certificate issued for
  `other.example.com`, flagged `does not cover admin.example.com`. nginx starts
  without complaint.
- `origin.example.com` on app01's `:8443` is served `app01-internal.crt`: days
  rather than weeks left, and issued for `app01.internal.example.com`. Nothing
  external watches the origin tier, so this is the expiry that takes out the
  edge-to-origin leg while the public certificate is still valid.

There is a fourth problem the page does *not* flag, and it is worth knowing about.
`fe_public` terminates TLS for `shop.example.com` and `admin.example.com` from one
`crt`, and only `shop.example.com` is on it. nagipath does not report the mismatch
because `admin.example.com` appears on that frontend only inside an ACL, and an
ACL is not a name the frontend declares — so there is nothing to compare the SANs
against. A haproxy frontend's hostnames are a property of its rules rather than of
its configuration shape, which is a real limit and not a bug to be papered over
with a guess.

**Credential directives are recorded, never read.** `web02/htpasswd`,
`app01/htpasswd` and lb01's `userlist` are all in the inventory as the directives
that reference them, with their provenance, and nagipath never opens the first two
or interprets the third. `sudoers-nagipath` grants `openssl x509 -noout` and no way
at all to print the contents of `/etc/ssl/lab`, which is what makes it safe for
lb01's certificate to be a combined PEM: the expiry and the SANs come back, and
the private key sharing the file cannot.

The stats page behind that userlist is at <http://127.0.0.1:8404/stats>,
`statsviewer` / `nagipath-lab`. It is the running state the configuration cannot
tell you: which balancer members are up, which is drained, which is a standby.

**Search** (`/search`) covers both indexes — modelled directives, and the verbatim
text of every current file. `proxy_pass` finds the rules. `X-Frame-Options` returns
hits on two of the four nodes and none on the other two: web02's `add_header …
DENY` carrying a `shadowed` badge, app01's `Header set … SAMEORIGIN` at server
scope, and app01's `Header always set … DENY` inside the partner macro. Two
different values, three spellings with three different merge behaviours, nothing at
all on web05 — and the value that reaches a client on the documented request is
none of the ones anybody chose. `10.90.4.7` finds the dead address on all three
vendors at once: web05's upstream and auth pool, lb01's TCP passthrough and backup
server, app01's hot standby.

## The sudoers grant

`sudoers-nagipath` is what the nodes install, and it is deliberately close to
what the product documents. It is read-only by construction: every entry either
reports state or reads a bounded prefix of a file.

The one honest departure is `NAGIPATH_LAB_LAYOUT`, which repeats a few entries
against `/usr/local/...` because the upstream haproxy and httpd images install
there and a real Debian or RHEL host does not. It is a separate alias so it is
obvious which lines are the grant and which are an artefact of the lab.

Note what is *not* there: `sh`. nagipath runs each privileged command as its own
argv rather than through a shell, because `sudo sh -c *` is a root shell with a
narrow-grant label on it.

The grant is what makes web02's snapshot authoritative. `nginx -T` run as an
unprivileged user aborts on the pid file before printing a single line of
configuration, so nagipath tries it as the login user, sees it fail, and retries
it through the grant. web05 has no grant to retry through, which is precisely why
it falls back to a filesystem walk.

## Keeping the prose honest

`internal/trace/lab_test.go` builds the fleet from the configuration files in this
directory and asserts the claims above about the trace — without Docker, so it
runs in CI. Editing a config in a way that invalidates a sentence here fails
`go test ./...`. The lab is documentation, and documentation that is wrong is
worse than none.

## Housekeeping

```sh
docker compose logs -f nagipath
docker compose down            # keeps the database and host keys
docker compose down -v         # discards them; the next up starts clean
```

`keys/` and `certs/` are generated and git-ignored. A private key in a repository
is a private key on every laptop that clones it, lab or not.
