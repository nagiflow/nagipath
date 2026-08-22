# Certificate metadata is extracted on the target, so private key bytes never leave the host

Certificate details are obtained by running `openssl x509` over the existing SSH session and transferring only the parsed metadata and the public certificate. Reading certificate files and transferring them was rejected because HAProxy's standard convention is a Combined PEM — one file holding the certificate, the chain *and* the private key — so any collector that fetches certificate files to read them necessarily pulls private keys across the wire and into the Snapshot store.

Where `openssl` is absent (or the read fails for any other reason), the collector does not fall back to reading the certificate file itself — that Binding is skipped rather than guessed at, so a certificate never appears with fabricated or partial metadata. An in-memory strip-and-parse fallback was considered and rejected: it would still require opening a file that may be a Combined PEM, and "we stripped the key bytes before anything touched disk" is a claim about *this build*, not one an auditor can verify from the outside. Skipping the binding is a claim with no fallback path to get wrong — the honest cost is an incomplete certificate inventory rather than a wrong one, and *that* gap belongs in a future collection-health signal, not in a code path that opens the file to avoid it.

## Consequences

Snapshots cover **configuration files only**. Certificate and key files are not part of a Snapshot; only extracted Certificate metadata is.

This makes a strong and literally true security claim available: nagipath never transfers or stores private key material, and never reads key bytes at all, on any host, regardless of whether `openssl` is present. The claim is only achievable if decided before the collector is written — retrofitting it after a collector that fetches whole files is substantially harder.
