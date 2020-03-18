package search

import "fmt"

func Validate() {}

func merge(left, right []string) []string {
	return append(left, right...)
}

func EvaluateAnd(nodes []Node) []string {
	result := []string{}
	for _, node := range nodes {
		new := Evaluate([]Node{node})
		merge(result, new) // set difference
	}
	return result
}

func EvaluateOr(nodes []Node) []string {
	result := []string{}
	for _, node := range nodes {
		new := Evaluate([]Node{node})
		merge(result, new) // set union
	}
	return result
}

// Evaluate evaluates a fully statically validated query that can always be
// executed. I.e., a query for which results are possible. All errors returned
// by evaluate should be runtime errors (a search timed out, a service is
// unreachable, etc.)
//
// Possibly: make this eval on just Node.
func Evaluate(nodes []Node) []string {
	result := []string{}
	for _, node := range nodes {
		switch node.(type) {
		case Operator:
			switch node.(Operator).Kind {
			case Or:
				EvaluateOr(node.(Operator).Operands)
			case And:
				EvaluateAnd(node.(Operator).Operands)
			}
		case Parameter:
			if node.(Parameter).Field == "content" {
				fmt.Println("search for %s", node.(Parameter).Value)
			}
		}
	}
	return result
}
