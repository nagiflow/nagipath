package api

import (
	"database/sql"
	"fmt"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
)

// toPBProvenance ports internal/web/templates/parts/prov.html's link-building
// logic (internal/store.DriftFinding.Prov() carries the same four fields):
// no file recorded means no link, and the byte offset is optional even when
// a file is.
func toPBProvenance(path string, fileID, snapshotID, byteStart sql.NullInt64) *pb.Provenance {
	p := &pb.Provenance{Path: path, FileId: fileID.Int64, SnapshotId: snapshotID.Int64, ByteStart: byteStart.Int64}
	if fileID.Valid {
		p.Link = fmt.Sprintf("/snapshots/%d/file/%d", snapshotID.Int64, fileID.Int64)
		if byteStart.Valid {
			p.Link += fmt.Sprintf("?b=%d", byteStart.Int64)
		}
	}
	return p
}
