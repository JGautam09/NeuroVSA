package checkselect

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type Project struct {
	ID     string `json:"id"`
	Policy string `json:"policy"`
	Suite  string `json:"suite"`
}

type Corpus struct {
	Version  int       `json:"version"`
	Projects []Project `json:"projects"`
}

type Stats struct {
	Cases                    int `json:"cases"`
	CaseFailures             int `json:"case_failures"`
	DirectRequiredMisses     int `json:"direct_required_misses"`
	FinalRequiredMisses      int `json:"final_required_misses"`
	Fallbacks                int `json:"fallbacks"`
	FallbackMismatches       int `json:"fallback_mismatches"`
	ForbiddenRecommendations int `json:"forbidden_recommendations"`
	CasesWithForbiddenChecks int `json:"cases_with_forbidden_checks"`
	ChecksSelected           int `json:"checks_selected"`
	OptionalEligibleCases    int `json:"optional_eligible_cases"`
	OptionalCorrectCases     int `json:"optional_correct_cases"`
	OptionalSuggestions      int `json:"optional_suggestions"`
	OptionalCorrect          int `json:"optional_correct"`
	OptionalAbstentions      int `json:"optional_abstentions_on_eligible_cases"`
}

func (s *Stats) add(r Result, expected Expectations) {
	s.Cases++
	if len(issues(r, expected)) != 0 {
		s.CaseFailures++
	}
	s.DirectRequiredMisses += len(Difference(expected.Required, r.DirectRequired))
	s.FinalRequiredMisses += len(Difference(expected.Required, r.Required))
	if r.Fallback != "" {
		s.Fallbacks++
	}
	if (r.Fallback != "") != expected.Fallback {
		s.FallbackMismatches++
	}
	if len(expected.Forbidden) != 0 {
		s.CasesWithForbiddenChecks++
	}
	ids := selected(r)
	for _, id := range expected.Forbidden {
		if slices.Contains(ids, id) {
			s.ForbiddenRecommendations++
		}
	}
	s.ChecksSelected += len(ids)
	if len(expected.Optional) != 0 {
		s.OptionalEligibleCases++
		if len(r.Optional) == 0 {
			s.OptionalAbstentions++
		}
	}
	correct := 0
	for _, id := range r.Optional {
		if slices.Contains(expected.Optional, id) {
			correct++
		}
	}
	if correct > 0 {
		s.OptionalCorrectCases++
	}
	s.OptionalSuggestions += len(r.Optional)
	s.OptionalCorrect += correct
}

type Group struct {
	Project string `json:"project"`
	Split   string `json:"split"`
	Source  string `json:"source"` // historical or synthetic
	Stats
}

type Benchmark struct {
	Selector        string              `json:"selector"`
	FirstDecisionNS int64               `json:"first_decision_ns"`
	P50NS           int64               `json:"p50_ns"`
	P95NS           int64               `json:"p95_ns"`
	Groups          []Group             `json:"groups"`
	Decisions       []BenchmarkDecision `json:"decisions"`
	Promotion       *Promotion          `json:"promotion,omitempty"`
}

type Promotion struct {
	EligibleCases       int     `json:"eligible_cases"`
	Baseline            string  `json:"baseline"`
	CoverageGainPP      float64 `json:"coverage_gain_percentage_points"`
	Precision           float64 `json:"precision"`
	FinalRequiredMisses int     `json:"final_required_misses"`
	Passed              bool    `json:"passed"`
}

type BenchmarkDecision struct {
	Project  string       `json:"project"`
	CaseID   string       `json:"case_id"`
	Expected Expectations `json:"expected"`
	Result   Result       `json:"result"`
	Issues   []string     `json:"issues"`
}

type benchmarkProject struct {
	ID     string
	Policy Policy
	Suite  Suite
}

// RunBenchmark reports dev as well as holdout results. Only the holdout measures
// similarity on cases excluded from its examples.
func RunBenchmark(manifest string, repeat int, filter string) ([]Benchmark, error) {
	corpus, err := ReadJSON[Corpus](manifest)
	if err != nil {
		return nil, err
	}
	if corpus.Version != 1 || len(corpus.Projects) == 0 || repeat < 1 || repeat > 1000 {
		return nil, fmt.Errorf("benchmark requires version 1, projects, and repeat from 1 to 1000")
	}
	names := []string{"full", "rules", "similarity", "hdc"}
	if filter != "" && !slices.Contains(names, filter) {
		return nil, fmt.Errorf("unknown selector %q", filter)
	}
	var projects []benchmarkProject
	seen := make(map[string]bool)
	for _, project := range corpus.Projects {
		if !identifier.MatchString(project.ID) || seen[project.ID] || !validPath(project.Policy) || !validPath(project.Suite) {
			return nil, fmt.Errorf("invalid project %q", project.ID)
		}
		seen[project.ID] = true
		p, err := LoadPolicy(filepath.Join(filepath.Dir(manifest), project.Policy))
		if err != nil {
			return nil, err
		}
		s, err := LoadSuite(filepath.Join(filepath.Dir(manifest), project.Suite))
		if err != nil {
			return nil, err
		}
		catalog := make(map[string]bool, len(p.Checks))
		for _, id := range p.Checks {
			catalog[id] = true
		}
		for _, c := range s.Cases {
			if err := knownChecks(catalog, slices.Concat(c.Expected.Required, c.Expected.Optional, c.Expected.Forbidden)); err != nil {
				return nil, fmt.Errorf("project %s, case %s: %w", project.ID, c.ID, err)
			}
		}
		projects = append(projects, benchmarkProject{project.ID, p, s})
	}
	var reports []Benchmark
	for _, name := range names {
		if filter == "" || filter == name {
			reports = append(reports, measure(projects, name, repeat))
		}
	}
	hdc := slices.IndexFunc(reports, func(b Benchmark) bool { return b.Selector == "hdc" })
	if hdc >= 0 {
		baselines := reports
		if filter == "hdc" {
			baselines = []Benchmark{measure(projects, "rules", 1), measure(projects, "similarity", 1)}
		}
		gate := promotion(reports[hdc], baselines)
		reports[hdc].Promotion = &gate
	}
	return reports, nil
}

func promotion(hdc Benchmark, reports []Benchmark) Promotion {
	coverage := func(b Benchmark) int {
		count := 0
		for _, g := range b.Groups {
			if g.Split == "holdout" {
				count += g.OptionalCorrectCases
			}
		}
		return count
	}
	gate := Promotion{Baseline: "rules"}
	correct, suggestions := 0, 0
	for _, g := range hdc.Groups {
		gate.FinalRequiredMisses += g.FinalRequiredMisses
		if g.Split == "holdout" {
			gate.EligibleCases += g.OptionalEligibleCases
			correct += g.OptionalCorrect
			suggestions += g.OptionalSuggestions
		}
	}
	strongest := -1
	for _, b := range reports {
		if b.Selector == "rules" || b.Selector == "similarity" {
			if n := coverage(b); n > strongest {
				strongest, gate.Baseline = n, b.Selector
			}
		}
	}
	gain := coverage(hdc) - strongest
	if gate.EligibleCases > 0 {
		gate.CoverageGainPP = 100 * float64(gain) / float64(gate.EligibleCases)
	}
	if suggestions > 0 {
		gate.Precision = float64(correct) / float64(suggestions)
	}
	gate.Passed = gate.EligibleCases >= 40 && 10*gain >= gate.EligibleCases &&
		suggestions > 0 && 100*correct >= 95*suggestions && gate.FinalRequiredMisses == 0
	return gate
}

func measure(projects []benchmarkProject, name string, repeat int) Benchmark {
	b := Benchmark{Selector: name}
	var timings []int64
	for _, p := range projects {
		predict := p.Policy.Recommend
		switch name {
		case "full":
			all := sortedUnique(p.Policy.Checks)
			predict = func([]Change) Result { return Result{DirectRequired: all, Required: all} }
		case "similarity":
			predict = similarity(p.Policy, p.Suite.Cases)
		case "hdc":
			predict = hdcSelector(p.Policy, p.Suite.Cases)
		}
		for _, c := range p.Suite.Cases {
			source := "synthetic"
			if c.Head != "" {
				source = "historical"
			}
			idx := slices.IndexFunc(b.Groups, func(g Group) bool {
				return g.Project == p.ID && g.Source == source && g.Split == c.Split
			})
			if idx == -1 {
				idx = len(b.Groups)
				b.Groups = append(b.Groups, Group{Project: p.ID, Source: source, Split: c.Split})
			}
			for i := 0; i < repeat; i++ {
				start := time.Now()
				result := predict(c.Changes)
				ns := time.Since(start).Nanoseconds()
				if len(timings) == 0 {
					b.FirstDecisionNS = ns
				}
				timings = append(timings, ns)
				if i == 0 {
					b.Groups[idx].Stats.add(result, c.Expected)
					b.Decisions = append(b.Decisions, BenchmarkDecision{p.ID, c.ID, c.Expected, result, issues(result, c.Expected)})
				}
			}
		}
	}
	slices.Sort(timings)
	b.P50NS = timings[(50*len(timings)+99)/100-1]
	b.P95NS = timings[(95*len(timings)+99)/100-1]
	return b
}

type example struct {
	features     []string
	requirements Expectations
}

// similarity is a nearest-example baseline. Paths describe directories, suffixes
// and change types. It abstains below 0.5 Jaccard or on disagreeing best examples.
func similarity(p Policy, cases []Case) func([]Change) Result {
	var examples []example
	for _, c := range cases {
		if c.Split == "development" && !c.Expected.Fallback {
			e := c.Expected
			e.Required, e.Optional = sortedUnique(e.Required), sortedUnique(e.Optional)
			examples = append(examples, example{features(c.Changes), e})
		}
	}
	return func(changes []Change) Result {
		query := features(changes)
		bestScore, tied := 0.0, false
		var best Expectations
		for _, e := range examples {
			score := jaccard(query, e.features)
			switch {
			case score > bestScore:
				bestScore, best, tied = score, e.requirements, false
			case score == bestScore && (!slices.Equal(best.Required, e.requirements.Required) || !slices.Equal(best.Optional, e.requirements.Optional)):
				tied = true
			}
		}
		r := Result{DirectRequired: sortedUnique(p.Always)}
		if bestScore < 0.5 || tied {
			return p.fullSuite(r, "uncertain-similarity")
		}
		r.DirectRequired = sortedUnique(slices.Concat(r.DirectRequired, best.Required))
		r.Required = r.DirectRequired
		r.Optional = Difference(best.Optional, r.Required)
		return r
	}
}

func features(changes []Change) []string {
	var tokens []string
	for _, filename := range changePaths(changes) {
		tokens = append(tokens, "dir="+path.Dir(filename), "ext="+strings.ToLower(path.Ext(filename)))
	}
	for _, change := range changes {
		tokens = append(tokens, "status="+change.Status)
	}
	return sortedUnique(tokens)
}

func jaccard(a, b []string) float64 {
	common := len(a) - len(Difference(a, b))
	union := len(a) + len(b) - common
	if union == 0 {
		return 1
	}
	return float64(common) / float64(union)
}
