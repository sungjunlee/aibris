package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func releaseStageIndexes(t *testing.T, job workflowJob) (draft, attest, public, tap int) {
	t.Helper()
	draft, attest, public, tap = -1, -1, -1, -1
	stages := 0
	for i, step := range job.Steps {
		switch {
		case strings.HasPrefix(step.Uses, "goreleaser/goreleaser-action@"):
			draft = i
			stages++
		case strings.HasPrefix(step.Uses, "actions/attest-build-provenance@"):
			attest = i
			stages++
		case strings.Contains(step.Run, "gh release edit"):
			public = i
			stages++
		case strings.Contains(step.Run, "publish-homebrew-formula.sh"):
			tap = i
			stages++
		}
	}
	if stages != 4 || draft < 0 || attest <= draft || public <= attest || tap <= public {
		t.Fatal("parsed release steps must create a draft, attest, publish, then update the tap")
	}
	return
}

func assertReleaseVerificationGraph(t *testing.T) {
	t.Helper()
	release := readWorkflowContract(t, ".github/workflows/release.yml")
	verify, ok := release.Jobs["verify"]
	if !ok {
		t.Fatal("release has no same-SHA reusable verification job")
	}
	if verify.Uses != "./.github/workflows/ci.yml" || verify.With["commit"] != "${{ github.sha }}" {
		t.Fatal("release must call shared CI with the immutable tag event commit")
	}
	assertRequiredJob(t, "verify", verify)
	if verify.Permissions["contents"] != "read" {
		t.Error("verification must have read-only source access")
	}
	ci := readWorkflowContract(t, ".github/workflows/ci.yml")
	input := ci.On["workflow_call"].Inputs["commit"]
	if !input.Required || input.Type != "string" {
		t.Error("shared CI must require a commit input")
	}
	for _, name := range []string{"check", "test", "windows-safety", "release-build"} {
		if _, exists := ci.Jobs[name]; !exists {
			t.Errorf("shared CI missing required job %s", name)
		}
	}
	for name, job := range ci.Jobs {
		assertRequiredJob(t, "CI/"+name, job)
		assertCheckoutCommit(t, name, job, "${{ inputs.commit || github.sha }}")
	}
	for _, name := range []string{"goreleaser", "brew-pour"} {
		job, exists := release.Jobs[name]
		if !exists || !slices.Contains(jobNeeds(t, job), "verify") {
			t.Errorf("%s must explicitly need same-SHA verification", name)
		}
		assertRequiredJob(t, name, job)
		assertCheckoutCommit(t, name, job, "${{ github.sha }}")
	}
	if !slices.Contains(jobNeeds(t, release.Jobs["brew-pour"]), "goreleaser") {
		t.Error("pour must need successful attested publication")
	}
	releaseStageIndexes(t, release.Jobs["goreleaser"])
	pinned := regexp.MustCompile(`^[0-9a-f]{40}$`)
	version := regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	for name, job := range release.Jobs {
		for _, step := range job.Steps {
			if step.Uses == "" {
				continue
			}
			_, sha, _ := strings.Cut(step.Uses, "@")
			if !pinned.MatchString(sha) {
				t.Errorf("privileged release action %s must be commit-pinned", step.Uses)
			}
			if !strings.Contains(readRepoFile(t, ".github/workflows/release.yml"), step.Uses+" # v") {
				t.Errorf("%s action pin needs a version comment", name)
			}
			if strings.HasPrefix(step.Uses, "goreleaser/goreleaser-action@") && !version.MatchString(step.With["version"]) {
				t.Error("GoReleaser must use an exact version")
			}
			if strings.HasPrefix(step.Uses, "anchore/sbom-action/download-syft@") && !version.MatchString(step.With["syft-version"]) {
				t.Error("syft must use an exact version")
			}
		}
	}
	if !strings.Contains(readRepoFile(t, ".github/dependabot.yml"), "package-ecosystem: github-actions") {
		t.Error("Dependabot must continue managing action pins")
	}
}

func TestReleasePublishFixtures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("publication uses the Ubuntu runner shell; PowerShell/native Windows are separate CI checks")
	}
	release := readWorkflowContract(t, ".github/workflows/release.yml")
	ci := readWorkflowContract(t, ".github/workflows/ci.yml")
	job := release.Jobs["goreleaser"]
	draft, attest, public, tap := releaseStageIndexes(t, job)
	const sha = "0123456789abcdef0123456789abcdef01234567"
	type fixture struct{ name, failedJob, status, verifiedSHA, want string }
	cases := []fixture{
		{"success", "", "", sha, "draft\nattest\npublic\ntap\n"},
		{"another-SHA", "", "", strings.Repeat("f", 40), ""},
		{"verification-absent", "verify", "", sha, ""},
		{"verification-failed", "verify", "failure", sha, ""},
		{"verification-skipped", "verify", "skipped", sha, ""},
		{"verification-cancelled", "verify", "cancelled", sha, ""},
		{"draft-failed", "draft", "failure", sha, "draft\n"},
		{"attestation-failed", "attest", "failure", sha, "draft\nattest\n"},
		{"attestation-cancelled", "attest", "cancelled", sha, "draft\nattest\n"},
		{"public-failed", "public", "failure", sha, "draft\nattest\npublic\n"},
		{"tap-failed", "tap", "failure", sha, "draft\nattest\npublic\ntap\n"},
	}
	for name := range ci.Jobs {
		for _, status := range []string{"failure", "cancelled", "skipped", ""} {
			cases = append(cases, fixture{name + "/" + status, name, status, sha, ""})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			log := filepath.Join(home, "calls")
			if err := os.WriteFile(log, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(home, "bin")
			scripts := filepath.Join(home, ".github", "scripts")
			for _, dir := range []string{bin, scripts} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for path, stage := range map[string]string{
				filepath.Join(bin, "draft"):                           "draft",
				filepath.Join(bin, "attest"):                          "attest",
				filepath.Join(bin, "gh"):                              "public",
				filepath.Join(scripts, "publish-homebrew-formula.sh"): "tap",
			} {
				body := "#!/bin/sh\nprintf '" + stage + "\\n' >> \"$CALL_LOG\"\n[ \"$FAIL_STAGE\" != " + stage + " ]\n"
				if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			// Model only Actions' default needs-success rule over the parsed job
			// graph. The actual publication shell commands run against stubs.
			results := map[string]string{"verify": "success"}
			for name := range ci.Jobs {
				results[name] = "success"
				if tc.failedJob == name {
					results[name] = tc.status
				}
				if results[name] != "success" {
					results["verify"] = "failure"
				}
			}
			if tc.failedJob == "verify" {
				results["verify"] = tc.status
			}
			// A green run of a different SHA cannot be this invocation's verify.
			if tc.verifiedSHA != sha {
				delete(results, "verify")
			}
			eligible := true
			for _, need := range jobNeeds(t, job) {
				if results[need] != "success" {
					eligible = false
				}
			}
			if eligible {
				// Use the parsed step sequence; replace only network-capable
				// actions/commands with local logging stubs.
				var commands []string
				for i := draft; i <= tap; i++ {
					switch i {
					case draft:
						commands = append(commands, "draft")
					case attest:
						commands = append(commands, "attest")
					case public, tap:
						commands = append(commands, job.Steps[i].Run)
					default:
						t.Fatalf("unexpected publication stage: %+v", job.Steps[i])
					}
				}
				cmd := exec.Command("bash", "-e", "-c", strings.Join(commands, "\n"))
				cmd.Dir = home
				cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin", "CALL_LOG="+log, "FAIL_STAGE="+tc.failedJob, "GITHUB_REF_NAME=v0.0.0-fixture")
				out, err := cmd.CombinedOutput()
				failedStage := slices.Contains([]string{"draft", "attest", "public", "tap"}, tc.failedJob)
				if (err != nil) != failedStage {
					t.Fatalf("publication exit: %v\n%s", err, out)
				}
			}
			data, err := os.ReadFile(log)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if string(data) != tc.want {
				t.Fatalf("stub calls = %q; want %q (needs %v)", data, tc.want, jobNeeds(t, job))
			}
		})
	}
}
