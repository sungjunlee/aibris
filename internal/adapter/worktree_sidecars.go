package adapter

// registeredWorktreeSidecars are finite exact member names that are not
// checkout candidates. They are skipped during member classification so an
// empty or occupied sidecar cannot fail-close a valid owner.
var registeredWorktreeSidecars = map[string]struct{}{
	".orca-worktree-trash": {},
}

// IsWorktreeSidecarName reports whether name is a registered worktree sidecar.
func IsWorktreeSidecarName(name string) bool {
	_, ok := registeredWorktreeSidecars[name]
	return ok
}
