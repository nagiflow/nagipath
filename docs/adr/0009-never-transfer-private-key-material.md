# Certificate metadata is extracted on the target, so private key bytes never leave the host

Certificate details are obtained by running `openssl x509` over the existing SSH session and transferring only the parsed metadata and the public certificate. Reading certificate files and transferring them was rejected because HAProxy's standard convention is a Combined PEM — one file holding the certificate, the chain *and* the private key — so any collector that fetches certificate files to read them necessarily pulls private keys across the wire and into the Snapshot store.

Where `openssl` is absent, files are read and `PRIVATE KEY` blocks stripped in memory before anything touches disk, and the Certificate records that its file was a Combined PEM — which is itself useful for the operator to see. Encrypted central storage of key material was rejected outright.

## Consequences

Snapshots cover **configuration files only**. Certificate and key files are not part of a Snapshot; only extracted Certificate metadata is.

This makes a strong and literally true security claim available: nagipath never transfers or stores private key material, and never reads key bytes at all on a host where `openssl` is present. The claim is only achievable if decided before the collector is written — retrofitting it after a collector that fetches whole files is substantially harder.
