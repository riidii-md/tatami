package search

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCatalogValidation(t *testing.T) {
	tests := []struct {
		name string
		docs []Document
		max  int
		want error
	}{
		{name: "invalid limit", max: 0, want: ErrInvalidLimit},
		{name: "empty ID", max: 1, docs: []Document{{Primary: "x"}}, want: ErrEmptyID},
		{name: "duplicate ID", max: 1, docs: []Document{{ID: "x"}, {ID: "x"}}, want: ErrDuplicateID},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewCatalog(test.docs, test.max)
			if !errors.Is(err, test.want) {
				t.Fatalf("NewCatalog() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestMatchRequiresAllNormalizedTokensAcrossFields(t *testing.T) {
	catalog := mustCatalog(t, []Document{{
		ID:        "workspace",
		Primary:   "Résumé App",
		Secondary: "Kyiv Tools",
		Fields:    []Field{{Name: "repository", Value: "github.com/Riidii/Tatami", Class: MetadataField}},
	}})

	for _, query := range []string{"  RÉSUMÉ   KYIV ", "app tatami", "résumé github"} {
		if got := catalog.Match(query, MaxResults); len(got.Matches) != 1 {
			t.Fatalf("Match(%q) = %#v, want one result", query, got)
		} else if strings.Contains(query, "tatami") || strings.Contains(query, "github") {
			if got.Matches[0].MatchedField != "repository" {
				t.Fatalf("Match(%q) explanation = %#v", query, got.Matches[0])
			}
		}
	}
	if got := catalog.Match("résumé missing", MaxResults); len(got.Matches) != 0 {
		t.Fatalf("Match() = %#v, want no results", got)
	}
}

func TestMatchRankingAndExplanation(t *testing.T) {
	docs := []Document{
		{ID: "metadata", Primary: "Alpha", Fields: []Field{{Name: "repository", Value: "host/tatami", Class: MetadataField}}, Ordinal: 0},
		{ID: "secondary-substring", Primary: "Bravo", Secondary: "A tatami workspace", Ordinal: 1},
		{ID: "secondary-prefix", Primary: "Charlie", Secondary: "Tatami workspace", Ordinal: 2},
		{ID: "primary-substring", Primary: "MyTatami", Ordinal: 3},
		{ID: "primary-word", Primary: "Open Tatami", Ordinal: 4},
		{ID: "primary-exact", Primary: "Tatami", Ordinal: 5},
	}
	got := mustCatalog(t, docs).Match("tatami", MaxResults)
	want := []ID{"primary-exact", "primary-word", "primary-substring", "secondary-prefix", "secondary-substring", "metadata"}
	if len(got.Matches) != len(want) {
		t.Fatalf("Match count = %d, want %d", len(got.Matches), len(want))
	}
	for i := range want {
		if got.Matches[i].ID != want[i] {
			t.Fatalf("Match[%d] = %q, want %q", i, got.Matches[i].ID, want[i])
		}
	}
	last := got.Matches[len(got.Matches)-1]
	if last.MatchedField != "repository" || last.MatchedValue != "host/tatami" {
		t.Fatalf("metadata explanation = %#v", last)
	}
	if got.Matches[0].MatchedField != "" {
		t.Fatalf("primary explanation should be empty: %#v", got.Matches[0])
	}
}

func TestMatchUsesBoostThenOrdinalForTies(t *testing.T) {
	docs := []Document{
		{ID: "later", Primary: "Tatami one", Ordinal: 5},
		{ID: "boosted", Primary: "Tatami two", Ordinal: 9, Boost: 3},
		{ID: "earlier", Primary: "Tatami three", Ordinal: 1},
	}
	got := mustCatalog(t, docs).Match("tat", MaxResults)
	want := []ID{"boosted", "earlier", "later"}
	for i := range want {
		if got.Matches[i].ID != want[i] {
			t.Fatalf("Match[%d] = %q, want %q", i, got.Matches[i].ID, want[i])
		}
	}
}

func TestCatalogAndResultTruncationAreDeterministic(t *testing.T) {
	docs := make([]Document, 6)
	for i := range docs {
		docs[i] = Document{ID: ID(fmt.Sprintf("doc-%d", i)), Primary: "match", Ordinal: i}
	}
	catalog, err := NewCatalog(docs, 4)
	if err != nil {
		t.Fatal(err)
	}
	got := catalog.Match("match", 2)
	if !got.Truncated || got.Total != 4 || len(got.Matches) != 2 {
		t.Fatalf("Match() = %#v", got)
	}
	if got.Matches[0].ID != "doc-0" || got.Matches[1].ID != "doc-1" {
		t.Fatalf("truncated order = %#v", got.Matches)
	}
}

func TestDuplicateVisibleLabelsKeepDistinctIDs(t *testing.T) {
	got := mustCatalog(t, []Document{
		{ID: "local:app", Primary: "app", Ordinal: 0},
		{ID: "remote:host:app", Primary: "app", Ordinal: 1},
	}).Match("app", MaxResults)
	if len(got.Matches) != 2 || got.Matches[0].ID == got.Matches[1].ID {
		t.Fatalf("Match() = %#v", got)
	}
}

func BenchmarkMatch_MaxCatalog(b *testing.B) {
	docs := benchmarkDocuments()
	catalog, err := NewCatalog(docs, MaxDocuments)
	if err != nil {
		b.Fatal(err)
	}
	for _, query := range []string{"workspace", "workspace github", "not-present", "github"} {
		b.Run(query, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = catalog.Match(query, MaxResults)
			}
		})
	}
}

func BenchmarkBuildDocuments_MaxCatalog(b *testing.B) {
	docs := benchmarkDocuments()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := NewCatalog(docs, MaxDocuments); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkDocuments() []Document {
	docs := make([]Document, MaxDocuments)
	for i := range docs {
		docs[i] = Document{
			ID:        ID(fmt.Sprintf("workspace-%05d", i)),
			Primary:   fmt.Sprintf("Workspace %05d", i),
			Secondary: fmt.Sprintf("folder/team-%03d", i%250),
			Fields: []Field{{
				Name:  "repository",
				Value: fmt.Sprintf("github.com/riidii/project-%04d", i%4096),
				Class: MetadataField,
			}},
			Ordinal: i,
		}
	}
	return docs
}

func mustCatalog(t testing.TB, docs []Document) Catalog {
	t.Helper()
	catalog, err := NewCatalog(docs, MaxDocuments)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
