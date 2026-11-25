package checkselect

import (
	"fmt"
	"slices"
)

type Expectations struct {
	Required  []string `json:"required"`
	Optional  []string `json:"optional,omitempty"`
	Forbidden []string `json:"forbidden,omitempty"`
	Fallback  bool     `json:"fallback,omitempty"`
}

type Case struct {
	ID       string       `json:"id"`
	Split    string       `json:"split"`
	Base     string       `json:"base,omitempty"`
	Head     string       `json:"head,omitempty"`
	Changes  []Change     `json:"changes"`
	Expected Expectations `json:"expected"`
	Reason   string       `json:"reason"`
}

type Suite struct {
	Version int    `json:"version"`
	Cases   []Case `json:"cases"`
}

func LoadSuite(filename string) (Suite, error) {
	s, err := ReadJSON[Suite](filename)
	if err != nil {
		return s, err
	}
	if s.Version != 1 || len(s.Cases) == 0 {
		return s, fmt.Errorf("%s: suite requires version 1 and at least one case", filename)
	}
	seen := make(map[string]bool)
	for i := range s.Cases {
		c := &s.Cases[i]
		if !identifier.MatchString(c.ID) || seen[c.ID] || c.Reason == "" || c.Changes == nil ||
			(c.Split != "development" && c.Split != "holdout") || (c.Base == "") != (c.Head == "") {
			return s, fmt.Errorf("invalid or duplicate case %q", c.ID)
		}
		seen[c.ID] = true
		c.Changes, err = Normalize(c.Changes)
		if err != nil {
			return s, fmt.Errorf("case %q: %w", c.ID, err)
		}
		ids := slices.Concat(c.Expected.Required, c.Expected.Optional, c.Expected.Forbidden)
		for _, id := range ids {
			if !identifier.MatchString(id) {
				return s, fmt.Errorf("case %q: invalid check ID %q", c.ID, id)
			}
		}
		if len(sortedUnique(ids)) != len(ids) {
			return s, fmt.Errorf("case %q: check expectations overlap or repeat", c.ID)
		}
	}
	return s, nil
}

type Comparison struct {
	CaseID       string   `json:"case_id"`
	Before       Result   `json:"before"`
	After        Result   `json:"after"`
	Added        []string `json:"added"`
	Removed      []string `json:"removed"`
	BeforeIssues []string `json:"before_issues"`
	AfterIssues  []string `json:"after_issues"`
}

func Compare(before, after Policy, suite Suite) []Comparison {
	rows := make([]Comparison, 0, len(suite.Cases))
	for _, c := range suite.Cases {
		a, b := before.Recommend(c.Changes), after.Recommend(c.Changes)
		oldIDs, newIDs := selected(a), selected(b)
		rows = append(rows, Comparison{
			CaseID: c.ID, Before: a, After: b,
			Added: Difference(newIDs, oldIDs), Removed: Difference(oldIDs, newIDs),
			BeforeIssues: issues(a, c.Expected), AfterIssues: issues(b, c.Expected),
		})
	}
	return rows
}

func selected(result Result) []string {
	return sortedUnique(slices.Concat(result.Required, result.Optional))
}

func issues(result Result, expected Expectations) []string {
	problems := []string{}
	for _, id := range Difference(expected.Required, result.Required) {
		problems = append(problems, "missing required: "+id)
	}
	for _, id := range Difference(expected.Optional, selected(result)) {
		problems = append(problems, "missing optional: "+id)
	}
	for _, id := range expected.Forbidden {
		if slices.Contains(selected(result), id) {
			problems = append(problems, "forbidden: "+id)
		}
	}
	if expected.Fallback != (result.Fallback != "") {
		problems = append(problems, fmt.Sprintf("expected fallback: %t", expected.Fallback))
	}
	return problems
}
