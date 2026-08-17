package output

import (
	"encoding/json"
	"io"
)

// JSON renders the whole report, measurements included, for whatever comes
// next: a spreadsheet, a plot, a diff against last season's run.
func JSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
