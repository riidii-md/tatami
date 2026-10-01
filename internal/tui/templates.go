package tui

import (
	"strings"

	appsearch "github.com/OleksandrBesan/tatami/internal/search"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

// TemplateView displays the template picker
type TemplateView struct {
	templates  []workspace.Template
	search     searchController
	mobileMode bool
}

// SetMobileMode enables numbered, compact menu rendering.
func (t *TemplateView) SetMobileMode(enabled bool) {
	t.mobileMode = enabled
}

// NewTemplateView creates a new template view
func NewTemplateView() *TemplateView {
	view := &TemplateView{
		templates: workspace.GetTemplates(),
		search:    newSearchController("templates"),
	}
	view.rebuildSearch()
	return view
}

// Selected returns the currently selected template
func (t *TemplateView) Selected() *workspace.Template {
	id, ok := t.search.ActiveDocumentID()
	if !ok {
		return nil
	}
	for i := range t.templates {
		if templateSearchID(t.templates[i]) == id {
			return &t.templates[i]
		}
	}
	return nil
}

// Update handles input for the template view
func (t *TemplateView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		event, cmd := t.handleKey(msg)
		if event.Consumed {
			return cmd
		}
		switch msg.String() {
		case "j":
			t.search.Move(1)
		case "k":
			t.search.Move(-1)
		case "g":
			t.search.SelectFirst()
		case "G":
			t.search.SelectLast()
		default:
			if t.mobileMode {
				t.search.SelectVisibleChoice(msg.String(), 8, 1)
			}
		}
	}
	return nil
}

func (t *TemplateView) handleKey(msg tea.KeyMsg) (searchEvent, tea.Cmd) {
	return t.search.Update(msg)
}

func (t *TemplateView) rebuildSearch() {
	documents := make([]appsearch.Document, 0, len(t.templates))
	rows := make([]searchRow, 0, len(t.templates))
	for index, template := range t.templates {
		id := templateSearchID(template)
		documents = append(documents, appsearch.Document{
			ID:        id,
			Kind:      "template",
			Primary:   template.Name,
			Secondary: template.Description,
			Ordinal:   index,
		})
		rows = append(rows, searchRow{RowID: id, DocumentID: id})
	}
	_ = t.search.ReplaceDocuments(1, documents, rows)
}

func templateSearchID(template workspace.Template) appsearch.ID {
	return appsearch.ID("template:" + template.Name)
}

// View renders the template view
func (t *TemplateView) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Choose Layout Template"))
	b.WriteString("\n")
	b.WriteString(t.search.QueryView())
	b.WriteString("\n\n")

	start, rows := t.search.VisibleRows(8, 1)
	for visibleIndex, row := range rows {
		i := start + visibleIndex
		tmpl := t.templateByID(row.DocumentID)
		if tmpl == nil {
			continue
		}
		cursor := choicePrefix(t.mobileMode, visibleIndex, i == t.search.ActiveIndex())
		style := normalStyle
		if i == t.search.ActiveIndex() {
			style = selectedStyle
		}

		name := style.Render(tmpl.Name)
		desc := ""
		if !t.mobileMode {
			desc = mutedStyle.Render(" - " + tmpl.Description)
		}
		b.WriteString(cursor + name + desc + "\n")
	}
	if len(t.search.Rows()) == 0 {
		b.WriteString(mutedStyle.Render("No matching templates."))
		b.WriteString("\n")
	}
	if t.search.Truncated() {
		b.WriteString(mutedStyle.Render("Results truncated."))
		b.WriteString("\n")
	}

	help := "\n[type]search  [↓]browse  [enter]select  [esc]clear/cancel"
	if t.mobileMode {
		help = "\n[type]search  [↓]browse  [1-9]select in browse  [enter]apply  [b]back"
	}
	b.WriteString(helpStyle.Render(help))

	return renderPanel(b.String(), t.mobileMode)
}

func (t *TemplateView) templateByID(id appsearch.ID) *workspace.Template {
	for i := range t.templates {
		if templateSearchID(t.templates[i]) == id {
			return &t.templates[i]
		}
	}
	return nil
}
