package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func newGitFixtureRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	newGitFixtureRepoAt(t, repo)
	return repo
}

func newGitFixtureRepoAt(t testing.TB, repo string) {
	t.Helper()
	root := filepath.Dir(repo)
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(root, "remote.git")
	runGitFixture(t, root, "init", "--bare", remote)
	runGitFixture(t, root, "clone", remote, repo)
	runGitFixture(t, repo, "checkout", "-b", "main")
	writeGitFixtureFile(t, repo, "README.md", "initial\n")
	runGitFixture(t, repo, "add", "README.md")
	runGitFixture(t, repo, "commit", "-m", "initial")
	runGitFixture(t, repo, "push", "-u", "origin", "main")
}

func writeGitFixtureFile(t testing.TB, repo, name, content string) {
	t.Helper()
	path := filepath.Join(repo, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func runGitFixture(t testing.TB, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Aibris Test",
		"GIT_AUTHOR_EMAIL=aibris@example.invalid",
		"GIT_COMMITTER_NAME=Aibris Test",
		"GIT_COMMITTER_EMAIL=aibris@example.invalid",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}
