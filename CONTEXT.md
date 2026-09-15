# nagipath

nagipath is a vendor-neutral control plane for long-lived web server fleets. It discovers what is running across Apache, NGINX, HAProxy and IIS, shows how a request for a given hostname and path actually flows through them, and reports configuration drift and certificate exposure — without taking ownership of anyone's configuration.

## Language

### Fleet

**Node**:
A host that nagipath connects to and collects from. Identified by how it is reached, not by what runs on it.
_Avoid_: server, machine, box, target, host

**Instance**:
One running web server on a Node. A single Node may carry several Instances of the same or different Vendors.
_Avoid_: service, daemon, process, server

**Vendor**:
The web server product family an Instance belongs to — Apache, NGINX, HAProxy or IIS.
_Avoid_: product, flavour, type, engine

**Cluster**:
A set of Instances whose configuration is identical. Discovered from the configuration itself; the operator only names it.
_Avoid_: group, pool, farm, tier

**Golden Peer**:
The Cluster member explicitly designated as the Baseline for its peers. Absent a designation, the Cluster's majority configuration serves instead.
_Avoid_: master, reference node, source of truth

### Application

**Application**:
A named set of Entry Points with an owner. Its Instances, Sites, Upstreams and Certificates are derived by tracing from those Entry Points, never enumerated by hand.
_Avoid_: service, app, site, system, workload

**Entry Point**:
A hostname plus optional path prefix at which an Application is reached. The starting point of a Trace.
_Avoid_: URL, endpoint, ingress, route

### Configuration

**Collection**:
One run of gathering configuration and certificate metadata from a Node. Always initiated by nagipath, always over a connection the operator configured.
_Avoid_: scan, sync, poll, crawl

**Snapshot**:
The immutable configuration text captured by one Collection for one Instance. Never edited, never regenerated, retained for history.
_Avoid_: backup, dump, capture, version

**Effective Configuration**:
Configuration as the Vendor's own tooling reports it, with includes, conditionals and inheritance already resolved by that tooling.
_Avoid_: merged config, expanded config, running config, resolved config

**Config Object**:
A single normalized element parsed out of a Snapshot — a Listener, Site, Route, Upstream, Upstream Member or Certificate reference.
_Avoid_: entity, resource, node, element

**Provenance**:
The file and byte range in a Snapshot that a Config Object or Rule was parsed from. Every Config Object carries it.
_Avoid_: source ref, location, position, origin

**Opaque Directive**:
Configuration preserved verbatim in a Snapshot but deliberately not modelled as a Config Object. Searchable as text, never interpreted.
_Avoid_: unknown directive, unsupported config, passthrough

**Rule**:
One ordered directive within a scope, carrying its raw text, its Action Class and its Provenance. Unmodelled and custom directives are still Rules.
_Avoid_: directive, policy, rule set, statement

**Action Class**:
The closed taxonomy a Rule is sorted into — match, rewrite, redirect, header, auth, cache, rate-limit, proxy, access-control, other.
_Avoid_: type, category, kind, tag

### Topology

**Listener**:
An address, port and TLS posture on which an Instance accepts connections.
_Avoid_: bind, socket, port, frontend

**Site**:
The Vendor's host-level routing object — an NGINX `server` block, an Apache `VirtualHost`, an IIS site, an HAProxy frontend.
_Avoid_: vhost, virtual host, server block, host

**Route**:
A path or condition within a Site that selects a destination.
_Avoid_: location, path, mapping, rule

**Upstream**:
A named group of destinations a Route can send requests to.
_Avoid_: backend, pool, farm, cluster

**Upstream Member**:
One destination within an Upstream.
_Avoid_: server, node, host, endpoint

**Trace**:
The ordered chain of Hops a request for an Entry Point takes across the fleet.
_Avoid_: path, flow, chain, graph

**Hop**:
One step in a Trace — a Listener, Site, Route and Upstream on a single Instance.
_Avoid_: step, tier, layer, stage

**External Hop**:
A Hop whose destination is not a managed Instance. A Trace that leaves the fleet ends in one and says so, rather than ending silently.
_Avoid_: unknown node, dead end, black box, unmanaged

### Confidence

**Inferred**:
Derived from configuration alone. The default state of every Hop and every Trace edge.
_Avoid_: guessed, assumed, computed

**Candidate**:
A Rule that configuration says could apply to a path, with no confirmation that it did.
_Avoid_: possible, potential, maybe

**Observed Effect**:
A Rule whose result is visible in a Probe's response — a header that was set, a redirect that fired, an authentication challenge that was issued.
_Avoid_: partially verified, detected

**Verified**:
A Hop confirmed by evidence from the Vendor's own access log, correlated to a specific Probe.
_Avoid_: confirmed, proven, tested, validated

**Probe**:
A single operator-initiated GET or HEAD request against an Entry Point, sent to verify a Trace. Never scheduled, never automatic, always audited.
_Avoid_: test, check, health check, synthetic request, ping

### Change detection

**Drift**:
A divergence between an Instance's current configuration and its Baseline.
_Avoid_: change, delta, difference, violation

**Baseline**:
What Drift is measured against — either the Instance's previous Snapshot or its Cluster's Golden Peer or majority.
_Avoid_: desired state, golden config, expected config, target

### Certificates

**Certificate**:
A TLS certificate identified by its fingerprint, so the same material found on many Instances is one object.
_Avoid_: cert file, SSL cert, keypair, identity

**Certificate Binding**:
The link between a Certificate and the Listener or Site that serves it.
_Avoid_: assignment, attachment, usage, mapping

**Combined PEM**:
A file holding a Certificate and its private key together, as HAProxy conventionally uses. Recorded as a property of the Certificate because it constrains how that Certificate may be read.
_Avoid_: bundle, pem file, chain

### Access

**Credential**:
A stored SSH identity nagipath authenticates to Nodes with. Resolved per-Node, then per-Cluster, then a default.
_Avoid_: secret, account, login, key, auth

**Master Key**:
The secret, held outside the database, that every stored Credential is encrypted with.
_Avoid_: encryption key, secret key, root key

**Host Key Approval**:
The operator's explicit acceptance of a Node's SSH host key. Until it is given, nagipath runs no command on that Node — unless the off-by-default `tofu_enabled` setting has auto-approved the Node's first-ever key, which a key that would replace an approved one never is; that always still needs explicit approval.
_Avoid_: trust, fingerprint check, known host
