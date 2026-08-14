//go:build windows

package scanner

// pathOwnedByCurrentUser reports whether the current user owns path. Windows
// exposes no portable uid; the ownership gate for an explicitly rooted system
// temp dir therefore relies on the recorded-cwd owning-agent signal.
func pathOwnedByCurrentUser(_ string) bool {
	return true
}
