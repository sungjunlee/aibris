package main

import (
	"fmt"
	"sort"
	"time"
)

// Invocation is one measured `scan` of one binary in the four-pair sequence.
type Invocation struct {
	Order      int           `json:"order"`
	Binary     string        `json:"binary"`
	Wall       time.Duration `json:"wall_ns"`
	Items      int64         `json:"items"`
	Bytes      int64         `json:"bytes"`
	InvSig     string        `json:"inv_sig"`
	RetSig     string        `json:"ret_sig"`
	RetBuckets int           `json:"ret_buckets"`
	CacheSig   string        `json:"cache_sig"`
	Partial    bool          `json:"partial"`
	ExitCode   int           `json:"exit_code"`
}

// Pair is one adjacent base/change pair from the alternating sequence.
type Pair struct {
	Index        int           `json:"index"`
	OrderLabel   string        `json:"order"`
	BaseWall     time.Duration `json:"base_wall_ns"`
	ChangeWall   time.Duration `json:"change_wall_ns"`
	Delta        time.Duration `json:"change_minus_base_ns"`
	BaseItems    int64         `json:"base_items"`
	ChangeItems  int64         `json:"change_items"`
	BaseBytes    int64         `json:"base_bytes"`
	ChangeBytes  int64         `json:"change_bytes"`
	Accepted     bool          `json:"accepted"`
	RejectReason string        `json:"reject_reason,omitempty"`
}

// Correctness is the additive non-interference A/B result.
type Correctness struct {
	NonInterference  bool   `json:"non_interference"`
	Additive         bool   `json:"additive"`
	BaseItems        int64  `json:"base_items"`
	ChangeItems      int64  `json:"change_items"`
	BaseBytes        int64  `json:"base_bytes"`
	ChangeBytes      int64  `json:"change_bytes"`
	BaseInvSig       string `json:"base_inv_sig"`
	ChangeInvSig     string `json:"change_inv_sig"`
	BaseRetSig       string `json:"base_ret_sig"`
	ChangeRetSig     string `json:"change_ret_sig"`
	RetentionBuckets int    `json:"change_retention_buckets"`
	Detail           string `json:"detail"`
}

// Report is the full measurement result, renderable as JSON and Markdown.
type Report struct {
	BaseRef      string          `json:"base_ref"`
	ChangeRef    string          `json:"change_ref"`
	BaseSHA      string          `json:"base_source_sha"`
	ChangeSHA    string          `json:"change_source_sha"`
	BaseBinSHA   string          `json:"base_binary_sha256"`
	ChangeBinSHA string          `json:"change_binary_sha256"`
	HomeDesc     string          `json:"home_desc"`
	InputSig     string          `json:"input_fingerprint"`
	InputStable  bool            `json:"input_stable"`
	Pairs        int             `json:"pairs"`
	Correctness  Correctness     `json:"correctness"`
	Invocations  []Invocation    `json:"invocations"`
	PairResults  []Pair          `json:"pair_results"`
	BaseDrift    bool            `json:"base_drift"`
	ChangeDrift  bool            `json:"change_drift"`
	Accepted     []time.Duration `json:"accepted_deltas_ns"`
	AcceptedN    int             `json:"accepted_count"`
	Median       time.Duration   `json:"median_ns"`
	Min          time.Duration   `json:"min_ns"`
	Max          time.Duration   `json:"max_ns"`
	ThresholdSet bool            `json:"threshold_set"`
	Threshold    time.Duration   `json:"threshold_ns"`
	Verdict      string          `json:"verdict"`
}

// pairSequence returns the alternating invocation order for n adjacent pairs:
// even pairs run base->change, odd pairs change->base, giving two of each order
// over the default four-pair block (matches the frozen DOGFOOD series shape).
func pairSequence(n int) []string {
	var seq []string
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			seq = append(seq, "base", "change")
		} else {
			seq = append(seq, "change", "base")
		}
	}
	return seq
}

// Measure runs the four-pair protocol against a pre-generated synthetic home.
func Measure(baseBin, changeBin *Binary, home, homeDesc string, pairs int, threshold time.Duration, thresholdSet bool, tmpDir string) (*Report, error) {
	if pairs <= 0 {
		pairs = 4
	}
	rep := &Report{
		BaseRef: baseBin.SourceRef, ChangeRef: changeBin.SourceRef,
		BaseSHA: baseBin.SourceSHA, ChangeSHA: changeBin.SourceSHA,
		BaseBinSHA: baseBin.SHA256, ChangeBinSHA: changeBin.SHA256,
		HomeDesc: homeDesc, Pairs: pairs,
		ThresholdSet: thresholdSet, Threshold: threshold,
	}

	inputStart, _, err := hashHomeInputs(home)
	if err != nil {
		return nil, fmt.Errorf("input fingerprint: %w", err)
	}
	rep.InputSig = inputStart

	binFor := map[string]*Binary{"base": baseBin, "change": changeBin}

	// Warm-up (unmeasured) primes the activity/scan caches so measured scans are
	// all warm, matching the protocol's warm-series control.
	for _, label := range []string{"base", "change"} {
		if _, err := runScan(binFor[label].Path, home, tmpDir); err != nil {
			return nil, fmt.Errorf("warm-up %s: %w", label, err)
		}
	}

	seq := pairSequence(pairs)
	invs := make([]Invocation, 0, len(seq))
	for i, label := range seq {
		obs, err := runScan(binFor[label].Path, home, tmpDir)
		if err != nil {
			return nil, fmt.Errorf("invocation %d (%s): %w", i+1, label, err)
		}
		retBuckets := 0
		if obs.Result.Retention != nil {
			retBuckets = len(obs.Result.Retention.Buckets)
		}
		invs = append(invs, Invocation{
			Order: i + 1, Binary: label, Wall: obs.Wall,
			Items: obs.Result.Items, Bytes: obs.Result.Bytes,
			InvSig: existingInvSig(obs.Result), RetSig: obs.Result.RetentionSig, RetBuckets: retBuckets,
			CacheSig: obs.CacheAfter, Partial: obs.Result.Partial, ExitCode: obs.ExitCode,
		})
	}
	rep.Invocations = invs

	inputEnd, _, err := hashHomeInputs(home)
	if err != nil {
		return nil, fmt.Errorf("input fingerprint (end): %w", err)
	}
	rep.InputStable = inputStart == inputEnd

	rep.BaseDrift = hasDrift(invs, "base")
	rep.ChangeDrift = hasDrift(invs, "change")
	rep.Correctness = correctnessAB(invs)

	accepted := make([]time.Duration, 0, pairs)
	for p := 0; p < pairs; p++ {
		a, b := invs[2*p], invs[2*p+1]
		var base, change Invocation
		if a.Binary == "base" {
			base, change = a, b
		} else {
			base, change = b, a
		}
		pair := Pair{
			Index: p + 1, OrderLabel: a.Binary + "->" + b.Binary,
			BaseWall: base.Wall, ChangeWall: change.Wall, Delta: change.Wall - base.Wall,
			BaseItems: base.Items, ChangeItems: change.Items,
			BaseBytes: base.Bytes, ChangeBytes: change.Bytes,
		}
		if reason := rejectReason(base, change, rep); reason == "" {
			pair.Accepted = true
			accepted = append(accepted, pair.Delta)
		} else {
			pair.RejectReason = reason
		}
		rep.PairResults = append(rep.PairResults, pair)
	}
	rep.Accepted = accepted
	rep.AcceptedN = len(accepted)
	if len(accepted) > 0 {
		rep.Median, rep.Min, rep.Max = durationStats(accepted)
	}
	rep.Verdict = verdict(rep)
	return rep, nil
}

// hasDrift reports whether the named binary's inventory (InvSig+RetSig) differs
// across its measured appearances. On a frozen synthetic home this is always
// false; a true result means the harness or home is non-deterministic.
func hasDrift(invs []Invocation, label string) bool {
	var invSig, retSig string
	first := true
	for _, inv := range invs {
		if inv.Binary != label {
			continue
		}
		if first {
			invSig, retSig, first = inv.InvSig, inv.RetSig, false
			continue
		}
		if inv.InvSig != invSig || inv.RetSig != retSig {
			return true
		}
	}
	return false
}

// existingInvSig signs the existing inventory (worktrees+summary) excluding the
// retention projection, so base and change can be compared for non-interference
// even though change additionally carries retention. Paired with RetSig
// (retention only), the two signatures fully determine whole-output determinism.
func existingInvSig(r *scanResult) string {
	return hashHex([]byte(r.WorktreesSig + "|" + r.SummarySig))
}

// correctnessAB checks the change is an additive, non-interfering retention
// projection: existing inventory identical, retention present only on change.
func correctnessAB(invs []Invocation) Correctness {
	var base, change *Invocation
	for i := range invs {
		if invs[i].Binary == "base" && base == nil {
			base = &invs[i]
		}
		if invs[i].Binary == "change" && change == nil {
			change = &invs[i]
		}
	}
	c := Correctness{}
	if base == nil || change == nil {
		c.Detail = "missing base or change invocation"
		return c
	}
	c.BaseInvSig, c.ChangeInvSig = base.InvSig, change.InvSig
	c.BaseRetSig, c.ChangeRetSig = base.RetSig, change.RetSig
	c.BaseItems, c.ChangeItems = base.Items, change.Items
	c.BaseBytes, c.ChangeBytes = base.Bytes, change.Bytes
	c.RetentionBuckets = change.RetBuckets
	c.NonInterference = base.InvSig == change.InvSig
	c.Additive = base.RetSig == "" && change.RetSig != ""
	switch {
	case !c.NonInterference:
		c.Detail = "existing inventory differs between base and change (interference)"
	case !c.Additive:
		c.Detail = fmt.Sprintf("retention not additive (baseRetSig=%q changeRetSig=%q)", base.RetSig, change.RetSig)
	default:
		c.Detail = fmt.Sprintf("change adds %d retention bucket(s); existing inventory byte-identical", change.RetBuckets)
	}
	return c
}

func rejectReason(base, change Invocation, rep *Report) string {
	if !rep.InputStable {
		return "input fingerprint changed during run (home not frozen)"
	}
	if base.Partial || change.Partial {
		return "partial scan output"
	}
	if base.ExitCode != 0 || change.ExitCode != 0 {
		return fmt.Sprintf("non-zero exit (base=%d change=%d)", base.ExitCode, change.ExitCode)
	}
	if rep.BaseDrift {
		return "base output non-deterministic across appearances"
	}
	if rep.ChangeDrift {
		return "change output non-deterministic across appearances"
	}
	return ""
}

// durationStats returns the median, min, and max of ds. The median is the
// upper-middle element for an even-length slice (a "high median"), a common
// conservative convention in performance reporting. It is an observation only,
// never a pass/fail, absent a predeclared threshold.
func durationStats(ds []time.Duration) (median, min, max time.Duration) {
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2], s[0], s[len(s)-1]
}

func verdict(rep *Report) string {
	if !rep.Correctness.NonInterference || !rep.Correctness.Additive {
		return "correctness FAILED: change is not an additive, non-interfering retention projection"
	}
	if rep.BaseDrift || rep.ChangeDrift || !rep.InputStable {
		return "drift detected: series not comparable"
	}
	if rep.AcceptedN == 0 {
		return "inconclusive: no drift-free pairs"
	}
	if !rep.ThresholdSet {
		return fmt.Sprintf("inconclusive: no predeclared threshold (observation only; median %s, range [%s, %s] over %d drift-free pair(s))",
			rep.Median, rep.Min, rep.Max, rep.AcceptedN)
	}
	if rep.Median > rep.Threshold {
		return fmt.Sprintf("regression: median change-minus-base %s exceeds threshold %s", rep.Median, rep.Threshold)
	}
	return fmt.Sprintf("within threshold: median change-minus-base %s <= %s (range [%s, %s] over %d drift-free pair(s))",
		rep.Median, rep.Threshold, rep.Min, rep.Max, rep.AcceptedN)
}
