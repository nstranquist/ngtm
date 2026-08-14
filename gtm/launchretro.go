package gtm

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The retro is the launch loop's SELF-IMPROVEMENT harness: after a cohort
// runs, it aggregates which channels actually produced signal across products
// and emits deterministic recommendations for the next cohort. The audit is
// its integrity counterpart: anomaly detection over the append-only ledger so
// a sloppy loop (signals before posts, zombie kills) is surfaced, not silent.

// ChannelRetro aggregates one channel's performance across a cohort.
type ChannelRetro struct {
	Channel string  `json:"channel"`
	Posts   int     `json:"posts"`
	Signals int     `json:"signals"`
	Score   float64 `json:"score"` // weighted signal contribution attributed to this channel
}

// CohortRetro is the weekly learning artifact.
type CohortRetro struct {
	Week     string `json:"week"`
	Target   int    `json:"target"`
	Planned  int    `json:"planned"`
	Kits     int    `json:"kits"`
	Posted   int    `json:"posted"`
	Measured int    `json:"measured"`
	Retired  int    `json:"retired"`
	// Distributed counts attempts with at least one placement on a channel that
	// can reach a new audience. It is the honest denominator for the rates
	// below: a tag-only attempt is "posted" but cannot produce a kill or a
	// double-down either way, so including it silently deflates both rates.
	Distributed    int `json:"distributed"`
	NotDistributed int `json:"not_distributed"`
	// Unmeasured counts distributed attempts whose destination could not see
	// arrivals. They are excluded from the rate denominator below for exactly
	// the reason tag-only attempts are: an attempt that cannot produce a kill or
	// a double-down either way silently deflates both rates from the bottom.
	Unmeasured int `json:"unmeasured"`
	// Judged is the rate denominator — distributed attempts we could actually
	// measure. Reported so a low kill rate can never be read as health without
	// the reader seeing how many attempts it was computed over.
	Judged          int                   `json:"judged"`
	Verdicts        map[LaunchVerdict]int `json:"verdicts"`
	KillRate        float64               `json:"kill_rate"`        // kills / judged
	DoubleDownRate  float64               `json:"double_down_rate"` // double-downs / judged
	Channels        []ChannelRetro        `json:"channels"`         // sorted by score desc
	Recommendations []string              `json:"recommendations"`
}

// BuildRetro folds one week's launches into the learning artifact. week ""
// means all weeks combined (an all-time channel leaderboard).
func BuildRetro(launches []ProductLaunch, week string, target int) CohortRetro {
	if target <= 0 {
		target = 20
	}
	r := CohortRetro{Week: week, Target: target, Verdicts: map[LaunchVerdict]int{}}
	chPosts := map[string]int{}
	chSignals := map[string]int{}
	chScore := map[string]float64{}

	for _, p := range launches {
		if week != "" && p.Week != week {
			continue
		}
		r.Planned++
		if p.Disposition.Valid() {
			r.Retired++
		}
		if p.KitAt != "" {
			r.Kits++
		}
		if len(p.Posts) > 0 {
			r.Posted++
			if distributing, _ := distributionPlacements(p.Posts); len(distributing) > 0 {
				r.Distributed++
				if p.Verdict == VerdictUnmeasured {
					r.Unmeasured++
				}
			} else {
				r.NotDistributed++
			}
		}
		if len(p.Signals) > 0 {
			r.Measured++
		}
		r.Verdicts[p.Verdict]++
		for _, post := range p.Posts {
			chPosts[post.Channel]++
		}
		// Attribute weighted signal score per channel (signals are levels —
		// SignalScore already dedupes latest per key within the subset).
		byCh := map[string][]LaunchEvent{}
		for _, s := range p.Signals {
			byCh[s.Channel] = append(byCh[s.Channel], s)
			chSignals[s.Channel]++
		}
		for ch, sigs := range byCh {
			score, _ := SignalScore(sigs)
			chScore[ch] += score
		}
	}
	// Rates divide by attempts that could produce the verdict being rated —
	// neither Posted nor Distributed qualifies on its own. Dividing by Posted
	// would count attempts that never reached anyone, so a portfolio whose only
	// "launch" was a release tag would report a 0% kill rate and read as healthy.
	// Dividing by Distributed reintroduces the same error one step further along
	// the funnel: a launch onto a surface that cannot see arrivals is never
	// KILLed (it verdicts UNMEASURED), so it can only ever deflate the rate.
	// Both are the same false-reading class this surface exists to remove.
	r.Judged = r.Distributed - r.Unmeasured
	if r.Judged > 0 {
		r.KillRate = float64(r.Verdicts[VerdictKill]) / float64(r.Judged)
		r.DoubleDownRate = float64(r.Verdicts[VerdictDoubleDown]) / float64(r.Judged)
	}

	chans := map[string]bool{}
	for ch := range chPosts {
		chans[ch] = true
	}
	for ch := range chScore {
		chans[ch] = true
	}
	for ch := range chans {
		r.Channels = append(r.Channels, ChannelRetro{Channel: ch, Posts: chPosts[ch], Signals: chSignals[ch], Score: chScore[ch]})
	}
	sort.Slice(r.Channels, func(i, j int) bool {
		if r.Channels[i].Score != r.Channels[j].Score {
			return r.Channels[i].Score > r.Channels[j].Score
		}
		return r.Channels[i].Channel < r.Channels[j].Channel
	})

	// Deterministic recommendations — the "what to change next cohort" output.
	if r.Planned < target {
		r.Recommendations = append(r.Recommendations, fmt.Sprintf("cohort filled %d/%d — plan %d more product(s) to hit the weekly target", r.Planned, target, target-r.Planned))
	}
	activeUnposted := r.Planned - r.Posted - r.Retired
	if activeUnposted > 0 {
		r.Recommendations = append(r.Recommendations, fmt.Sprintf("%d active planned product(s) never posted — kits without placement produce no learning", activeUnposted))
	}
	if len(r.Channels) > 0 && r.Channels[0].Score > 0 {
		r.Recommendations = append(r.Recommendations, fmt.Sprintf("top channel: %s (weighted score %.1f) — lean next cohort's placements into it", r.Channels[0].Channel, r.Channels[0].Score))
	}
	for _, c := range r.Channels {
		if c.Posts > 0 && c.Score == 0 {
			r.Recommendations = append(r.Recommendations, fmt.Sprintf("channel %s: %d post(s), zero measured signal — change the angle or drop it", c.Channel, c.Posts))
		}
	}
	if r.Posted > 0 && r.Measured < r.Posted {
		r.Recommendations = append(r.Recommendations, fmt.Sprintf("%d posted product(s) unmeasured — run `launch signals <p> --record` so verdicts are evidence, not vibes", r.Posted-r.Measured))
	}
	if r.NotDistributed > 0 {
		r.Recommendations = append(r.Recommendations, fmt.Sprintf(
			"%d attempt(s) placed only on non-distribution channels — a release tag is not a launch; place them on a real channel before judging demand", r.NotDistributed))
	}
	if r.Unmeasured > 0 {
		r.Recommendations = append(r.Recommendations, fmt.Sprintf(
			"%d distributed attempt(s) landed on a surface that cannot see arrivals — the rates below are computed without them; "+
				"fix with `ndev endpoints analytics --blind`, then re-run this retro", r.Unmeasured))
	}
	return r
}

// Markdown renders the retro for terminals/docs.
func (r CohortRetro) Markdown() string {
	var b strings.Builder
	scope := r.Week
	if scope == "" {
		scope = "all weeks"
	}
	fmt.Fprintf(&b, "# Launch retro — %s\n\n", scope)
	fmt.Fprintf(&b, "planned %d/%d · retired %d · kits %d · posted %d (distributed %d · judged %d) · measured %d · double-down rate %.0f%% · kill rate %.0f%%\n\n",
		r.Planned, r.Target, r.Retired, r.Kits, r.Posted, r.Distributed, r.Judged, r.Measured, r.DoubleDownRate*100, r.KillRate*100)
	if r.NotDistributed > 0 {
		fmt.Fprintf(&b, "> %d attempt(s) placed only on non-distribution channels (e.g. a release tag). "+
			"They are excluded from the rate denominators — they cannot produce a kill or a double-down either way.\n\n",
			r.NotDistributed)
	}
	if r.Unmeasured > 0 {
		fmt.Fprintf(&b, "> %d distributed attempt(s) landed on a surface that cannot see arrivals (verdict UNMEASURED). "+
			"They are excluded from the rate denominators for the same reason: with no instrumentation they cannot "+
			"produce a kill or a double-down, so counting them would deflate both rates and read as health.\n\n",
			r.Unmeasured)
	}
	if len(r.Channels) > 0 {
		b.WriteString("| channel | posts | signals | weighted score |\n|---|---|---|---|\n")
		for _, c := range r.Channels {
			fmt.Fprintf(&b, "| %s | %d | %d | %.1f |\n", c.Channel, c.Posts, c.Signals, c.Score)
		}
		b.WriteString("\n")
	}
	if len(r.Recommendations) > 0 {
		b.WriteString("Next cohort:\n")
		for _, rec := range r.Recommendations {
			fmt.Fprintf(&b, "- %s\n", rec)
		}
	}
	return b.String()
}

// dedupeStrings preserves first-seen order so anomaly messages are stable
// across runs (audit output is diffed by the strict gate).
func dedupeStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// LaunchAnomaly is one audit finding over the ledger.
type LaunchAnomaly struct {
	Product string `json:"product"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
}

// AuditLaunchLedgerRead combines semantic launch-loop anomalies with physical
// ledger corruption. Audit is the only tolerant reader; all normal workflows
// fail closed through LaunchLedger.Read.
func AuditLaunchLedgerRead(report LaunchLedgerRead, now time.Time) []LaunchAnomaly {
	out := make([]LaunchAnomaly, 0, len(report.Issues))
	for _, issue := range report.Issues {
		out = append(out, LaunchAnomaly{
			Product: issue.Product,
			Code:    issue.Code,
			Message: issue.Message,
			Line:    issue.Line,
		})
	}
	out = append(out, AuditLaunchLedger(report.Events, now)...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Product != out[j].Product {
			return out[i].Product < out[j].Product
		}
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// AuditLaunchLedger runs integrity checks over raw ledger events:
//
//	posted_before_plan         — a placement with no prior planned event
//	kit_before_plan            — a generated kit with no prior planned event
//	signal_before_post         — a signal recorded before any placement
//	verdict_before_plan        — a verdict with no prior planned event
//	stale_plan                 — planned >7 days ago, still no placement
//	zombie_after_kill          — activity recorded after a KILL verdict snapshot
//	retired_before_plan        — retirement without a prior plan
//	retirement_after_post      — an executed launch was incorrectly retired
//	retirement_week_mismatch   — retirement does not target the active attempt
//	activity_after_retirement  — activity without an intervening re-plan
//	not_distributed            — placed only on channels that reach no new audience
func AuditLaunchLedger(events []LaunchEvent, now time.Time) []LaunchAnomaly {
	var out []LaunchAnomaly
	planned := map[string]bool{}
	posted := map[string]bool{}
	killed := map[string]bool{}
	retired := map[string]bool{}
	plannedWeek := map[string]string{}
	plannedAt := map[string]time.Time{}
	// placements tracks real EventPosted channels per attempt. It is deliberately
	// separate from `posted`, which the signal_before_post branch also sets in
	// order to report once per product — reusing it here would invent placements
	// for a product that only ever recorded a stray signal.
	placements := map[string][]string{}
	distributed := map[string]bool{}

	for _, ev := range events {
		switch ev.Type {
		case EventPlanned:
			planned[ev.Product] = true
			posted[ev.Product] = false
			killed[ev.Product] = false // a re-plan revives the product
			retired[ev.Product] = false
			placements[ev.Product] = nil
			distributed[ev.Product] = false
			plannedWeek[ev.Product] = ev.Week
			if t, err := time.Parse(time.RFC3339, ev.TS); err == nil {
				plannedAt[ev.Product] = t
			}
		case EventPosted:
			if !planned[ev.Product] {
				out = append(out, LaunchAnomaly{Product: ev.Product, Code: "posted_before_plan",
					Message: fmt.Sprintf("posted on %s without a planned event — cohort metrics will undercount", ev.Channel)})
			}
			if killed[ev.Product] {
				out = append(out, LaunchAnomaly{Product: ev.Product, Code: "zombie_after_kill",
					Message: fmt.Sprintf("posted on %s after a KILL verdict — re-plan it into a new cohort instead", ev.Channel)})
			}
			if retired[ev.Product] {
				out = append(out, LaunchAnomaly{Product: ev.Product, Code: "activity_after_retirement",
					Message: fmt.Sprintf("posted on %s after retirement — re-plan it into a new cohort first", ev.Channel)})
			}
			posted[ev.Product] = true
			placements[ev.Product] = append(placements[ev.Product], ev.Channel)
			if ChannelDistributes(ev.Channel) {
				distributed[ev.Product] = true
			}
		case EventSignal:
			if killed[ev.Product] {
				out = append(out, LaunchAnomaly{Product: ev.Product, Code: "zombie_after_kill",
					Message: fmt.Sprintf("signal %s recorded after a KILL verdict — re-plan it into a new cohort first", ev.Metric)})
			}
			if retired[ev.Product] {
				out = append(out, LaunchAnomaly{Product: ev.Product, Code: "activity_after_retirement",
					Message: fmt.Sprintf("signal %s recorded after retirement — re-plan it before new activity", ev.Metric)})
			}
			if !posted[ev.Product] {
				out = append(out, LaunchAnomaly{Product: ev.Product, Code: "signal_before_post",
					Message: fmt.Sprintf("signal %s recorded before any placement — baseline noise will pollute the verdict", ev.Metric)})
				posted[ev.Product] = true // report once per product
			}
		case EventVerdict:
			if !planned[ev.Product] {
				out = append(out, LaunchAnomaly{Product: ev.Product, Code: "verdict_before_plan",
					Message: "verdict recorded without a planned event"})
			}
			if killed[ev.Product] && ev.Verdict != VerdictKill {
				out = append(out, LaunchAnomaly{Product: ev.Product, Code: "zombie_after_kill",
					Message: fmt.Sprintf("verdict %s recorded after a KILL verdict — re-plan it into a new cohort first", ev.Verdict)})
			}
			if retired[ev.Product] {
				out = append(out, LaunchAnomaly{Product: ev.Product, Code: "activity_after_retirement",
					Message: "verdict recorded after retirement — re-plan it before a new decision"})
			}
			if ev.Verdict == VerdictKill || strings.EqualFold(ev.Note, string(VerdictKill)) {
				killed[ev.Product] = true
			}
		case EventKit:
			if !planned[ev.Product] {
				out = append(out, LaunchAnomaly{Product: ev.Product, Code: "kit_before_plan",
					Message: "kit recorded without a planned event"})
			}
			if killed[ev.Product] {
				out = append(out, LaunchAnomaly{Product: ev.Product, Code: "zombie_after_kill",
					Message: "kit recorded after a KILL verdict — re-plan it into a new cohort first"})
			}
			if retired[ev.Product] {
				out = append(out, LaunchAnomaly{Product: ev.Product, Code: "activity_after_retirement",
					Message: "kit recorded after retirement — re-plan it into a new cohort first"})
			}
		case EventRetired:
			if !planned[ev.Product] {
				out = append(out, LaunchAnomaly{Product: ev.Product, Code: "retired_before_plan",
					Message: "retirement has no prior planned event"})
			}
			if activeWeek := plannedWeek[ev.Product]; activeWeek != "" && ev.Week != activeWeek {
				out = append(out, LaunchAnomaly{Product: ev.Product, Code: "retirement_week_mismatch",
					Message: fmt.Sprintf("retired week %s but active attempt is %s", ev.Week, activeWeek)})
			}
			if posted[ev.Product] {
				out = append(out, LaunchAnomaly{Product: ev.Product, Code: "retirement_after_post",
					Message: "executed launch was retired after a placement — persist a verdict instead"})
			}
			if retired[ev.Product] {
				out = append(out, LaunchAnomaly{Product: ev.Product, Code: "duplicate_retirement",
					Message: "launch attempt was retired more than once"})
			}
			retired[ev.Product] = true
		}
	}
	for p, t := range plannedAt {
		if !posted[p] && !retired[p] && now.Sub(t).Hours() > 24*7 {
			out = append(out, LaunchAnomaly{Product: p, Code: "stale_plan",
				Message: fmt.Sprintf("planned %s, never posted — the cohort slot is dead weight", t.Format("2006-01-02"))})
		}
	}
	// A launch placed only on non-distribution channels reads as an executed
	// launch everywhere downstream (cohort stage "posted", kill-window eligible)
	// while having reached nobody. Surface it as a ledger finding so the operator
	// sees it before the verdict engine has to explain it.
	for p, channels := range placements {
		if len(channels) == 0 || distributed[p] || retired[p] {
			continue
		}
		out = append(out, LaunchAnomaly{Product: p, Code: "not_distributed",
			Message: fmt.Sprintf("%d placement(s) on %s only — no distribution channel reached; the attempt cannot produce demand information",
				len(channels), strings.Join(dedupeStrings(channels), ", "))})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Product != out[j].Product {
			return out[i].Product < out[j].Product
		}
		return out[i].Code < out[j].Code
	})
	return out
}
