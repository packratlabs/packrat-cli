// Package report prints an Audit as a plain-text table or as JSON.
package report

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/packratlabs/packrat-cli/internal/audit"
)

// NoStack is the line printed when the repository is not a supported stack.
const NoStack = "No supported stack found (Rails is the only one yet)"

// Promise is always the last line of the table.
const Promise = "Nothing was changed. Nothing was sent."

// Text writes the table for a.
func Text(w io.Writer, a audit.Audit) error {
	var b strings.Builder
	fmt.Fprintf(&b, "packrat audit  %s\n\n", filepath.Base(a.Path))
	if a.Stack == nil {
		b.WriteString(NoStack + "\n\n")
	}
	b.WriteString("Already here\n  nothing\n\n")
	b.WriteString(Promise + "\n")
	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("writing table: %w", err)
	}
	return nil
}
