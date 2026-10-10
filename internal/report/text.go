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

const (
	maxWidth     = 99
	continuation = "      "
	// A repository with opentelemetry-instrumentation-all locks about fifty of these as
	// dependencies. One row each would bury every other finding, so the table counts them. The
	// JSON lists them all.
	instrumentationPrefix = "opentelemetry-instrumentation-"
)

type row struct {
	vendor, gem, where, what, notes string
}

// Text writes the table for a.
func Text(w io.Writer, a audit.Audit) error {
	var b strings.Builder
	b.WriteString(header(a) + "\n\n")
	if a.Stack == nil {
		b.WriteString(NoStack + "\n\n")
	}
	for _, note := range a.Notes {
		b.WriteString(note + "\n")
	}
	if len(a.Notes) > 0 {
		b.WriteString("\n")
	}
	b.WriteString("Already here\n")
	writeRows(&b, sdkRows(a.SDKs))
	b.WriteString("\n" + Promise + "\n")
	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("writing table: %w", err)
	}
	return nil
}

func header(a audit.Audit) string {
	head := "packrat audit  " + filepath.Base(a.Path)
	if a.Stack == nil {
		return head
	}
	parts := []string{strings.TrimSpace("Rails " + a.Stack.Version)}
	if a.Stack.Ruby != "" {
		parts = append(parts, "Ruby "+a.Stack.Ruby)
	}
	return head + "  " + strings.Join(parts, ", ")
}

func sdkRows(sdks []audit.Finding) []row {
	var rows []row
	count := transitiveInstrumentations(sdks)
	counted := false
	for _, sdk := range sdks {
		if !isTransitiveInstrumentation(sdk) {
			rows = append(rows, sdkRow(sdk))
			continue
		}
		if !counted {
			gems := fmt.Sprintf("%s* (%d gems)", instrumentationPrefix, count)
			if count == 1 {
				gems = instrumentationPrefix + "* (1 gem)"
			}
			rows = append(rows, row{vendor: sdk.Vendor, gem: gems, what: "traces (dependency)"})
			counted = true
		}
	}
	return rows
}

func sdkRow(sdk audit.Finding) row {
	what := strings.Join(sdk.Provides, ", ")
	if !sdk.Direct {
		what += " (dependency)"
	}
	if !sdk.Production() {
		what += " (" + strings.Join(sdk.Groups, ", ") + " only)"
	}
	where := ""
	if len(sdk.Evidence) > 0 {
		where = fmt.Sprintf("%s:%d", sdk.Evidence[0].Path, sdk.Evidence[0].Line)
	}
	return row{
		vendor: sdk.Vendor,
		gem:    strings.TrimSpace(sdk.Gem + " " + sdk.Version),
		where:  where,
		what:   what,
		notes:  strings.Join(sdk.Notes, "; "),
	}
}

func isTransitiveInstrumentation(sdk audit.Finding) bool {
	return !sdk.Direct && strings.HasPrefix(sdk.Gem, instrumentationPrefix)
}

func transitiveInstrumentations(sdks []audit.Finding) int {
	count := 0
	for _, sdk := range sdks {
		if isTransitiveInstrumentation(sdk) {
			count++
		}
	}
	return count
}

func writeRows(b *strings.Builder, rows []row) {
	if len(rows) == 0 {
		b.WriteString("  nothing\n")
		return
	}
	vendorWidth, gemWidth, whereWidth := 0, 0, 0
	for _, r := range rows {
		vendorWidth = max(vendorWidth, len(r.vendor))
		gemWidth = max(gemWidth, len(r.gem))
		whereWidth = max(whereWidth, len(r.where))
	}
	for _, r := range rows {
		left := fmt.Sprintf("  %-*s  %-*s", vendorWidth, r.vendor, gemWidth, r.gem)
		right := r.what
		if r.where != "" {
			right = fmt.Sprintf("%-*s  %s", whereWidth, r.where, r.what)
		}
		b.WriteString(fit(left, right, r.notes) + "\n")
	}
}

// fit keeps a row under 100 columns. The notes move to a second line first, then the evidence.
func fit(left, right, notes string) string {
	line := left + "  " + right
	switch {
	case notes == "":
	case len(line)+len("; ")+len(notes) <= maxWidth:
		return line + "; " + notes
	case len(line) <= maxWidth:
		return line + "\n" + continuation + notes
	default:
		right += "; " + notes
	}
	if len(line) <= maxWidth {
		return line
	}
	return strings.TrimRight(left, " ") + "\n" + continuation + right
}
