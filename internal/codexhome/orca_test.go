package codexhome

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestHomesOrcaPlatformAndDeduplication(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	orca := testutil.OrcaCodexHome(t, home)
	primary := filepath.Join(home, ".codex")
	got, err := Homes()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{primary}
	if runtime.GOOS == "darwin" {
		want = append(want, orca)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Homes() = %v; want %v", got, want)
	}
	for _, env := range []string{"AIBRIS_CODEX_HOMES", "CODEX_HOME"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(env, orca)
			got, err := Homes()
			if err != nil {
				t.Fatal(err)
			}
			want := []string{primary, orca}
			if env == "CODEX_HOME" {
				want = []string{orca}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Homes() = %v; want deduplicated %v", got, want)
			}
		})
	}
}

func TestHomesOrcaRejectsInvalidLayoutSilently(t *testing.T) {
	for _, scenario := range []string{"missing-config", "config-directory", "config-symlink", "missing-sessions", "sessions-file", "sessions-symlink"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			orca := testutil.OrcaCodexHome(t, home)
			entry := filepath.Join(orca, "config.toml")
			if scenario == "missing-sessions" || scenario == "sessions-file" || scenario == "sessions-symlink" {
				entry = filepath.Join(orca, "sessions")
			}
			if err := os.Remove(entry); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "config-directory":
				if err := os.Mkdir(entry, 0755); err != nil {
					t.Fatal(err)
				}
			case "sessions-file":
				if err := os.WriteFile(entry, nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "config-symlink", "sessions-symlink":
				target := t.TempDir()
				if scenario == "config-symlink" {
					target = filepath.Join(target, "config")
					if err := os.WriteFile(target, nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, entry); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			got, err := Homes()
			if err != nil || !reflect.DeepEqual(got, []string{filepath.Join(home, ".codex")}) {
				t.Fatalf("Homes() = %v, %v; invalid layout must be ignored", got, err)
			}
		})
	}
}

func TestHomesOrcaDeduplicatesConfiguredHomeAlias(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Orca Codex home is macOS-only")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	orca := testutil.OrcaCodexHome(t, home)
	alias := filepath.Join(home, "orca-home-alias")
	if err := os.Symlink(orca, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for _, env := range []string{"CODEX_HOME", "AIBRIS_CODEX_HOMES"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(env, alias)
			got, err := Homes()
			want := []string{alias}
			if env == "AIBRIS_CODEX_HOMES" {
				want = []string{filepath.Join(home, ".codex"), alias}
			}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("Homes() = %v, %v; want no duplicate auto-discovered alias: %v", got, err, want)
			}
		})
	}
}
