package tui

import (
	"fmt"
	"testing"

	appsearch "github.com/OleksandrBesan/tatami/internal/search"
	tea "github.com/charmbracelet/bubbletea"
)

func TestSearchControllerFocusAndActivation(t *testing.T) {
	controller := testSearchController(t)
	if !controller.InQueryFocus() {
		t.Fatal("controller should start in query focus")
	}

	typeKey(&controller, "be")
	if controller.Query() != "be" || len(controller.Rows()) != 1 {
		t.Fatalf("query=%q rows=%#v", controller.Query(), controller.Rows())
	}
	event, _ := controller.Update(controllerKeyMsg(tea.KeyDown))
	if !event.BrowseSelectionChanged || controller.InQueryFocus() || controller.ActiveIndex() != 0 {
		t.Fatalf("down event=%#v active=%d queryFocus=%v", event, controller.ActiveIndex(), controller.InQueryFocus())
	}
	event, _ = controller.Update(controllerKeyMsg(tea.KeyEnter))
	if !event.Activate {
		t.Fatalf("enter event=%#v", event)
	}
	event, _ = controller.Update(controllerKeyMsg(tea.KeyUp))
	if !event.Consumed || !controller.InQueryFocus() {
		t.Fatalf("top-row up event=%#v queryFocus=%v", event, controller.InQueryFocus())
	}
}

func TestSearchControllerEscapeSlashAndBrowseCommands(t *testing.T) {
	controller := testSearchController(t)
	typeKey(&controller, "b")
	controller.Update(controllerKeyMsg(tea.KeyDown))
	event, _ := controller.Update(runeKey('/'))
	if !event.Consumed || !controller.InQueryFocus() || controller.Query() != "b" {
		t.Fatalf("slash event=%#v query=%q", event, controller.Query())
	}
	event, _ = controller.Update(controllerKeyMsg(tea.KeyEsc))
	if !event.Consumed || controller.Query() != "" {
		t.Fatalf("first escape event=%#v query=%q", event, controller.Query())
	}
	event, _ = controller.Update(controllerKeyMsg(tea.KeyEsc))
	if event.Consumed {
		t.Fatalf("empty escape should be delegated: %#v", event)
	}
	controller.Update(controllerKeyMsg(tea.KeyDown))
	event, _ = controller.Update(runeKey('d'))
	if event.Consumed {
		t.Fatalf("browse mnemonic should be delegated: %#v", event)
	}
	event, _ = controller.Update(controllerKeyMsg(tea.KeyCtrlC))
	if event.Consumed {
		t.Fatalf("ctrl+c should be global: %#v", event)
	}
}

func TestSearchControllerPreservesStableIdentityAcrossGenerations(t *testing.T) {
	controller := testSearchController(t)
	controller.Update(controllerKeyMsg(tea.KeyDown))
	controller.Move(1)
	id, _ := controller.ActiveDocumentID()
	if id != "beta" {
		t.Fatalf("selected %q, want beta", id)
	}

	docs := []appsearch.Document{
		{ID: "beta", Primary: "Beta", Ordinal: 0},
		{ID: "alpha", Primary: "Alpha", Ordinal: 1},
	}
	rows := []searchRow{{RowID: "row-beta", DocumentID: "beta"}, {RowID: "row-alpha", DocumentID: "alpha"}}
	if err := controller.ReplaceDocuments(2, docs, rows); err != nil {
		t.Fatal(err)
	}
	id, _ = controller.ActiveDocumentID()
	if id != "beta" {
		t.Fatalf("replacement selected %q, want beta", id)
	}
	if err := controller.ReplaceDocuments(1, []appsearch.Document{{ID: "stale", Primary: "Stale"}}, nil); err != nil {
		t.Fatal(err)
	}
	id, _ = controller.ActiveDocumentID()
	if id != "beta" {
		t.Fatalf("stale replacement selected %q", id)
	}
}

func TestSearchControllerQueryFocusEnterActivatesRankedTopResult(t *testing.T) {
	controller := newSearchController("Search...")
	docs := []appsearch.Document{
		{ID: "alpha", Primary: "Alpha match", Ordinal: 0},
		{ID: "beta", Primary: "Beta match", Ordinal: 1},
	}
	rows := []searchRow{{RowID: "row-alpha", DocumentID: "alpha"}, {RowID: "row-beta", DocumentID: "beta"}}
	if err := controller.ReplaceDocuments(1, docs, rows); err != nil {
		t.Fatal(err)
	}
	controller.Update(controllerKeyMsg(tea.KeyDown))
	controller.Move(1)
	controller.Update(runeKey('/'))
	typeKey(&controller, "match")

	event, _ := controller.Update(controllerKeyMsg(tea.KeyEnter))
	id, _ := controller.ActiveDocumentID()
	if !event.Activate || id != "alpha" {
		t.Fatalf("query-focus enter event=%#v selected=%q, want ranked top alpha", event, id)
	}
}

func TestSearchControllerViewportKeepsActiveRowVisible(t *testing.T) {
	controller := newSearchController("Search...")
	docs := make([]appsearch.Document, 20)
	rows := make([]searchRow, 20)
	for index := range docs {
		id := appsearch.ID(fmt.Sprintf("item-%02d", index))
		docs[index] = appsearch.Document{ID: id, Primary: string(id), Ordinal: index}
		rows[index] = searchRow{RowID: id, DocumentID: id}
	}
	if err := controller.ReplaceDocuments(1, docs, rows); err != nil {
		t.Fatal(err)
	}
	controller.SetHeight(8)
	controller.SelectLast()
	start, visible := controller.VisibleRows(4, 1)
	if start != 16 || len(visible) != 4 || visible[len(visible)-1].DocumentID != "item-19" {
		t.Fatalf("visible start=%d rows=%#v", start, visible)
	}
}

func testSearchController(t *testing.T) searchController {
	t.Helper()
	controller := newSearchController("Search...")
	docs := []appsearch.Document{
		{ID: "alpha", Primary: "Alpha", Ordinal: 0},
		{ID: "beta", Primary: "Beta", Ordinal: 1},
	}
	rows := []searchRow{{RowID: "row-alpha", DocumentID: "alpha"}, {RowID: "row-beta", DocumentID: "beta"}}
	if err := controller.ReplaceDocuments(1, docs, rows); err != nil {
		t.Fatal(err)
	}
	return controller
}

func typeKey(controller *searchController, value string) {
	for _, r := range value {
		controller.Update(runeKey(r))
	}
}

func runeKey(r rune) tea.KeyMsg                   { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }
func controllerKeyMsg(key tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: key} }
