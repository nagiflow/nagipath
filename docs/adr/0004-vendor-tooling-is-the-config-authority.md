# Vendor tooling resolves configuration, not our own resolver

Effective Configuration comes from each Vendor's own inspection commands, not from a resolver we write. `nginx -T` returns the whole configuration with every include already resolved, banner-attributed per file. `httpd -V` reports the real `HTTPD_ROOT` and `SERVER_CONFIG_FILE` instead of us guessing paths, and `httpd -t -D DUMP_VHOSTS` / `DUMP_MODULES` / `DUMP_INCLUDES` return resolved Sites, loaded modules and the include tree. HAProxy has no include directive at all, so the running process's `-f` arguments are the complete file set, and `haproxy -vv` reports build capabilities.

The alternative — reading raw files and resolving includes, `<IfModule>` and `<IfDefine>` conditionals, `Define` substitution and directive inheritance ourselves, across every 2.4.x point release and every distribution layout — is months of work that produces a graph subtly wrong in exactly the places a prospect will check. Being confidently wrong is the failure mode this product cannot afford.

## Consequences

Collection needs to *execute* those binaries, which generally means `sudo`. We ship a copy-pasteable `sudoers` snippet granting exactly the read-only inspection commands plus reads of configuration and access-log paths, and nothing else — an easier security conversation than requesting a privileged account, because none of the granted commands can mutate anything. Where `sudo` is unavailable we fall back to reading raw files best-effort and label the result **degraded**; a partial graph is never presented as complete.

Snapshots still store the raw configuration text, so Provenance and diffs remain byte-exact and independent of the dump output.
