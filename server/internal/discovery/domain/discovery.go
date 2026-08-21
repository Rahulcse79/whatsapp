// Package domain holds the discovery pure logic (T13.01): query validation and
// cross-type relevance scoring for the metadata search (channels, public
// communities, usernames). METADATA ONLY — this never touches E2EE message
// content. No I/O; the search backend (Postgres trigram by default, OpenSearch
// as a drop-in) supplies candidate rows, and this ranks them uniformly.
package domain

import (
	"errors"
	"strings"
)

const (
	MinQuery = 2
	MaxQuery = 100
)

var ErrBadQuery = errors.New("discovery: query must be 2–100 characters")

// Kind is the type of a searchable public entity.
type Kind string

const (
	KindChannel   Kind = "channel"
	KindCommunity Kind = "community"
	KindUser      Kind = "user"
)

func KindValid(k Kind) bool {
	return k == KindChannel || k == KindCommunity || k == KindUser
}

// NormalizeQuery lower-cases + trims a query.
func NormalizeQuery(q string) string { return strings.ToLower(strings.TrimSpace(q)) }

// ValidateQuery bounds a query length.
func ValidateQuery(q string) error {
	n := len(strings.TrimSpace(q))
	if n < MinQuery || n > MaxQuery {
		return ErrBadQuery
	}
	return nil
}

// MatchScore rates how well a candidate matches the query, uniformly across the
// entity types so their results can be merged into one ranked list:
//   - exact match on the primary field ....... 1.00
//   - primary starts with the query .......... 0.80
//   - a word in primary starts with the query  0.60
//   - primary contains the query ............. 0.40
//   - secondary (handle/description) contains  0.20
//   - verified entities get a small boost.
//
// Returns 0 for no match. query is assumed already normalised.
func MatchScore(query, primary, secondary string, verified bool) float64 {
	p := strings.ToLower(strings.TrimSpace(primary))
	s := strings.ToLower(secondary)
	var score float64
	switch {
	case p == query:
		score = 1.0
	case strings.HasPrefix(p, query):
		score = 0.8
	case wordPrefix(p, query):
		score = 0.6
	case strings.Contains(p, query):
		score = 0.4
	case strings.Contains(s, query):
		score = 0.2
	default:
		score = 0.0
	}
	if score > 0 && verified {
		score += 0.05
	}
	return score
}

// wordPrefix reports whether any word in text starts with q.
func wordPrefix(text, q string) bool {
	for _, w := range strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == '-' || r == '_' || r == '.' }) {
		if strings.HasPrefix(w, q) {
			return true
		}
	}
	return false
}
