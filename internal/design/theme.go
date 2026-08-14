package design

// Mode is a color scheme polarity.
type Mode string

const (
	ModeDark  Mode = "dark"
	ModeLight Mode = "light"
)

// HarmonyStrategy is the color-wheel relationship used to derive the accent
// hue from the primary (brand) hue. These are the classic color-theory schemes.
type HarmonyStrategy string

const (
	HarmonyMono          HarmonyStrategy = "monochromatic"    // same hue, vary L/C
	HarmonyAnalogous     HarmonyStrategy = "analogous"        // neighbor hue (+30°)
	HarmonyComplementary HarmonyStrategy = "complementary"    // opposite hue (+180°)
	HarmonySplit         HarmonyStrategy = "split-complement" // +150° (calmer than full complement)
	HarmonyTriadic       HarmonyStrategy = "triadic"          // +120° (vivid, balanced)
)

// allHarmonies is the search space for the tuning loop.
var allHarmonies = []HarmonyStrategy{
	HarmonyMono, HarmonyAnalogous, HarmonyComplementary, HarmonySplit, HarmonyTriadic,
}

// accentHueOffset returns the degrees to rotate the primary hue for the accent.
func (h HarmonyStrategy) accentHueOffset() float64 {
	switch h {
	case HarmonyAnalogous:
		return 30
	case HarmonyComplementary:
		return 180
	case HarmonySplit:
		return 150
	case HarmonyTriadic:
		return 120
	default: // mono
		return 0
	}
}

// Palette is the semantic color-token set for one mode. Every field is an
// "#rrggbb" string ready to drop into CSS custom properties.
type Palette struct {
	Mode      Mode
	BG        string // page background
	Surface   string // raised panel/card
	Surface2  string // higher elevation
	Border    string // hairline separators, input borders
	FG        string // primary text
	Muted     string // secondary text
	Primary   string // brand action color (buttons, links)
	PrimaryFg string // text/icon on Primary
	Accent    string // secondary highlight
	AccentFg  string // text on Accent
	Success   string // positive state
	Warning   string // caution state
	Danger    string // destructive/error state
	Info      string // neutral-informative state
	Code      string // code surface background
}

// Theme is a complete, self-contained design system: the seed, the scheme,
// one or two palettes (dark always present; light optional), and the
// type/spacing/shape scales.
type Theme struct {
	Name     string
	Seed     OKLCH // brand primary in OKLCH (the generative root)
	Harmony  HarmonyStrategy
	Dark     Palette
	Light    *Palette // optional companion light palette
	Type     TypeScale
	Spacing  SpacingScale
	Radius   float64 // base corner radius, px
	FontSans string
	FontMono string
}

const (
	defaultSans = `-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif`
	defaultMono = `ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace`
)
