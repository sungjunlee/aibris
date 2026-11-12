package codexactivity

import "strings"

// dosPathFromFinal converts a GetFinalPathNameByHandle result into a DOS path
// canonicalPath can be compared with. Volume GUID and other non-DOS forms are
// rejected so the caller fails closed instead of treating them as agreement.
func dosPathFromFinal(raw string) (string, bool) {
	const (
		prefix = `\\?\`
		unc    = `\\?\UNC\`
	)
	switch {
	case strings.HasPrefix(raw, unc):
		raw = `\\` + raw[len(unc):]
	case strings.HasPrefix(raw, prefix):
		raw = raw[len(prefix):]
		if len(raw) < 2 || raw[1] != ':' {
			return "", false
		}
	case len(raw) >= 2 && raw[1] == ':':
	case strings.HasPrefix(raw, `\\`):
	default:
		return "", false
	}
	if raw == "" || raw == `\\` {
		return "", false
	}
	return raw, true
}
