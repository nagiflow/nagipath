package web

import (
	// Aliased because this package already has a template helper called csv.
	encsv "encoding/csv"
	"fmt"
	"net/http"
)

// writeCSV sends a list screen's current, filtered result set as a download. The
// design offers "Export CSV" on six screens; this is the whole of it, because the
// rows are already computed for the page and a second query would be a second
// answer to the same question.
//
// The export is the filter, not the page: an operator who narrowed a list and
// pressed Export means the narrowed list, all of it, not the fifty rows visible.
// The audit trail records the download, because an export leaves the product.
func (s *Server) writeCSV(w http.ResponseWriter, r *http.Request, name string, rows [][]string) {
	u := userOf(r)
	// len(rows)-1 because the first row is the header.
	s.DB.Audit(r.Context(), &u.ID, "export.csv", name, nil,
		fmt.Sprintf("%d row(s)", len(rows)-1))

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	// No timestamp in the filename: the rows already carry their own times, and two
	// exports a second apart collide in a way that matters to nobody.
	w.Header().Set("Content-Disposition", `attachment; filename="nagipath-`+name+`.csv"`)
	cw := encsv.NewWriter(w)
	// A write error here is a client that hung up mid-download. There is nothing to
	// report to and nothing to roll back, so it is dropped deliberately.
	_ = cw.WriteAll(rows)
}
