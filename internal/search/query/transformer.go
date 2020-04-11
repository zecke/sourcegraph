package query

import (
	"strings"
)

// LowercaseFieldNames performs strings.ToLower on every field name.
func LowercaseFieldNames(nodes []Node) []Node {
	return MapParameter(nodes, func(field, value string, negated bool) Node {
		return Parameter{Field: strings.ToLower(field), Value: value, Negated: negated}
	})
}

// HoistParameters is a heuristic that rewrites simple but ambiguous queries,
// changing the default interpretation of and/or expressions in a way that some
// consider to be more natural. For example, the following query without
// parentheses is interpreted as follows in the grammar:
//
// repo:foo a or b and c => (repo:foo a) or ((b) and (c))
//
// This function rewrites the above expression as follows:
//
// repo:foo a or b and c => repo:foo (a or b and c)
//
// Any number of field:value parameters may occur before and after the pattern
// expression, and these are hoisted out. The pattern expression must be
// contiguous. If not, we want to preserve the default interpretation, which
// corresponds more naturally to groupings with field parameters, i.e.,
//
// repo:foo a or b or repo:bar c => (repo:foo a) or (b) or (repo:bar c)
func HoistParameters(nodes []Node) []Node {
	return nodes
}
