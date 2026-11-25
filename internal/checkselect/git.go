package checkselect

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// GitChanges reads the committed PR diff from the merge base. It never reads the
// working tree or configures the repository.
func GitChanges(repo, base, head string) ([]Change, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	git := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(cmd.Environ(), "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0")
		return cmd.Output()
	}
	resolve := func(ref string) (string, error) {
		out, err := git("rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
		return strings.TrimSpace(string(out)), err
	}
	baseID, err := resolve(base)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve base %q: %w", base, err)
	}
	headID, err := resolve(head)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve head %q: %w", head, err)
	}
	out, err := git("merge-base", "--all", baseID, headID)
	if err != nil {
		return nil, fmt.Errorf("cannot determine merge base: %w", err)
	}
	bases := strings.Fields(string(out))
	if len(bases) != 1 {
		return nil, fmt.Errorf("expected one merge base, got %d", len(bases))
	}
	// User diff preferences must not hide paths that affect the policy.
	diff, err := git("diff", "--name-status", "-z", "--find-renames", "--no-ext-diff", "--no-textconv",
		"--no-relative", "--ignore-submodules=none", bases[0], headID, "--")
	if err != nil {
		return nil, fmt.Errorf("cannot read Git diff: %w", err)
	}
	return parseGitChanges(diff)
}

func parseGitChanges(data []byte) ([]Change, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if data[len(data)-1] != 0 {
		return nil, fmt.Errorf("unterminated Git diff record")
	}
	fields := bytes.Split(data[:len(data)-1], []byte{0})
	var changes []Change
	for len(fields) > 0 {
		if len(fields[0]) == 0 || len(fields) < 2 {
			return nil, fmt.Errorf("incomplete Git diff record")
		}
		change := Change{Status: string(fields[0][:1]), Path: string(fields[1])}
		fields = fields[2:]
		if change.Status == "R" || change.Status == "C" {
			if len(fields) == 0 {
				return nil, fmt.Errorf("Git diff record lacks destination path")
			}
			change.OldPath, change.Path = change.Path, string(fields[0])
			fields = fields[1:]
		}
		changes = append(changes, change)
	}
	return Normalize(changes)
}
