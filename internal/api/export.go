package api

import (
	encsv "encoding/csv"
	"fmt"
	"net/http"
)

// writeCSV ports internal/web/export.go's writeCSV: every export in this
// package (audit, collections) is a plain download, not a proto response —
// there is no TypeScript consumer decoding this, just a browser save.
func (s *Server) writeCSV(w http.ResponseWriter, r *http.Request, name string, rows [][]string) {
	u := userOf(r)
	// len(rows)-1 because the first row is the header.
	_ = s.DB.Audit(r.Context(), &u.ID, "export.csv", name, nil, fmt.Sprintf("%d row(s)", len(rows)-1))

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="nagipath-`+name+`.csv"`)
	cw := encsv.NewWriter(w)
	_ = cw.WriteAll(rows)
}
