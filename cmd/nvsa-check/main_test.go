package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JGautam09/NeuroVSA/internal/checkselect"
)

func TestBundledPoliciesPreserveRequiredChecks(t *testing.T) {
	runs, err := checkselect.RunBenchmark("../../arena/check-selection/corpus.json", 1, "rules")
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range runs[0].Groups {
		if group.FinalRequiredMisses != 0 || group.FallbackMismatches != 0 || group.ForbiddenRecommendations != 0 {
			t.Errorf("%s %s %s: %d required misses, %d fallback mismatches, %d forbidden checks",
				group.Project, group.Split, group.Source, group.FinalRequiredMisses, group.FallbackMismatches, group.ForbiddenRecommendations)
		}
	}
}

func TestQuickstartComparison(t *testing.T) {
	good := "../../arena/check-selection/chi-policy.json"
	bad := "../../examples/check-selection/chi/nvsa-check.regression.json"
	cases := "../../examples/check-selection/chi/compare-cases.json"
	var out, stderr bytes.Buffer
	code := run([]string{"compare", "-before", good, "-after", bad, "-cases", cases, "-gate"}, &out, &stderr)
	if code != 1 || !strings.Contains(out.String(), "missing optional: middleware-unit") ||
		!strings.Contains(out.String(), "2 cases, 1 candidate failures") {
		t.Fatalf("regression exit %d\n%s\n%s", code, &out, &stderr)
	}
	out.Reset()
	code = run([]string{"compare", "-before", bad, "-after", good, "-cases", cases, "-gate"}, &out, &stderr)
	if code != 0 || !strings.Contains(out.String(), "2 cases, 0 candidate failures") {
		t.Fatalf("repair exit %d\n%s\n%s", code, &out, &stderr)
	}
}

func TestGitAndFixtureAgreeOnRename(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=" + repo}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	put := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, path), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	put("old name.go", "package fixture\n")
	git("add", "--", "old name.go")
	git("commit", "-qm", "base")
	base := git("rev-parse", "HEAD")
	if err := os.Mkdir(filepath.Join(repo, "middleware"), 0700); err != nil {
		t.Fatal(err)
	}
	git("mv", "old name.go", "middleware/test.go")
	git("commit", "-qm", "rename")
	head := git("rev-parse", "HEAD")
	// Move the base forward independently. The base-only file must not become a
	// deletion in the pull request's merge-base diff.
	git("checkout", "--detach", base)
	put("new base file.xyz", "base only")
	git("add", "--", "new base file.xyz")
	git("commit", "-qm", "base advanced")

	put("fixture.json", `{"version":1,"changes":[{"status":"R","old_path":"old name.go","path":"middleware/test.go"}]}`)
	common := []string{"recommend", "-config", "../../arena/check-selection/chi-policy.json", "-format", "json"}
	var fromGit, fromFixture, stderr bytes.Buffer
	if n := run(append(common, "-repo", repo, "-base", "HEAD", "-head", head), &fromGit, &stderr); n != 0 {
		t.Fatalf("git mode exit %d: %s", n, &stderr)
	}
	if n := run(append(common, "-fixture", filepath.Join(repo, "fixture.json")), &fromFixture, &stderr); n != 0 {
		t.Fatalf("fixture mode exit %d: %s", n, &stderr)
	}
	if fromGit.String() != fromFixture.String() || !bytes.Contains(fromGit.Bytes(), []byte(`"router-unit"`)) ||
		!bytes.Contains(fromGit.Bytes(), []byte(`"middleware-unit"`)) {
		t.Fatalf("rename results disagree or miss a side:\n%s\n%s", &fromGit, &fromFixture)
	}
	fromGit.Reset()
	if n := run(append(common, "-repo", repo, "-base", "nonexistent", "-head", head), &fromGit, &stderr); n != 0 ||
		!bytes.Contains(fromGit.Bytes(), []byte(`"fallback": "git-history-unavailable"`)) ||
		!bytes.Contains(fromGit.Bytes(), []byte(`"examples-build"`)) {
		t.Fatalf("missing Git history did not select the full suite (%d): %s\n%s", n, &fromGit, &stderr)
	}
}

func TestGitDiffIncludesIgnoredSubmoduleChanges(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=Fixture",
			"-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=" + repo}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("commit", "--allow-empty", "-qm", "first link target")
	first := git("rev-parse", "HEAD")
	git("commit", "--allow-empty", "-qm", "second link target")
	second := git("rev-parse", "HEAD")
	git("update-index", "--add", "--cacheinfo", "160000,"+first+",vendor/component")
	git("commit", "-qm", "base")
	base := git("rev-parse", "HEAD")
	git("update-index", "--add", "--cacheinfo", "160000,"+second+",vendor/component")
	git("commit", "-qm", "update gitlink")
	git("config", "diff.ignoreSubmodules", "all")

	got, err := checkselect.GitChanges(repo, base, "HEAD")
	want := []checkselect.Change{{Status: "M", Path: "vendor/component"}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("changed submodule not reported: got %v (%v), want %v", got, err, want)
	}
}
