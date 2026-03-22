// Package render converts SteerSpec entity files into human-readable formats.
package render

import (
	"fmt"
	"io"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// Format represents an output format.
type Format string

// Supported output formats.
const (
	FormatMarkdown Format = "markdown"
)

// Renderer converts an entity file to a specific output format.
type Renderer interface {
	Render(w io.Writer, ef *entity.File) error
}

// New returns a Renderer for the given format.
func New(format Format, opts ...Option) (Renderer, error) {
	switch format {
	case FormatMarkdown:
		return NewMarkdownRenderer(opts...), nil
	default:
		return nil, fmt.Errorf("unsupported format: %q", format)
	}
}
