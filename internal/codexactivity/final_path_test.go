package codexactivity

import "testing"

func TestDOSPathFromFinal(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{raw: `\\?\C:\codex`, want: `C:\codex`, ok: true},
		{raw: `\\?\UNC\server\share\codex`, want: `\\server\share\codex`, ok: true},
		{raw: `C:\codex`, want: `C:\codex`, ok: true},
		{raw: `\\server\share\codex`, want: `\\server\share\codex`, ok: true},
		{raw: `\\?\Volume{1234-abcd}\codex`},
		{raw: `\\?\`},
		{raw: ``},
		{raw: `relative\codex`},
	}
	for _, tc := range cases {
		got, ok := dosPathFromFinal(tc.raw)
		if ok != tc.ok || got != tc.want {
			t.Errorf("dosPathFromFinal(%q) = %q, %t; want %q, %t", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}
