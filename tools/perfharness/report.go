package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RenderJSON returns the report as indented JSON (machine-readable evidence).
func RenderJSON(rep *Report) ([]byte, error) {
	return json.MarshalIndent(rep, "", "  ")
}

func dur(d time.Duration) string { return d.String() }

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// RenderMarkdown returns a DOGFOOD-style human report.
func RenderMarkdown(rep *Report) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	w("# aibris offline four-pair measurement")
	w("")
	w("Frozen #139 L2 protocol; deterministic-input layer (#129). Offline observation only.")
	w("")
	w("**Verdict:** %s", rep.Verdict)
	w("")
	w("## Immutable inputs")
	w("")
	w("Platform: `%s`", rep.Platform)
	w("")
	w("| | base | change |")
	w("| --- | --- | --- |")
	w("| source ref | `%s` | `%s` |", rep.BaseRef, rep.ChangeRef)
	w("| source sha | `%s` | `%s` |", short(rep.BaseSHA), short(rep.ChangeSHA))
	w("| binary sha256 | `%s` | `%s` |", short(rep.BaseBinSHA), short(rep.ChangeBinSHA))
	w("")
	w("## Synthetic home")
	w("")
	w("%s", rep.HomeDesc)
	w("")
	w("Input fingerprint: `%s` (stable across run: %s)", short(rep.InputSig), yesNo(rep.InputStable))
	w("")
	w("## Correctness A/B (additive, non-interfering)")
	w("")
	c := rep.Correctness
	w("- non-interference (existing inventory byte-identical): %s", yesNo(c.NonInterference))
	w("- additive (retention only on change): %s", yesNo(c.Additive))
	w("- base items / bytes: %d / %d", c.BaseItems, c.BaseBytes)
	w("- change items / bytes: %d / %d", c.ChangeItems, c.ChangeBytes)
	w("- change retention buckets: %d", c.RetentionBuckets)
	w("- detail: %s", c.Detail)
	w("")
	w("## Warm four-pair series")
	w("")
	w("Times are wall-clock `real`. Scale is `items/bytes`. `change-base` is computed regardless of order.")
	w("")
	w("| Pair / order | Base time; scale | Change time; scale | change-base | Decision |")
	w("| --- | --- | --- | ---: | --- |")
	for _, p := range rep.PairResults {
		decision := "accepted"
		if !p.Accepted {
			decision = "discarded: " + p.RejectReason
		}
		w("| %d / %s | %s; %d/%d | %s; %d/%d | %s | %s |",
			p.Index, p.OrderLabel,
			dur(p.BaseWall), p.BaseItems, p.BaseBytes,
			dur(p.ChangeWall), p.ChangeItems, p.ChangeBytes,
			dur(p.Delta), decision)
	}
	w("")
	w("## Drift")
	w("")
	w("- base output drift: %s", yesNo(rep.BaseDrift))
	w("- change output drift: %s", yesNo(rep.ChangeDrift))
	w("- input fingerprint stable: %s", yesNo(rep.InputStable))
	w("")
	w("## Accepted deltas")
	w("")
	w("- accepted: %d of %d pairs", rep.AcceptedN, rep.Pairs)
	if rep.AcceptedN > 0 {
		w("- median / min / max: %s / %s / %s", dur(rep.Median), dur(rep.Min), dur(rep.Max))
		if rep.ThresholdSet {
			w("- predeclared threshold: %s; min-pairs %d; quorum %.2f; %d accepted pair(s) exceed individually",
				dur(rep.Threshold), rep.MinPairs, rep.Quorum, rep.Exceeding)
		} else {
			w("- predeclared threshold: none (series is an observation, not a pass/fail)")
		}
	}
	w("")
	w("## Verdict")
	w("")
	w("%s", rep.Verdict)
	w("")
	w("---")
	w("")
	w("This harness validates the four-pair mechanics and the additive, non-interfering shape of the Codex-sessions retention projection on a deterministic synthetic home. It does NOT close the real-home Done Criteria (DC19-21); those still require a quiescent real-home measurement window, and the #139 L2 park stays in effect.")
	return b.String()
}
