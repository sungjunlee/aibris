package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "perfharness: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		repoFlag      = flag.String("repo", "", "aibris repo root (default: git rev-parse --show-toplevel)")
		baseFlag      = flag.String("base", "", "base git ref (default: merge-base of -change and main)")
		changeFlag    = flag.String("change", "issue-139-codex-sessions-retention-inventory", "change git ref")
		pairs         = flag.Int("pairs", 4, "number of adjacent base/change pairs")
		monthsFlag    = flag.String("months", "", "comma-separated UTC month buckets, e.g. 2024-01,2024-02 (default: built-in past range)")
		filesPerMonth = flag.Int("files-per-month", 40, "rollout leaves per month bucket")
		minBytes      = flag.Int64("min-bytes", 512, "min apparent bytes per rollout")
		maxBytes      = flag.Int64("max-bytes", 4096, "max apparent bytes per rollout")
		liveEvery     = flag.Int("live-every", 3, "one live cwd per N rollouts (0 = all orphaned)")
		nmFiles       = flag.Int("node-modules-files", 3, "files in the auxiliary node_modules dir (<=0 omits it)")
		threshold     = flag.Duration("threshold", 0, "predeclared regression threshold for median change-minus-base (0 = report inconclusive)")
		minPairs      = flag.Int("min-pairs", 3, "minimum drift-free pairs required for a pass/fail threshold verdict")
		quorum        = flag.Float64("quorum", 0.67, "fraction of accepted pairs that must individually exceed the threshold for a regression")
		workdirFlag   = flag.String("workdir", "", "working directory for exported trees/binaries/home (default: a temp dir)")
		keep          = flag.Bool("keep", false, "keep the working directory after the run")
		jsonOut       = flag.String("json-out", "", "write the JSON report to this path")
		mdOut         = flag.String("md-out", "", "write the Markdown report to this path")
		quick         = flag.Bool("quick", false, "small synthetic home for a fast smoke run")
		homeFlag      = flag.String("home", "", "measure an existing home instead of generating a synthetic one (for a real-home run on a quiet window)")
	)
	flag.Parse()

	repo := *repoFlag
	if repo == "" {
		tl, err := gitToplevel()
		if err != nil {
			return fmt.Errorf("determining repo root (use -repo): %w", err)
		}
		repo = tl
	}

	changeRef := *changeFlag
	baseRef := *baseFlag
	if baseRef == "" {
		mb, err := gitMergeBase(repo, changeRef, "main")
		if err != nil {
			return fmt.Errorf("computing base (merge-base of %s and main; use -base): %w", changeRef, err)
		}
		baseRef = mb
	}

	workdir := *workdirFlag
	if workdir == "" {
		wd, err := os.MkdirTemp("", "aibris-perfharness-")
		if err != nil {
			return err
		}
		workdir = wd
	} else if err := os.MkdirAll(workdir, 0o755); err != nil {
		return err
	}
	if !*keep {
		defer os.RemoveAll(workdir)
	}

	fmt.Fprintf(os.Stderr, "building base (%s) and change (%s) binaries...\n", baseRef, changeRef)
	baseBin, err := BuildBinary("base", baseRef, repo, workdir)
	if err != nil {
		return err
	}
	changeBin, err := BuildBinary("change", changeRef, repo, workdir)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "base   %s -> binary %s\n", baseBin.SourceSHA[:12], baseBin.SHA256[:12])
	fmt.Fprintf(os.Stderr, "change %s -> binary %s\n", changeBin.SourceSHA[:12], changeBin.SHA256[:12])

	opts := HomeOpts{
		FilesPerMonth:    *filesPerMonth,
		MinBytes:         *minBytes,
		MaxBytes:         *maxBytes,
		LiveEvery:        *liveEvery,
		NodeModulesFiles: *nmFiles,
	}
	if *monthsFlag != "" {
		opts.Months = splitCSV(*monthsFlag)
	}
	if *quick {
		if opts.Months == nil {
			opts.Months = []string{"2024-01", "2024-02"}
		}
		if *filesPerMonth == 40 {
			opts.FilesPerMonth = 6
		}
	}
	var home, homeDesc string
	if *homeFlag != "" {
		home = *homeFlag
		if fi, err := os.Stat(home); err != nil || !fi.IsDir() {
			return fmt.Errorf("home %q is not an existing directory", home)
		}
		homeDesc = "existing home: " + home
		fmt.Fprintf(os.Stderr, "measuring existing home %s (ensure it is quiescent; drift rejects pairs)\n", home)
	} else {
		spec := DefaultHomeSpec(opts)
		home = filepath.Join(workdir, "home")
		if err := Generate(home, spec); err != nil {
			return fmt.Errorf("generating synthetic home: %w", err)
		}
		homeDesc = describeSpec(spec)
	}
	tmpDir := filepath.Join(workdir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "running %d-pair measurement on %s...\n", *pairs, homeDesc)
	rep, err := Measure(baseBin, changeBin, home, homeDesc, *pairs, *threshold, *threshold > 0, *minPairs, *quorum, tmpDir)
	if err != nil {
		return err
	}

	md := RenderMarkdown(rep)
	fmt.Println(md)

	if *jsonOut != "" {
		jb, err := RenderJSON(rep)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*jsonOut, jb, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", *jsonOut)
	}
	if *mdOut != "" {
		if err := os.WriteFile(*mdOut, []byte(md), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", *mdOut)
	}
	if *keep {
		fmt.Fprintf(os.Stderr, "kept working directory: %s\n", workdir)
	}
	return nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func gitToplevel() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitMergeBase(repoDir, a, b string) (string, error) {
	cmd := exec.Command("git", "-C", repoDir, "merge-base", a, b)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// describeSpec summarizes a synthetic home spec for the report.
func describeSpec(spec HomeSpec) string {
	months := map[string]bool{}
	liveSet := map[string]bool{}
	for _, lt := range spec.LiveTargets {
		liveSet[lt] = true
	}
	live, orphan := 0, 0
	for _, r := range spec.Rollouts {
		months[fmt.Sprintf("%04d-%02d", r.Year, r.Month)] = true
		if liveSet[r.CWDTgt] {
			live++
		} else {
			orphan++
		}
	}
	nm := 0
	for _, n := range spec.NodeModules {
		nm += len(n.Files)
	}
	return fmt.Sprintf("%d rollout leaves (%d live / %d orphan) across %d month bucket(s); %d node_modules file(s)",
		len(spec.Rollouts), live, orphan, len(months), nm)
}
