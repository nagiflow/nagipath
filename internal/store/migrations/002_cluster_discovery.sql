-- Clusters are discovered, not declared: Instances whose parsed configuration is
-- identical are one Cluster. The hash of that configuration is the Cluster's
-- identity, so an operator's rename survives every recollection and a Cluster
-- that dissolves and re-forms comes back under its own name.
ALTER TABLE cluster ADD COLUMN config_hash TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX cluster_config_hash ON cluster(config_hash) WHERE config_hash <> '';
