package main

import (
	"strings"
	"testing"
	"time"
)

func TestPairSequence(t *testing.T) {
	cases := []struct {
		pairs int
		want  []string
	}{
		{1, []string{"base", "change"}},
		{2, []string{"base", "change", "change", "base"}},
		{4, []string{"base", "change", "change", "base", "base", "change", "change", "base"}},
	}
	for _, c := range cases {
		got := pairSequence(c.pairs)
		if len(got) != len(c.want) {
			t.Fatalf("pairSequence(%d) len = %d; want %d (%v)", c.pairs, len(got), len(c.want), got)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("pairSequence(%d)[%d] = %s; want %s", c.pairs, i, got[i], c.want[i])
			}
		}
	}
}

func TestExistingInvSigExcludesRetention(t *testing.T) {
	base := &scanResult{WorktreesSig: "w", SummarySig: "s", RetentionSig: ""}
	change := &scanResult{WorktreesSig: "w", SummarySig: "s", RetentionSig: "r"}
	if existingInvSig(base) != existingInvSig(change) {
		t.Fatalf("existingInvSig must exclude retention: base=%s change=%s",
			existingInvSig(base), existingInvSig(change))
	}
	other := &scanResult{WorktreesSig: "w2", SummarySig: "s", RetentionSig: ""}
	if existingInvSig(base) == existingInvSig(other) {
		t.Fatalf("existingInvSig must reflect a worktrees change")
	}
	otherSummary := &scanResult{WorktreesSig: "w", SummarySig: "s2", RetentionSig: ""}
	if existingInvSig(base) == existingInvSig(otherSummary) {
		t.Fatalf("existingInvSig must reflect a summary change")
	}
}

func TestCorrectnessAB(t *testing.T) {
	invs := []Invocation{
		{Binary: "base", InvSig: "inv", RetSig: "", RetBuckets: 0, Items: 1, Bytes: 10},
		{Binary: "change", InvSig: "inv", RetSig: "ret", RetBuckets: 2, Items: 1, Bytes: 10},
	}
	c := correctnessAB(invs)
	if !c.NonInterference {
		t.Fatalf("identical InvSig must be non-interfering")
	}
	if !c.Additive {
		t.Fatalf("empty base RetSig + non-empty change RetSig must be additive")
	}
	if c.RetentionBuckets != 2 {
		t.Fatalf("RetentionBuckets = %d; want 2", c.RetentionBuckets)
	}

	interference := []Invocation{
		{Binary: "base", InvSig: "inv1", RetSig: ""},
		{Binary: "change", InvSig: "inv2", RetSig: "ret"},
	}
	if correctnessAB(interference).NonInterference {
		t.Fatalf("differing InvSig must be interference")
	}

	notAdditive := []Invocation{
		{Binary: "base", InvSig: "inv", RetSig: "ret"},
		{Binary: "change", InvSig: "inv", RetSig: "ret"},
	}
	if correctnessAB(notAdditive).Additive {
		t.Fatalf("base carrying retention must not be additive")
	}

	if d := correctnessAB(nil).Detail; d == "" {
		t.Fatalf("missing base/change should set an explanatory detail")
	}
}

func TestRejectReason(t *testing.T) {
	clean := func() (*Report, Invocation, Invocation) {
		return &Report{InputStable: true}, Invocation{ExitCode: 0}, Invocation{ExitCode: 0}
	}

	rep, b, c := clean()
	if r := rejectReason(b, c, rep); r != "" {
		t.Fatalf("clean pair should have no reject reason; got %q", r)
	}

	rep, b, c = clean()
	rep.InputStable = false
	if r := rejectReason(b, c, rep); r == "" {
		t.Fatalf("unstable input fingerprint must be rejected")
	}

	rep, b, c = clean()
	b.Partial = true
	if r := rejectReason(b, c, rep); r == "" {
		t.Fatalf("partial base scan must be rejected")
	}

	rep, b, c = clean()
	c.ExitCode = 1
	if r := rejectReason(b, c, rep); r == "" {
		t.Fatalf("non-zero change exit must be rejected")
	}

	rep, b, c = clean()
	rep.ChangeDrift = true
	if r := rejectReason(b, c, rep); r == "" {
		t.Fatalf("change drift must be rejected")
	}
}

func TestVerdict(t *testing.T) {
	base := func() *Report {
		return &Report{
			Correctness: Correctness{NonInterference: true, Additive: true},
			InputStable: true,
			AcceptedN:   3,
			Quorum:      0.67,
			MinPairs:    3,
			Accepted:    []time.Duration{5 * time.Second, 5 * time.Second, 5 * time.Second},
			Median:      5 * time.Second,
			Min:         1 * time.Second,
			Max:         9 * time.Second,
		}
	}

	r := base()
	r.Correctness.NonInterference = false
	if v := verdict(r); !strings.HasPrefix(v, "correctness FAILED") {
		t.Fatalf("verdict = %q; want correctness FAILED...", v)
	}

	r = base()
	r.BaseDrift = true
	if v := verdict(r); !strings.HasPrefix(v, "drift detected") {
		t.Fatalf("verdict = %q; want drift detected...", v)
	}

	r = base()
	r.InputStable = false
	if v := verdict(r); !strings.HasPrefix(v, "drift detected") {
		t.Fatalf("verdict = %q; want drift detected for unstable input...", v)
	}

	r = base()
	r.AcceptedN = 0
	r.Accepted = nil
	if v := verdict(r); !strings.HasPrefix(v, "inconclusive: no drift-free pairs") {
		t.Fatalf("verdict = %q; want inconclusive: no drift-free pairs", v)
	}

	r = base()
	r.ThresholdSet = false
	if v := verdict(r); !strings.HasPrefix(v, "inconclusive: no predeclared threshold") {
		t.Fatalf("verdict = %q; want inconclusive: no predeclared threshold...", v)
	}

	// Below min-pairs: no pass/fail verdict even with a threshold.
	r = base()
	r.ThresholdSet = true
	r.Threshold = 4 * time.Second
	r.Exceeding = 3
	r.AcceptedN = 2
	r.Accepted = []time.Duration{5 * time.Second, 5 * time.Second}
	if v := verdict(r); !strings.HasPrefix(v, "inconclusive: only 2 drift-free pair(s)") {
		t.Fatalf("verdict = %q; want inconclusive below min-pairs", v)
	}

	// Regression: median above threshold AND majority individually exceed.
	r = base()
	r.ThresholdSet = true
	r.Threshold = 4 * time.Second // median 5s > 4s
	r.Exceeding = 3               // quorum 0.67 of 3 = 2, so 3 meets it
	if v := verdict(r); !strings.HasPrefix(v, "regression") {
		t.Fatalf("verdict = %q; want regression...", v)
	}

	// Noise, not regression: median above threshold but quorum not met.
	// Not reachable at the default four-pair config (high-median already implies
	// the quorum for acceptedN <= 5); here it validates the branch in isolation
	// for larger -pairs where a regression affecting only half the pairs is
	// correctly reported as noise, not a false regression.
	r = base()
	r.ThresholdSet = true
	r.Threshold = 4 * time.Second
	r.Exceeding = 1 // quorum 0.67 of 3 = 2, so 1 is insufficient
	if v := verdict(r); !strings.HasPrefix(v, "no regression") {
		t.Fatalf("verdict = %q; want no regression (noise)...", v)
	}

	// Within threshold: median at/below threshold.
	r = base()
	r.ThresholdSet = true
	r.Threshold = 6 * time.Second // median 5s <= 6s
	r.Exceeding = 0
	if v := verdict(r); !strings.HasPrefix(v, "within threshold") {
		t.Fatalf("verdict = %q; want within threshold...", v)
	}
}

func TestCountExceedingAndMajorityNeeded(t *testing.T) {
	if got := countExceeding([]time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 10 * time.Millisecond}, 5*time.Millisecond); got != 1 {
		t.Fatalf("countExceeding = %d; want 1", got)
	}
	// No threshold => always zero.
	if got := countExceeding([]time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 10 * time.Millisecond}, 0); got != 0 {
		t.Fatalf("countExceeding (no threshold) = %d; want 0", got)
	}
	if got := majorityNeeded(3, 0.67); got != 2 {
		t.Fatalf("majorityNeeded(3, 0.67) = %d; want 2", got)
	}
	if got := majorityNeeded(4, 0.5); got != 2 {
		t.Fatalf("majorityNeeded(4, 0.5) = %d; want 2", got)
	}
	if got := majorityNeeded(4, 1.0); got != 4 {
		t.Fatalf("majorityNeeded(4, 1.0) = %d; want 4", got)
	}
}

func TestDurationStatsHighMedian(t *testing.T) {
	// Even-length: the median is the upper-middle element (high median).
	med, mn, mx := durationStats([]time.Duration{1, 2, 3, 4})
	if med != 3 || mn != 1 || mx != 4 {
		t.Fatalf("durationStats(1,2,3,4) = med %d, min %d, max %d; want med 3 (high median), min 1, max 4", med, mn, mx)
	}
	// Odd-length: the true middle.
	med, mn, mx = durationStats([]time.Duration{5, 1, 3})
	if med != 3 || mn != 1 || mx != 5 {
		t.Fatalf("durationStats(5,1,3) = med %d, min %d, max %d; want med 3, min 1, max 5", med, mn, mx)
	}
}

func TestHasDrift(t *testing.T) {
	stable := []Invocation{
		{Binary: "base", InvSig: "b", RetSig: ""},
		{Binary: "change", InvSig: "c", RetSig: "r"},
		{Binary: "base", InvSig: "b", RetSig: ""},
		{Binary: "change", InvSig: "c", RetSig: "r"},
	}
	if hasDrift(stable, "base") {
		t.Fatalf("stable base appearances must not be drift")
	}
	if hasDrift(stable, "change") {
		t.Fatalf("stable change appearances must not be drift")
	}

	invDrift := []Invocation{
		{Binary: "base", InvSig: "b1", RetSig: ""},
		{Binary: "base", InvSig: "b2", RetSig: ""},
	}
	if !hasDrift(invDrift, "base") {
		t.Fatalf("differing InvSig across base appearances must be drift")
	}

	retDrift := []Invocation{
		{Binary: "change", InvSig: "c", RetSig: "r1"},
		{Binary: "change", InvSig: "c", RetSig: "r2"},
	}
	if !hasDrift(retDrift, "change") {
		t.Fatalf("differing RetSig across change appearances must be drift")
	}
}
