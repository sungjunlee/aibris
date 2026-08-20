package worktree

import "time"

// WorktreeActivitySource identifies the trusted metadata source selected for
// a member's last activity. Constant order is also the deterministic tie
// precedence: Codex session, HEAD reflog, then scanner metadata.
type WorktreeActivitySource string

const (
	WorktreeActivityCodexSession WorktreeActivitySource = "codex_session"
	WorktreeActivityHeadReflog   WorktreeActivitySource = "head_reflog"
	WorktreeActivityFallback     WorktreeActivitySource = "scanner_metadata"

	// worktreeActivitySourceNotRegistered marks a unit produced by a tool
	// aibris has no session-activity reader for. It is a statement about
	// aibris's coverage, not about the worktree: HEAD reflog and scanner
	// metadata still date the unit, so review stays possible on Git evidence
	// alone.
	worktreeActivitySourceNotRegistered = "not-registered"
	worktreeActivityNotRegisteredReason = "no registered activity source for this tool"

	ActivitySourceNotRegistered = worktreeActivitySourceNotRegistered
	ActivityNotRegisteredReason = worktreeActivityNotRegisteredReason
)

// WorktreeActivityEvidence preserves both positive timestamps and source
// availability. Available evidence may have no matching timestamp; this is
// distinct from an index or command outage.
type WorktreeActivityEvidence struct {
	Source    WorktreeActivitySource
	Timestamp time.Time
	Available bool
	Error     string
}
