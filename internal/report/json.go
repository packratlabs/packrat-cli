package report

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/packratlabs/packrat-cli/internal/audit"
)

// JSON writes the stable JSON document for a.
func JSON(w io.Writer, a audit.Audit) error {
	doc, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding audit: %w", err)
	}
	if _, err := w.Write(append(doc, '\n')); err != nil {
		return fmt.Errorf("writing JSON: %w", err)
	}
	return nil
}
