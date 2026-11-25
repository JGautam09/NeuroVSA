// Package checkselect evaluates repository check policies against structured changes.
package checkselect

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/JGautam09/NeuroVSA/engine"
)

type Policy struct {
	Version int      `json:"version"`
	Scope   string   `json:"scope,omitempty"`
	Checks  []string `json:"checks"`
	Always  []string `json:"always"`
	Rules   []Rule   `json:"rules"`
}

type Rule struct {
	ID       string   `json:"id"`
	Paths    []string `json:"paths"`
	Required []string `json:"required,omitempty"`
	Optional []string `json:"optional,omitempty"`
}

type Change struct {
	Status  string `json:"status"`
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"`
}

type Fixture struct {
	Version int      `json:"version"`
	Changes []Change `json:"changes"`
}

type Match struct {
	Rule  string   `json:"rule"`
	Paths []string `json:"paths"`
}

type Result struct {
	DirectRequired []string                    `json:"direct_required"`
	Required       []string                    `json:"required"`
	Optional       []string                    `json:"optional"`
	Matches        []Match                     `json:"matches"`
	Fallback       string                      `json:"fallback,omitempty"`
	Unmatched      []string                    `json:"unmatched,omitempty"`
	PackMatches    []PackMatch                 `json:"pack_matches,omitempty"`
	PackConflict   bool                        `json:"pack_conflict,omitempty"`
	HDCProof       *engine.DecisionCertificate `json:"experimental_hdc_proof,omitempty"`
}

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,79}$`)

const maxArtifactBytes = 8 << 20

func ReadJSON[T any](filename string) (T, error) {
	var value T
	f, err := os.Open(filename)
	if err != nil {
		return value, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxArtifactBytes+1))
	if err != nil {
		return value, err
	}
	if len(data) > maxArtifactBytes {
		return value, fmt.Errorf("%s: JSON exceeds 8 MiB", filename)
	}
	if err := decodeJSON(data, &value); err != nil {
		return value, fmt.Errorf("%s: %w", filename, err)
	}
	return value, nil
}

// WriteArtifact uses the same size limit as ReadJSON so an emitted receipt or
// pack can be read back. Check before writing to avoid a partial JSON document.
func WriteArtifact(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) >= maxArtifactBytes {
		return fmt.Errorf("JSON artifact exceeds 8 MiB")
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

func decodeJSON(data []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

func LoadPolicy(filename string) (Policy, error) {
	p, err := ReadJSON[Policy](filename)
	if err != nil {
		return p, err
	}
	if err := p.validate(); err != nil {
		return p, fmt.Errorf("%s: %w", filename, err)
	}
	return p, nil
}

func (p Policy) validate() error {
	if p.Version != 1 || len(p.Checks) == 0 {
		return fmt.Errorf("policy requires version 1 and a nonempty checks catalog")
	}
	if p.Scope != "" && !scopeName.MatchString(p.Scope) {
		return fmt.Errorf("invalid policy scope %q", p.Scope)
	}
	checks := make(map[string]bool, len(p.Checks))
	for _, id := range p.Checks {
		if !identifier.MatchString(id) || checks[id] {
			return fmt.Errorf("invalid or duplicate check ID %q", id)
		}
		checks[id] = true
	}
	if err := knownChecks(checks, p.Always); err != nil {
		return fmt.Errorf("always: %w", err)
	}
	rules := make(map[string]bool, len(p.Rules))
	for _, r := range p.Rules {
		if !identifier.MatchString(r.ID) || rules[r.ID] || len(r.Paths) == 0 {
			return fmt.Errorf("rule %q has an invalid ID, duplicate ID, or no paths", r.ID)
		}
		rules[r.ID] = true
		for _, pattern := range r.Paths {
			glob := strings.TrimSuffix(pattern, "/**")
			_, err := path.Match(glob, "")
			if err != nil || !validPath(glob) || strings.Contains(glob, "**") ||
				(strings.HasSuffix(pattern, "/**") && strings.ContainsAny(glob, `*?[]\`)) {
				return fmt.Errorf("rule %q has invalid pattern %q", r.ID, pattern)
			}
		}
		for _, ids := range [][]string{r.Required, r.Optional} {
			if err := knownChecks(checks, ids); err != nil {
				return fmt.Errorf("rule %q: %w", r.ID, err)
			}
		}
	}
	return nil
}

func knownChecks(catalog map[string]bool, ids []string) error {
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !catalog[id] || seen[id] {
			return fmt.Errorf("unknown or duplicate check ID %q", id)
		}
		seen[id] = true
	}
	return nil
}

func validPath(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.HasPrefix(value, "../") &&
		utf8.ValidString(value) && !path.IsAbs(value) && !strings.ContainsAny(value, "\\\x00") && path.Clean(value) == value
}

func Normalize(changes []Change) ([]Change, error) {
	out := slices.Clone(changes)
	for _, change := range out {
		paired := change.Status == "R" || change.Status == "C"
		if !slices.Contains([]string{"A", "C", "D", "M", "R", "T"}, change.Status) || !validPath(change.Path) ||
			(paired && (!validPath(change.OldPath) || change.OldPath == change.Path)) || (!paired && change.OldPath != "") {
			return nil, fmt.Errorf("invalid change %q %q %q", change.Status, change.OldPath, change.Path)
		}
	}
	slices.SortFunc(out, func(a, b Change) int {
		return cmp.Or(strings.Compare(a.Path, b.Path), strings.Compare(a.OldPath, b.OldPath), strings.Compare(a.Status, b.Status))
	})
	return slices.Compact(out), nil
}

func changePaths(changes []Change) []string {
	var paths []string
	for _, change := range changes {
		paths = append(paths, change.Path)
		if change.OldPath != "" {
			paths = append(paths, change.OldPath)
		}
	}
	return sortedUnique(paths)
}

func matches(pattern, filename string) bool {
	if prefix, recursive := strings.CutSuffix(pattern, "/**"); recursive {
		return strings.HasPrefix(filename, prefix+"/")
	}
	ok, _ := path.Match(pattern, filename)
	return ok
}

// Recommend treats matching rules as additive. A matched rule may explicitly cover a
// path without adding checks. An uncovered path makes the selection incomplete.
func (p Policy) Recommend(changes []Change) Result {
	r := Result{DirectRequired: sortedUnique(p.Always), Matches: []Match{}}
	covered := make(map[string]bool)
	paths := changePaths(changes)
	for _, rule := range p.Rules {
		m := Match{Rule: rule.ID}
		for _, filename := range paths {
			if slices.ContainsFunc(rule.Paths, func(pattern string) bool { return matches(pattern, filename) }) {
				covered[filename] = true
				m.Paths = append(m.Paths, filename)
			}
		}
		if len(m.Paths) != 0 {
			r.Matches = append(r.Matches, m)
			r.DirectRequired = append(r.DirectRequired, rule.Required...)
			r.Optional = append(r.Optional, rule.Optional...)
		}
	}
	slices.SortFunc(r.Matches, func(a, b Match) int { return strings.Compare(a.Rule, b.Rule) })
	r.DirectRequired = sortedUnique(r.DirectRequired)
	for _, filename := range paths {
		if !covered[filename] {
			r.Unmatched = append(r.Unmatched, filename)
		}
	}
	if len(r.Unmatched) > 0 {
		return p.fullSuite(r, "unmatched-paths")
	}
	r.Required = r.DirectRequired
	r.Optional = Difference(sortedUnique(r.Optional), r.Required)
	return r
}

func (p Policy) MissingHistory() Result {
	return p.fullSuite(Result{DirectRequired: sortedUnique(p.Always), Matches: []Match{}}, "git-history-unavailable")
}

func (p Policy) fullSuite(result Result, reason string) Result {
	result.Required = sortedUnique(p.Checks)
	result.Optional = []string{}
	result.Fallback = reason
	return result
}

func Difference(a, b []string) []string {
	out := []string{}
	for _, item := range a {
		if !slices.Contains(b, item) {
			out = append(out, item)
		}
	}
	return out
}

func sortedUnique(ids []string) []string {
	out := append([]string{}, ids...)
	slices.Sort(out)
	return slices.Compact(out)
}
