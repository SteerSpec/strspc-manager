package render

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"text/template"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
)

//go:embed templates/default.md.tmpl
var defaultTemplateFS embed.FS

// Option configures a MarkdownRenderer.
type Option func(*MarkdownRenderer)

// WithTemplate overrides the built-in template with a custom template file.
func WithTemplate(path string) Option {
	return func(r *MarkdownRenderer) {
		r.templatePath = path
	}
}

// WithRuleIDFormatter overrides the RLIFRMT default rule identifier format,
// typically with NewRuleIDFormatter of the entity's Realm.
func WithRuleIDFormatter(f RuleIDFormatter) Option {
	return func(r *MarkdownRenderer) {
		r.formatter = f
	}
}

// Chain is the resolved inheritance chain of one entity, as realm-resolve
// returns it: the inheriting entity and its ancestors, nearest first.
type Chain struct {
	// Entity is the inheriting entity with its namespace.
	Entity inherit.Hop
	// Ancestors are the inherited entities, nearest first.
	Ancestors []inherit.Hop
}

// WithInheritance supplies the resolved inheritance chains of the rendered
// entities, keyed by EUID. An entity with a chain gets an "Inherited rules"
// section listing the rules it inherits (ENT-009), each with its owner and
// state, and marking those its own rules override.
func WithInheritance(chains map[string]Chain) Option {
	return func(r *MarkdownRenderer) {
		r.chains = chains
	}
}

// MarkdownRenderer renders entity files to Markdown using Go templates.
type MarkdownRenderer struct {
	formatter    RuleIDFormatter
	templatePath string
	chains       map[string]Chain

	tmplOnce sync.Once
	tmpl     *template.Template
	tmplErr  error
}

// NewMarkdownRenderer creates a new Markdown renderer with optional configuration.
func NewMarkdownRenderer(opts ...Option) *MarkdownRenderer {
	r := &MarkdownRenderer{
		formatter: DefaultRuleIDFormatter(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *MarkdownRenderer) getTemplate() (*template.Template, error) {
	r.tmplOnce.Do(func() {
		r.tmpl, r.tmplErr = r.parseTemplate()
	})
	return r.tmpl, r.tmplErr
}

// renderContext holds the data passed to the template.
type renderContext struct {
	entity.File
	// Depth is the nesting level of the entity, 1 for the top-level file;
	// heading levels derive from it.
	Depth int
	// NotesByRule groups the file's notes by the rule ID they reference.
	NotesByRule map[string][]entity.Note
	// SubEntities holds the render contexts of the nested entity files.
	SubEntities []renderContext
	// Inherited lists the rules the entity inherits, when its chain was
	// supplied with WithInheritance.
	Inherited []InheritedRule
}

// InheritedRule is one row of an entity's "Inherited rules" section.
type InheritedRule struct {
	// ID is the rule ID qualified with its owner's Realm.
	ID string
	// Body is the rule text.
	Body string
	// State is the rule's state in its owner.
	State string
	// Owner is the EUID of the entity that owns the rule, and OwnerRealm its
	// Realm.
	Owner      string
	OwnerRealm string
	// OverriddenBy is the ID of the entity's own rule that replaces this one
	// (RUL-009), empty when none does.
	OverriddenBy string
}

// inheritedRows lists what c.Entity inherits from c.Ancestors: every
// inherited rule, marked with the entity's own rule that overrides it.
func inheritedRows(c Chain) []InheritedRule {
	replacedBy := make(map[string]string)
	for _, r := range inherit.Effective(c.Entity, c.Ancestors, nil) {
		if r.InheritedBy == "" && r.Overrides != "" {
			replacedBy[r.Overrides] = r.Rule.ID
		}
	}
	inherited := inherit.Inherited(c.Ancestors)
	rows := make([]InheritedRule, 0, len(inherited))
	for _, r := range inherited {
		rows = append(rows, InheritedRule{
			ID:           r.QualifiedID(),
			Body:         r.Rule.Body,
			State:        r.Rule.State,
			Owner:        r.Owner,
			OwnerRealm:   r.OwnerRealm,
			OverriddenBy: replacedBy[r.QualifiedID()],
		})
	}
	return rows
}

func (r *MarkdownRenderer) newRenderContext(ef *entity.File, depth int) renderContext {
	notesByRule := make(map[string][]entity.Note, len(ef.Notes))
	for _, n := range ef.Notes {
		notesByRule[n.RuleRef] = append(notesByRule[n.RuleRef], n)
	}

	subCtxs := make([]renderContext, len(ef.SubEntities))
	for i := range ef.SubEntities {
		subCtxs[i] = r.newRenderContext(&ef.SubEntities[i], depth+1)
	}

	ctx := renderContext{
		File:        *ef,
		Depth:       depth,
		NotesByRule: notesByRule,
		SubEntities: subCtxs,
	}
	if c, ok := r.chains[ef.Entity.ID]; ok {
		ctx.Inherited = inheritedRows(c)
	}
	return ctx
}

// Render writes the entity file as Markdown to w.
func (r *MarkdownRenderer) Render(w io.Writer, ef *entity.File) error {
	tmpl, err := r.getTemplate()
	if err != nil {
		return err
	}

	ctx := r.newRenderContext(ef, 1)

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	var output string
	if r.templatePath == "" {
		// Clean up excessive blank lines (3+ consecutive newlines → 2) for the default template.
		output = collapseBlankLines(buf.String())
	} else {
		// Preserve exact formatting for custom templates.
		output = buf.String()
	}

	_, err = io.WriteString(w, output)
	return err
}

func (r *MarkdownRenderer) parseTemplate() (*template.Template, error) {
	// cachedTmpl holds the parsed template so renderEntity doesn't re-parse.
	var cachedTmpl *template.Template

	funcMap := template.FuncMap{
		"heading": func(depth int) string {
			if depth < 1 {
				depth = 1
			}
			if depth > 6 {
				depth = 6
			}
			return strings.Repeat("#", depth)
		},
		"add": func(a, b int) int {
			return a + b
		},
		"formatRuleID": func(rule entity.Rule) string {
			return r.formatter.Format(&rule)
		},
		"hashOrNone": func(h *string) string {
			if h == nil {
				return "none"
			}
			return *h
		},
		"notesFor": func(notesByRule map[string][]entity.Note, ruleID string) []entity.Note {
			return notesByRule[ruleID]
		},
		"renderEntity": func(ctx renderContext) (string, error) {
			var buf bytes.Buffer
			if err := cachedTmpl.Execute(&buf, ctx); err != nil {
				return "", err
			}
			return buf.String(), nil
		},
	}

	var (
		data []byte
		err  error
		name string
	)

	if r.templatePath != "" {
		data, err = os.ReadFile(r.templatePath)
		name = "custom"
	} else {
		data, err = defaultTemplateFS.ReadFile("templates/default.md.tmpl")
		name = "default"
	}
	if err != nil {
		return nil, fmt.Errorf("reading template: %w", err)
	}

	cachedTmpl, err = template.New(name).Funcs(funcMap).Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}

	return cachedTmpl, nil
}

func collapseBlankLines(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	newlines := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			newlines++
			if newlines <= 2 {
				b.WriteByte('\n')
			}
		} else {
			newlines = 0
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
