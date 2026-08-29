package api

import (
	// Aliased because this package already has a JSON encoder called json.
	encsv "encoding/csv"
	"fmt"
	"net/http"
)

// writeCSV ports internal/web/export.go's writeCSV: a list screen's current,
// filtered result set as a download, audited because an export leaves the
// product. See that file's doc comment for why the export is the filter, not
// the page.
func (s *Server) writeCSV(w http.ResponseWriter, r *http.Request, name string, rows [][]string) {
	u := userOf(r)
	s.DB.Audit(r.Context(), &u.ID, "export.csv", name, nil,
		fmt.Sprintf("%d row(s)", len(rows)-1))

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="nagipath-`+name+`.csv"`)
	cw := encsv.NewWriter(w)
	_ = cw.WriteAll(rows)
}
