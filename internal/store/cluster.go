package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// A Cluster is discovered, never typed in: Instances whose parsed configuration is
// identical are one Cluster, and the hash of that configuration is its identity.
// The operator's only say is the name.
//
// Identical means identical — every Rule, Site, Route and Upstream keyed the way
// the Drift comparison keys them, so a Cluster and a Drift report can never
// disagree about what "the same" means. That also means a discovered Cluster is a
// safe Drift baseline: on the day it forms every member matches by construction,
// and the first divergence is exactly what Drift is for.

// ReconcileClusters groups the parsed Instances by their configuration and makes
// each group of two or more a Cluster, creating what is new and clearing the
// membership of Instances whose configuration has since diverged.
//
// Empty Clusters are kept, not deleted: the row is where the operator's name
// lives, so a fleet that diverges during a rollout and converges again afterwards
// keeps the name it was given.
func (db *DB) ReconcileClusters(ctx context.Context) error {
	list, err := db.Instances(ctx)
	if err != nil {
		return err
	}
	// ponytail: every parsed instance is fingerprinted on every call. It is one
	// query per instance over rows already indexed by snapshot; move it behind a
	// stored hash on the snapshot when a fleet is big enough to notice.
	groups := map[string][]Instance{}
	for _, in := range list {
		if in.ParseState != "parsed" {
			continue
		}
		snap, err := db.CurrentSnapshot(ctx, in.ID)
		if err != nil {
			continue
		}
		hash, err := db.configHash(ctx, in.Vendor, snap.ID)
		if err != nil || hash == "" {
			continue
		}
		groups[hash] = append(groups[hash], in)
	}

	want := map[int64]int64{} // instance -> cluster
	hashes := make([]string, 0, len(groups))
	for h := range groups {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)
	for _, h := range hashes {
		members := groups[h]
		if len(members) < 2 {
			continue
		}
		id, err := db.clusterForHash(ctx, h, members)
		if err != nil {
			return err
		}
		for _, in := range members {
			want[in.ID] = id
		}
	}

	for _, in := range list {
		target, inGroup := want[in.ID]
		switch {
		case inGroup && (!in.ClusterID.Valid || in.ClusterID.Int64 != target):
			if _, err := db.W.ExecContext(ctx,
				`UPDATE instance SET cluster_id = ? WHERE id = ?`, target, in.ID); err != nil {
				return err
			}
		case !inGroup && in.ClusterID.Valid:
			if _, err := db.W.ExecContext(ctx,
				`UPDATE instance SET cluster_id = NULL WHERE id = ?`, in.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// clusterForHash is the Cluster for one configuration, created on first sight.
func (db *DB) clusterForHash(ctx context.Context, hash string, members []Instance) (int64, error) {
	var id int64
	err := db.R.QueryRowContext(ctx,
		`SELECT id FROM cluster WHERE config_hash = ?`, hash).Scan(&id)
	if err == nil {
		return id, nil
	}
	name := clusterName(members, hash)
	res, err := db.W.ExecContext(ctx,
		`INSERT INTO cluster (name, description, config_hash, created_at) VALUES (?,?,?,?)`,
		name, "", hash, Now())
	if err != nil {
		return 0, err
	}
	id, _ = res.LastInsertId()
	db.Audit(ctx, nil, "cluster.discover", "cluster", &id,
		fmt.Sprintf("%d instances with identical %s configuration", len(members), members[0].Vendor))
	return id, nil
}

// clusterName is a starting name the operator will probably replace. The vendor
// and the size are what the group is; the hash suffix only keeps names unique.
func clusterName(members []Instance, hash string) string {
	return fmt.Sprintf("%s-%dx-%s", members[0].Vendor, len(members), hash[:6])
}

// RenameCluster is the one thing an operator decides about a Cluster.
func (db *DB) RenameCluster(ctx context.Context, id int64, name string, by *int64) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("a cluster needs a name")
	}
	if _, err := db.W.ExecContext(ctx,
		`UPDATE cluster SET name = ? WHERE id = ?`, name, id); err != nil {
		return err
	}
	db.Audit(ctx, by, "cluster.rename", "cluster", &id, name)
	return nil
}

// configHash identifies a Snapshot's configuration. Provenance is deliberately
// excluded: the same configuration split across different filenames is the same
// configuration, and two hosts that agree on every directive are a cluster even
// when one of them keeps its sites in one file.
func (db *DB) configHash(ctx context.Context, vendor string, snapshotID int64) (string, error) {
	objs, err := db.driftObjects(ctx, snapshotID)
	if err != nil || len(objs) == 0 {
		return "", err
	}
	keys := make([]string, 0, len(objs))
	for k, o := range indexObjects(objs) {
		keys = append(keys, k+"\x00"+strings.TrimSpace(o.Text))
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(vendor + "\x00" + strings.Join(keys, "\n")))
	return hex.EncodeToString(sum[:]), nil
}
