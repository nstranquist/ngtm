package gtm

import (
	"fmt"
	"math"
	"strings"
)

// Shared display helpers for the model-driven verticals (economics / pricing /
// motion). Kept dependency-free so the pure models stay easy to unit-test.

func fmtMoney(v float64) string {
	if math.IsNaN(v) {
		return "n/a"
	}
	switch {
	case v >= 1_000_000_000:
		return fmt.Sprintf("$%.1fB", v/1_000_000_000)
	case v >= 1_000_000:
		return fmt.Sprintf("$%.1fM", v/1_000_000)
	case v >= 1_000:
		return fmt.Sprintf("$%.1fK", v/1_000)
	default:
		return fmt.Sprintf("$%.0f", v)
	}
}

func fmtMoneyExact(v float64) string { return fmt.Sprintf("$%.2f", v) }

// mathIsFinite reports whether v is a real, JSON-encodable number (not Inf/NaN).
func mathIsFinite(v float64) bool { return !math.IsInf(v, 0) && !math.IsNaN(v) }

// fmtPct renders a 0..1 fraction as a percentage.
func fmtPct(frac float64) string {
	if math.IsNaN(frac) {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", frac*100)
}

func fmtRatio(v float64) string {
	if math.IsNaN(v) {
		return "n/a"
	}
	if math.IsInf(v, 1) {
		return "∞"
	}
	return fmt.Sprintf("%.2f×", v)
}

func fmtMonths(v float64) string {
	if math.IsNaN(v) {
		return "n/a"
	}
	if v == 0 {
		return "at sale (one-time)"
	}
	if math.IsInf(v, 1) || v > 1200 {
		return "∞ (never, at this churn)"
	}
	return fmt.Sprintf("%.1f mo", v)
}

func mdTable(headers []string, rows [][]string) string {
	var b strings.Builder
	b.WriteString("| " + strings.Join(headers, " | ") + " |\n")
	seps := make([]string, len(headers))
	for i := range seps {
		seps[i] = "---"
	}
	b.WriteString("| " + strings.Join(seps, " | ") + " |\n")
	for _, r := range rows {
		b.WriteString("| " + strings.Join(r, " | ") + " |\n")
	}
	return b.String()
}
