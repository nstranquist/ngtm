package gtm

import (
	"fmt"
	"strings"
)

// ChannelSpec encodes one distribution channel's posting contract as typed
// data: hard format limits, the community's tolerance norms, and the best
// posting slot. The social vertical's generator (offline template or LLM) must
// obey the spec, and lintDraft checks every draft against it — placement
// knowledge lives here, not in prompt vibes, so it is testable and versioned.
type ChannelSpec struct {
	Key       string   // stable id, e.g. "show-hn"
	Label     string   // human label
	Kind      string   // "launch" (one-shot placement) | "social" (recurring)
	TitleMax  int      // hard title/hook character limit (0 = no title)
	BodyMax   int      // soft body character ceiling (0 = unlimited)
	TitleRule string   // mechanical title constraint, checked by lintDraft
	Norms     []string // community norms the draft must respect
	BestSlot  string   // empirically-best posting window
}

// bannedSuperlatives are marketing phrases that read as spam on builder
// channels (HN/Reddit especially). lintDraft flags any draft containing one.
var bannedSuperlatives = []string{
	"revolutionary", "game-changing", "game changer", "best-in-class",
	"world-class", "cutting-edge", "next-generation", "disruptive",
	"unparalleled", "groundbreaking",
}

// Channels is the typed distribution-channel registry, ordered by launch
// priority. Keys are stable; CLI/MCP callers select subsets by key.
var Channels = []ChannelSpec{
	{
		Key: "show-hn", Label: "Show HN", Kind: "launch",
		// BodyMax was 1200 at v1; the first live LLM kit run (2026-06-10) showed
		// every well-formed long-form draft tripping it while real Show HN text
		// posts routinely run 1500-2500 chars — rubric-driven recalibration.
		TitleMax: 80, BodyMax: 2500,
		TitleRule: "must start with \"Show HN: \"",
		Norms: []string{
			"first-person builder voice; lead with what it does and how it works",
			"technical substance over benefit language; no marketing superlatives",
			"text post: link plus a short story of why you built it",
			"be present in the comments for the first 3 hours",
		},
		BestSlot: "Tue-Thu 08:00-10:00 ET",
	},
	{
		Key: "producthunt", Label: "Product Hunt", Kind: "launch",
		TitleMax: 60, BodyMax: 2000,
		TitleRule: "tagline <= 60 chars, no trailing period",
		Norms: []string{
			"tagline states the outcome, not the category",
			"maker first-comment tells the origin story and asks for feedback",
			"line up 3-5 genuine supporters for the first hour; never buy upvotes",
		},
		BestSlot: "Tue-Thu 00:01 PT",
	},
	{
		Key: "reddit", Label: "Reddit (target subreddit)", Kind: "launch",
		TitleMax: 300, BodyMax: 3000,
		TitleRule: "no clickbait; describe the post's actual content",
		Norms: []string{
			"value-first: lead with the problem and what you learned, not the product",
			"respect the 9:1 rule — this account must not be promo-only",
			"check the subreddit's self-promo policy before posting; some require a 'Saturday thread'",
			"put the link at the end or in a comment, per sub norms",
		},
		BestSlot: "weekday mornings local to the sub's dominant timezone",
	},
	{
		Key: "x", Label: "X / Twitter thread", Kind: "social",
		TitleMax: 280, BodyMax: 2200,
		TitleRule: "hook tweet <= 280 chars and must create an open loop",
		Norms: []string{
			"5-8 tweets; one idea per tweet",
			"hook states the concrete outcome or surprising fact, never 'I made a thing'",
			"exactly one CTA, in the final tweet",
			"native screenshots/demo video outperform links; put the link in the last tweet",
		},
		BestSlot: "Tue-Thu 09:00-11:00 local",
	},
	{
		Key: "linkedin", Label: "LinkedIn post", Kind: "social",
		TitleMax: 0, BodyMax: 1300,
		Norms: []string{
			"story format: problem -> attempt -> insight -> product as the resolution",
			"short lines, white space; first 2 lines must work before the fold",
			"no external link in the body — put it in the first comment",
		},
		BestSlot: "Tue-Wed 08:00-10:00 local",
	},
	{
		Key: "indiehackers", Label: "Indie Hackers", Kind: "social",
		TitleMax: 100, BodyMax: 3000,
		TitleRule: "milestone or lesson framing, not an ad",
		Norms: []string{
			"share real numbers (users, revenue, conversion) — IH rewards transparency",
			"end with a genuine question for the community",
		},
		BestSlot: "weekday mornings ET",
	},
}

// ChannelByKey resolves a channel spec by key (case-insensitive).
func ChannelByKey(key string) (ChannelSpec, bool) {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, c := range Channels {
		if c.Key == k {
			return c, true
		}
	}
	return ChannelSpec{}, false
}

// SelectChannels resolves a key list to specs, defaulting to all channels.
// Unknown keys are reported, not silently dropped.
func SelectChannels(keys []string) ([]ChannelSpec, []string) {
	if len(keys) == 0 {
		return Channels, nil
	}
	var out []ChannelSpec
	var unknown []string
	for _, k := range keys {
		if c, ok := ChannelByKey(k); ok {
			out = append(out, c)
		} else if strings.TrimSpace(k) != "" {
			unknown = append(unknown, k)
		}
	}
	return out, unknown
}

// NonDistributionChannel is a recognized placement target that produces a real
// receipt but does not put the product in front of anyone who did not already
// know it exists. Recording one is legitimate — it is evidence the artifact
// shipped — but it must never be scored as a distribution attempt, because a
// launch that reached nobody cannot produce demand information either way.
//
// The registry is deliberately small and proves only the negative: a channel
// key absent from it is treated as distribution-bearing. See ChannelDistributes.
type NonDistributionChannel struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

// nonDistributionChannels holds the placement targets we are confident do not
// reach a new audience. Each entry states why, because the verdict rationale
// and the audit anomaly both quote it back to the operator.
var nonDistributionChannels = []NonDistributionChannel{
	{
		Key: "github-release", Label: "GitHub release tag",
		Reason: "a release tag notifies existing watchers only — it reaches nobody who is not already following the repo",
	},
	{
		Key: "changelog", Label: "Project changelog",
		Reason: "a changelog entry is read by people who already use the product",
	},
	{
		Key: "internal", Label: "Internal / self announcement",
		Reason: "an internal announcement has no external audience by construction",
	},
}

// NonDistributionChannelByKey resolves a non-distribution placement target.
func NonDistributionChannelByKey(key string) (NonDistributionChannel, bool) {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, c := range nonDistributionChannels {
		if c.Key == k {
			return c, true
		}
	}
	return NonDistributionChannel{}, false
}

// NonDistributionChannels returns the registry in display order.
func NonDistributionChannels() []NonDistributionChannel {
	return append([]NonDistributionChannel(nil), nonDistributionChannels...)
}

// ChannelDistributes reports whether a placement on this channel could plausibly
// reach a new audience.
//
// It fails OPEN: an unregistered key is distribution-bearing. The registry can
// prove a channel does not distribute; it can never assume it. Downgrading an
// operator's real launch because we had not registered their channel yet would
// be a worse error than scoring an obscure channel generously.
func ChannelDistributes(key string) bool {
	_, nonDistributing := NonDistributionChannelByKey(key)
	return !nonDistributing
}

// ChannelDraft is one channel-native content draft.
type ChannelDraft struct {
	Channel string `json:"channel"`
	Title   string `json:"title,omitempty"`
	Body    string `json:"body"`
}

// lintDraft checks a draft against its channel spec and returns human-readable
// violations (empty == clean). This is the self-eval harness of the content
// factory: a draft that breaks the channel's contract is flagged before a
// human ever pastes it.
func lintDraft(spec ChannelSpec, d ChannelDraft) []string {
	var v []string
	if spec.TitleMax > 0 && len([]rune(d.Title)) > spec.TitleMax {
		v = append(v, fmt.Sprintf("%s: title %d chars exceeds limit %d", spec.Key, len([]rune(d.Title)), spec.TitleMax))
	}
	if spec.BodyMax > 0 && len([]rune(d.Body)) > spec.BodyMax {
		v = append(v, fmt.Sprintf("%s: body %d chars exceeds ceiling %d", spec.Key, len([]rune(d.Body)), spec.BodyMax))
	}
	if spec.Key == "show-hn" && d.Title != "" && !strings.HasPrefix(d.Title, "Show HN: ") {
		v = append(v, "show-hn: title must start with \"Show HN: \"")
	}
	lower := strings.ToLower(d.Title + " " + d.Body)
	for _, banned := range bannedSuperlatives {
		if strings.Contains(lower, banned) {
			v = append(v, fmt.Sprintf("%s: contains marketing superlative %q", spec.Key, banned))
		}
	}
	return v
}
