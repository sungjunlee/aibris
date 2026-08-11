package codexhome

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestHomeHonorsCodexHomeEnv(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	codexHome := filepath.Join(t.TempDir(), "codex-runtime-home")
	t.Setenv("CODEX_HOME", codexHome)

	home, err := Home()
	if err != nil {
		t.Fatal(err)
	}
	if home != codexHome {
		t.Fatalf("Home() = %q; want %q", home, codexHome)
	}
}

func TestHomeDefaultsToDotCodex(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("CODEX_HOME", "")

	got, err := Home()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".codex"); got != want {
		t.Fatalf("Home() = %q; want %q", got, want)
	}
}

func TestHomeIgnoresBlankCodexHomeEnv(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("CODEX_HOME", "   ")

	got, err := Home()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".codex"); got != want {
		t.Fatalf("Home() = %q; want %q", got, want)
	}
}

func TestExtraHomesParsesPathStyleList(t *testing.T) {
	t.Setenv("AIBRIS_CODEX_HOMES", "")
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	sep := string(filepath.ListSeparator)
	t.Setenv("AIBRIS_CODEX_HOMES", first+sep+sep+"  "+sep+"relative/path"+sep+second)

	got := ExtraHomes()
	want := []string{first, second}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtraHomes() = %v; want %v", got, want)
	}
}

func TestExtraHomesEmptyWhenUnset(t *testing.T) {
	t.Setenv("AIBRIS_CODEX_HOMES", "")
	if got := ExtraHomes(); len(got) != 0 {
		t.Fatalf("ExtraHomes() = %v; want none", got)
	}
}

func TestHomesDeduplicatesPrimaryAndExtras(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("CODEX_HOME", "")
	primary := filepath.Join(home, ".codex")
	extra := filepath.Join(t.TempDir(), "extra")
	t.Setenv("AIBRIS_CODEX_HOMES", primary+string(filepath.ListSeparator)+extra+string(filepath.ListSeparator)+extra)

	got, err := Homes()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{primary, extra}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Homes() = %v; want %v", got, want)
	}
}
