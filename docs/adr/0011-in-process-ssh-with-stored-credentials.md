# In-process SSH with UI-managed Credentials, rather than shelling out to the system client

> "Never blind trust-on-first-use" below is the default and stays the default; [ADR-0019](0019-host-key-tofu-is-an-opt-in-off-by-default.md) adds an explicit, audited, off-by-default setting to narrow it for deployments that choose to.

Credentials are entered in the web UI and stored encrypted, so SSH runs in-process via `golang.org/x/crypto/ssh`. Host keys are stored and require explicit first-use approval in the UI, rejecting the connection until approval is given — never blind trust-on-first-use. Bastions are a field on a Node or Cluster, implemented by dialling the bastion and tunnelling a second SSH client through it, which supports multi-hop by repetition. Authentication is by SSH key, SSH certificate, or a stored username/password. A password credential offers both the `password` and `keyboard-interactive` SSH methods, since a PAM-backed sshd advertises only the latter; whether the account is local or directory-backed is the managed host's business, so there is no separate LDAP credential kind.

Shelling out to the system `ssh` binary was seriously considered and rejected. It would inherit `~/.ssh/config`, `ProxyJump`, `known_hosts` policy and GSSAPI/Kerberos for free — genuinely attractive — but with UI-managed Credentials it would require materialising private keys to disk on every connection, which defeats encrypting them at all. Host-key approval as a UI action is better operator experience than editing `known_hosts` on a fleet tool's server. Kerberos and CyberArk profiles may be stored for enterprise inventory consistency, but collection refuses them until their GSSAPI and vault providers are configured.

## Consequences

Credentials resolve per-Node, then per-Cluster, then a default, so the common shape of one automation account plus a few exceptions needs no configuration. Each Credential exposes which Nodes currently resolve to it, giving key rotation a visible blast radius. Private key fields are write-only everywhere — API, UI and diagnostics bundle.

That resolution order has two holes, and both are accepted rather than fixed:

- **The Cluster tier cannot apply on first contact.** Cluster membership is a property of an Instance, and Instances do not exist until a Collection has succeeded — which requires a Credential. First contact therefore always resolves per-Node or default, never per-Cluster. Operators onboarding a Cluster whose Credential differs from the default must set it per-Node for the first Collection, or set the default to it. The UI says so where Clusters are created; silently falling through to a default that fails authentication is the outcome worth preventing.
- **A Node whose Instances span two Clusters with different Credentials falls through to the default.** No plurality winner is invented, because a wrong guess here is an authentication failure against a host the operator did not expect to be affected. The Node is flagged so the operator can resolve it per-Node.

Both are consequences of Cluster membership being derived rather than declared at the Node level, which is itself the right call — but they are the kind of behaviour a future reader will otherwise read as a bug. Per-zone Credential scoping, when the execution gateway arrives, is what actually removes them (see `OPEN-DECISIONS.md`).

Credentials are encrypted with AES-256-GCM under the Master Key, which is read from a `0600` key file (or environment variable) at startup. If the Master Key is missing or malformed the server refuses to start rather than generating a new one; silently regenerating is how a database becomes permanently undecryptable. Consequently a stolen database file yields inventory but no fleet access, and a lost Master Key means every Credential must be re-entered — the documentation must say both, loudly.
