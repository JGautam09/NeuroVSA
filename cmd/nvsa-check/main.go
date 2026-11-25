// Command nvsa-check recommends repository check IDs and compares selection policies.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/JGautam09/NeuroVSA/internal/checkselect"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: nvsa-check <recommend|compare|bench|verify|pack-sign|pack-merge|mcp> [flags]")
		return 2
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		fmt.Fprintln(out, "usage: nvsa-check <recommend|compare|bench|verify|pack-sign|pack-merge|mcp> [flags]")
		return 0
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "output format: text or json")
	parse := func() error {
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 || (*format != "text" && *format != "json") {
			return fmt.Errorf("unexpected arguments or invalid output format")
		}
		return nil
	}
	fail := func(err error) int {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, "nvsa-check:", err)
		return 2
	}
	write := func(value any, text func(io.Writer) error) error {
		if *format == "text" {
			return text(out)
		}
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	}

	switch args[0] {
	case "recommend":
		config := fs.String("config", "nvsa-check.json", "check policy file")
		fixture := fs.String("fixture", "", "saved change fixture")
		repo := fs.String("repo", ".", "Git repository")
		base := fs.String("base", "", "PR base commit or ref")
		head := fs.String("head", "", "PR head commit or ref")
		receipt := fs.Bool("receipt", false, "emit the local replay receipt and its snapshot as JSON")
		trust, bundle := packFlags(fs)
		if err := parse(); err != nil {
			return fail(err)
		}
		if (*fixture == "" && (*base == "" || *head == "")) || (*fixture != "" && (*base != "" || *head != "")) {
			return fail(fmt.Errorf("specify either -fixture or both -base and -head"))
		}
		snapshot, err := loadSnapshot(*config, *trust, *bundle)
		if err != nil {
			return fail(err)
		}
		var changes []checkselect.Change
		missingHistory := false
		if *fixture != "" {
			f, err := checkselect.ReadJSON[checkselect.Fixture](*fixture)
			if err != nil {
				return fail(err)
			}
			if f.Version != 1 || f.Changes == nil {
				return fail(fmt.Errorf("fixture requires version 1 and a changes array"))
			}
			changes = f.Changes
		} else {
			changes, err = checkselect.GitChanges(*repo, *base, *head)
			if err != nil {
				fmt.Fprintf(stderr, "nvsa-check: recommending full suite (%v)\n", err)
				missingHistory = true
			}
		}
		replay, err := snapshot.Evaluate(changes, missingHistory)
		if err != nil {
			return fail(err)
		}
		if *receipt {
			if err := checkselect.WriteArtifact(out, replay); err != nil {
				return fail(err)
			}
			return 0
		}
		result := replay.Receipt.Recommendation
		if err := write(result, func(w io.Writer) error { return printRecommendation(w, result) }); err != nil {
			return fail(err)
		}
	case "compare":
		before := fs.String("before", "", "previous check policy")
		after := fs.String("after", "", "candidate check policy")
		cases := fs.String("cases", "", "versioned case suite with expectations")
		gate := fs.Bool("gate", false, "exit 1 when the candidate violates an expectation")
		if err := parse(); err != nil {
			return fail(err)
		}
		if *before == "" || *after == "" || *cases == "" {
			return fail(fmt.Errorf("-before, -after and -cases are required"))
		}
		a, err := checkselect.LoadPolicy(*before)
		if err != nil {
			return fail(err)
		}
		b, err := checkselect.LoadPolicy(*after)
		if err != nil {
			return fail(err)
		}
		s, err := checkselect.LoadSuite(*cases)
		if err != nil {
			return fail(err)
		}
		results := checkselect.Compare(a, b, s)
		if err := write(results, func(w io.Writer) error { return printComparison(w, results) }); err != nil {
			return fail(err)
		}
		if *gate {
			for _, r := range results {
				if len(r.AfterIssues) > 0 {
					return 1
				}
			}
		}
	case "bench":
		corpus := fs.String("corpus", "", "versioned benchmark manifest")
		repeat := fs.Int("repeat", 100, "timing repetitions per case")
		selector := fs.String("selector", "", "limit to one of full, rules, similarity or hdc")
		if err := parse(); err != nil {
			return fail(err)
		}
		if *corpus == "" {
			return fail(fmt.Errorf("-corpus is required"))
		}
		b, err := checkselect.RunBenchmark(*corpus, *repeat, *selector)
		if err != nil {
			return fail(err)
		}
		if err := write(b, func(w io.Writer) error { return printBenchmark(w, b) }); err != nil {
			return fail(err)
		}
	case "verify":
		file := fs.String("file", "", "replay JSON from recommend -receipt")
		expected := fs.String("expected-snapshot", "", "optional independently recorded SHA-256 snapshot digest")
		if err := parse(); err != nil {
			return fail(err)
		}
		if *file == "" {
			return fail(fmt.Errorf("-file is required"))
		}
		replay, err := checkselect.ReadJSON[checkselect.Replay](*file)
		if err != nil {
			return fail(err)
		}
		if err := replay.Verify(*expected); err != nil {
			fmt.Fprintln(stderr, "nvsa-check: verification failed:", err)
			return 1
		}
		if err := write(map[string]string{"verified_snapshot": replay.Receipt.SnapshotSHA256}, func(w io.Writer) error {
			_, err := fmt.Fprintln(w, "Verified snapshot", replay.Receipt.SnapshotSHA256)
			return err
		}); err != nil {
			return fail(err)
		}
	case "pack-sign":
		payload := fs.String("payload", "", "versioned pack payload JSON")
		key := fs.String("key", "", "private key backup from nvsa-pack keygen")
		if err := parse(); err != nil {
			return fail(err)
		}
		signed, err := signPackFile(*payload, *key)
		if err != nil {
			return fail(err)
		}
		if err := checkselect.WriteArtifact(out, signed); err != nil {
			return fail(err)
		}
	case "pack-merge":
		trust := fs.String("trust", "", "trusted signers for the receiving repo")
		var inputs fileFlags
		fs.Var(&inputs, "input", "signed pack or merged bundle (repeatable)")
		if err := parse(); err != nil {
			return fail(err)
		}
		bundle, err := mergePackFiles(*trust, inputs)
		if err != nil {
			return fail(err)
		}
		if err := checkselect.WriteArtifact(out, bundle); err != nil {
			return fail(err)
		}
	case "mcp":
		config := fs.String("config", "nvsa-check.json", "check policy file")
		trust, bundle := packFlags(fs)
		if err := parse(); err != nil {
			return fail(err)
		}
		snapshot, err := loadSnapshot(*config, *trust, *bundle)
		if err != nil {
			return fail(err)
		}
		if err := checkselect.ServeMCP(os.Stdin, out, snapshot); err != nil {
			return fail(err)
		}
	default:
		return fail(fmt.Errorf("unknown command %q (use -h)", args[0]))
	}
	return 0
}

func list(ids []string) string {
	if len(ids) == 0 {
		return "(none)"
	}
	return strings.Join(ids, ", ")
}

func printRecommendation(w io.Writer, r checkselect.Result) error {
	var text strings.Builder
	fmt.Fprintf(&text, "Required: %s\nOptional: %s\n", list(r.Required), list(r.Optional))
	if r.Fallback != "" {
		fmt.Fprintf(&text, "Full suite: %s\n", r.Fallback)
	}
	for _, path := range r.Unmatched {
		fmt.Fprintf(&text, "  Unmatched %q\n", path)
	}
	for _, m := range r.Matches {
		fmt.Fprintf(&text, "Rule %s matched %q\n", m.Rule, m.Paths)
	}
	for _, m := range r.PackMatches {
		fmt.Fprintf(&text, "Pack %s@%d (%s) matched %s: %s\n", m.Name, m.Revision, m.Signer, m.Example, list(m.Checks))
	}
	if r.PackConflict {
		fmt.Fprintln(&text, "Conflicting pack suggestions withheld")
	}
	_, err := io.WriteString(w, text.String())
	return err
}

func printComparison(w io.Writer, rows []checkselect.Comparison) error {
	var text strings.Builder
	failures := 0
	for _, r := range rows {
		fmt.Fprintf(&text, "%s: added %s; removed %s\n", r.CaseID, list(r.Added), list(r.Removed))
		if len(r.BeforeIssues) != 0 {
			fmt.Fprintf(&text, "  before: %s\n", list(r.BeforeIssues))
		}
		if len(r.AfterIssues) != 0 {
			failures++
			fmt.Fprintf(&text, "  after: %s\n", list(r.AfterIssues))
		}
	}
	fmt.Fprintf(&text, "%d cases, %d candidate failures\n", len(rows), failures)
	_, err := io.WriteString(w, text.String())
	return err
}

func printBenchmark(w io.Writer, reports []checkselect.Benchmark) error {
	var text strings.Builder
	for _, r := range reports {
		fmt.Fprintf(&text, "%s (p50 %d ns, p95 %d ns)\n", r.Selector, r.P50NS, r.P95NS)
		for _, g := range r.Groups {
			fmt.Fprintf(&text, "  %s %s %s: %d cases, %d expectation failures, required misses %d → %d, %d fallbacks, %d checks selected, optional correct %d/%d\n",
				g.Project, g.Split, g.Source, g.Cases, g.CaseFailures, g.DirectRequiredMisses, g.FinalRequiredMisses,
				g.Fallbacks, g.ChecksSelected, g.OptionalCorrect, g.OptionalSuggestions)
		}
		if r.Promotion != nil {
			p := r.Promotion
			fmt.Fprintf(&text, "  promotion passed=%t: %d eligible holdouts, %.1f pp vs %s, %.1f%% precision, %d required misses\n",
				p.Passed, p.EligibleCases, p.CoverageGainPP, p.Baseline, 100*p.Precision, p.FinalRequiredMisses)
		}
	}
	_, err := io.WriteString(w, text.String())
	return err
}
