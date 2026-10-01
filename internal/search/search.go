// Package search provides deterministic, in-memory matching over explicit,
// display-safe fields. It deliberately has no source or I/O integration.
package search

import (
	"container/heap"
	"errors"
	"sort"
	"strings"
	"unicode"
)

const (
	MaxDocuments = 20_000
	MaxResults   = 200
)

type ID string
type Kind string
type FieldClass uint8

const (
	PrimaryField FieldClass = iota
	SecondaryField
	MetadataField
)

type Field struct {
	Name  string
	Value string
	Class FieldClass
}

type Document struct {
	ID        ID
	Kind      Kind
	Primary   string
	Secondary string
	Fields    []Field
	Ordinal   int
	Boost     int
}

type Match struct {
	ID           ID
	MatchedField string
	MatchedValue string
}

type Results struct {
	Matches   []Match
	Total     int
	Truncated bool
}

type compiledField struct {
	name       string
	value      string
	normalized string
	class      FieldClass
}

type compiledDocument struct {
	id      ID
	fields  []compiledField
	ordinal int
	boost   int
	index   int
}

type Catalog struct {
	documents []compiledDocument
	truncated bool
}

var (
	ErrInvalidLimit = errors.New("search limit must be positive")
	ErrEmptyID      = errors.New("search document ID must not be empty")
	ErrDuplicateID  = errors.New("search document IDs must be unique")
)

func NewCatalog(documents []Document, maxDocuments int) (Catalog, error) {
	if maxDocuments <= 0 {
		return Catalog{}, ErrInvalidLimit
	}

	count := min(len(documents), maxDocuments)
	seen := make(map[ID]struct{}, count)
	for index, document := range documents {
		if document.ID == "" {
			return Catalog{}, ErrEmptyID
		}
		if _, ok := seen[document.ID]; ok {
			return Catalog{}, ErrDuplicateID
		}
		if index < count {
			seen[document.ID] = struct{}{}
		}
	}

	compiled := make([]compiledDocument, 0, count)
	for index, document := range documents[:count] {
		fields := make([]compiledField, 0, len(document.Fields)+2)
		fields = appendCompiledField(fields, "name", document.Primary, PrimaryField)
		fields = appendCompiledField(fields, "description", document.Secondary, SecondaryField)
		for _, field := range document.Fields {
			fields = appendCompiledField(fields, field.Name, field.Value, field.Class)
		}
		compiled = append(compiled, compiledDocument{
			id:      document.ID,
			fields:  fields,
			ordinal: document.Ordinal,
			boost:   clamp(document.Boost, -9, 9),
			index:   index,
		})
	}

	return Catalog{documents: compiled, truncated: len(documents) > count}, nil
}

func appendCompiledField(fields []compiledField, name, value string, class FieldClass) []compiledField {
	value = strings.TrimSpace(value)
	if value == "" {
		return fields
	}
	return append(fields, compiledField{
		name:       name,
		value:      value,
		normalized: normalize(value),
		class:      class,
	})
}

type rankedMatch struct {
	match   Match
	score   int
	ordinal int
	index   int
}

type rankedHeap []rankedMatch

func (h rankedHeap) Len() int { return len(h) }
func (h rankedHeap) Less(i, j int) bool {
	return rankedWorse(h[i], h[j])
}
func (h rankedHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *rankedHeap) Push(value any) {
	*h = append(*h, value.(rankedMatch))
}
func (h *rankedHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}

func (catalog Catalog) Match(query string, maxResults int) Results {
	if maxResults <= 0 {
		return Results{Truncated: catalog.truncated}
	}

	normalizedQuery := normalize(query)
	tokens := strings.Fields(normalizedQuery)
	ranked := make(rankedHeap, 0, min(len(catalog.documents), maxResults))
	total := 0
	for _, document := range catalog.documents {
		candidate, ok := rankDocument(document, normalizedQuery, tokens)
		if !ok {
			continue
		}
		total++
		if len(ranked) < maxResults {
			heap.Push(&ranked, candidate)
		} else if rankedBetter(candidate, ranked[0]) {
			ranked[0] = candidate
			heap.Fix(&ranked, 0)
		}
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		return rankedBetter(ranked[i], ranked[j])
	})

	matches := make([]Match, len(ranked))
	for i := range ranked {
		matches[i] = ranked[i].match
	}
	return Results{
		Matches:   matches,
		Total:     total,
		Truncated: catalog.truncated || total > len(matches),
	}
}

func rankedBetter(left, right rankedMatch) bool {
	if left.score != right.score {
		return left.score < right.score
	}
	if left.ordinal != right.ordinal {
		return left.ordinal < right.ordinal
	}
	return left.index < right.index
}

func rankedWorse(left, right rankedMatch) bool {
	return rankedBetter(right, left)
}

func rankDocument(document compiledDocument, query string, tokens []string) (rankedMatch, bool) {
	if len(tokens) == 0 {
		return rankedMatch{
			match:   Match{ID: document.id},
			score:   -document.boost,
			ordinal: document.ordinal,
			index:   document.index,
		}, true
	}

	bestField := -1
	bestCategory := 100
	explanationField := -1
	score := 0
	for _, token := range tokens {
		tokenCategory := 100
		tokenField := -1
		for fieldIndex, field := range document.fields {
			category, ok := fieldCategory(field, token)
			if ok && category < tokenCategory {
				tokenCategory = category
				tokenField = fieldIndex
			}
		}
		if tokenField < 0 {
			return rankedMatch{}, false
		}
		score += tokenCategory * 100
		if document.fields[tokenField].class != PrimaryField && explanationField < 0 {
			explanationField = tokenField
		}
		if tokenCategory < bestCategory {
			bestCategory = tokenCategory
			bestField = tokenField
		}
	}

	for index, field := range document.fields {
		if field.class == PrimaryField && field.normalized == query {
			score = -1_000
			bestField = index
			break
		}
	}
	score -= document.boost

	match := Match{ID: document.id}
	if explanationField < 0 && bestField >= 0 && document.fields[bestField].class != PrimaryField {
		explanationField = bestField
	}
	if explanationField >= 0 {
		match.MatchedField = document.fields[explanationField].name
		match.MatchedValue = document.fields[explanationField].value
	}
	return rankedMatch{match: match, score: score, ordinal: document.ordinal, index: document.index}, true
}

func fieldCategory(field compiledField, token string) (int, bool) {
	if !strings.Contains(field.normalized, token) {
		return 0, false
	}
	switch field.class {
	case PrimaryField:
		if strings.HasPrefix(field.normalized, token) || wordPrefix(field.normalized, token) {
			return 1, true
		}
		return 2, true
	case SecondaryField:
		if strings.HasPrefix(field.normalized, token) {
			return 3, true
		}
		return 4, true
	default:
		return 5, true
	}
}

func wordPrefix(value, token string) bool {
	for _, word := range strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if strings.HasPrefix(word, token) {
			return true
		}
	}
	return false
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
