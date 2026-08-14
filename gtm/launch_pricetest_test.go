package gtm

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validPriceTest() LaunchEvent {
	return LaunchEvent{
		TS: "2026-07-25T10:00:00Z", Product: "cadence", Type: EventPriceTest,
		Channel: "show-hn", Price: 39, Currency: "USD",
		Offer: "one-time license, lifetime updates", Response: ResponseDeclined,
		Source: SourceOperator,
	}
}

func TestPriceTestEvent_ValidateAcceptsWellFormed(t *testing.T) {
	if err := validPriceTest().Validate(); err != nil {
		t.Fatalf("valid price test rejected: %v", err)
	}
}

func TestPriceTestEvent_ValidateRejectsMalformed(t *testing.T) {
	cases := map[string]func(*LaunchEvent){
		"zero price":         func(e *LaunchEvent) { e.Price = 0 },
		"negative price":     func(e *LaunchEvent) { e.Price = -1 },
		"lowercase currency": func(e *LaunchEvent) { e.Currency = "usd" },
		"long currency":      func(e *LaunchEvent) { e.Currency = "DOLLARS" },
		"empty currency":     func(e *LaunchEvent) { e.Currency = "" },
		"unknown response":   func(e *LaunchEvent) { e.Response = "maybe" },
		"empty response":     func(e *LaunchEvent) { e.Response = "" },
		"missing channel":    func(e *LaunchEvent) { e.Channel = "" },
		"unsafe channel":     func(e *LaunchEvent) { e.Channel = "Show HN" },
		"non-operator":       func(e *LaunchEvent) { e.Source = SourceHackerNews },
		"multiline offer":    func(e *LaunchEvent) { e.Offer = "line one\nline two" },
		"untrimmed offer":    func(e *LaunchEvent) { e.Offer = " padded " },
	}
	for name, mutate := range cases {
		ev := validPriceTest()
		mutate(&ev)
		if err := ev.Validate(); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}

// The load-bearing detail of the slice: rejectUnexpectedFields iterates a fixed
// list, so a new field that is not added to it would be silently accepted on
// every other event type. This test fails loudly if that list drifts.
func TestPriceTestFields_RejectedOnEveryOtherEventType(t *testing.T) {
	base := map[LaunchEventType]LaunchEvent{
		EventPlanned: {TS: "2026-07-25T10:00:00Z", Product: "p", Type: EventPlanned, Week: "2026-W30"},
		EventKit:     {TS: "2026-07-25T10:00:00Z", Product: "p", Type: EventKit},
		EventPosted:  {TS: "2026-07-25T10:00:00Z", Product: "p", Type: EventPosted, Channel: "show-hn", URL: "https://example.com/x"},
		EventSignal:  {TS: "2026-07-25T10:00:00Z", Product: "p", Type: EventSignal, Metric: MetricClicks, Value: 3, Source: SourceOperator},
		EventVerdict: {TS: "2026-07-25T10:00:00Z", Product: "p", Type: EventVerdict, Verdict: VerdictWatch},
		EventRetired: {TS: "2026-07-25T10:00:00Z", Product: "p", Type: EventRetired, Week: "2026-W30", Disposition: DispositionCancelled, Reason: "slot withdrawn", Source: SourceOperator},
	}
	contaminate := map[string]func(*LaunchEvent){
		"price":    func(e *LaunchEvent) { e.Price = 39 },
		"currency": func(e *LaunchEvent) { e.Currency = "USD" },
		"offer":    func(e *LaunchEvent) { e.Offer = "a deal" },
		"response": func(e *LaunchEvent) { e.Response = ResponseAccepted },
	}
	for evType, template := range base {
		if err := template.Validate(); err != nil {
			t.Fatalf("baseline %s event is invalid, test is wrong: %v", evType, err)
		}
		for field, mutate := range contaminate {
			ev := template
			mutate(&ev)
			err := ev.Validate()
			if err == nil {
				t.Errorf("%s event must reject a %s field", evType, field)
				continue
			}
			if !strings.Contains(err.Error(), field) {
				t.Errorf("%s event rejection must name %q, got %v", evType, field, err)
			}
		}
	}
}

// A price test must never be scored as traction. It is demand evidence about
// willingness to pay, not a community signal, and folding it into SignalScore
// would let an offer nobody accepted inflate a verdict.
func TestPriceTest_DoesNotContributeToSignalScore(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	events := []LaunchEvent{
		{TS: "2026-07-20T09:00:00Z", Product: "cadence", Type: EventPlanned, Week: "2026-W30"},
		{TS: "2026-07-20T10:00:00Z", Product: "cadence", Type: EventPosted, Channel: "show-hn", URL: "https://example.com/x"},
		validPriceTestFor("cadence", 39, ResponseAccepted),
	}
	launches := BuildLaunches(events, now)
	if len(launches) != 1 {
		t.Fatalf("expected one launch, got %d", len(launches))
	}
	got := launches[0]
	if got.Score != 0 {
		t.Errorf("price tests must not contribute to the traction score, got %v", got.Score)
	}
	if !got.PriceTested {
		t.Error("PriceTested must be true once a test is recorded")
	}
	if len(got.PriceTests) != 1 {
		t.Errorf("expected the price test to be folded in, got %d", len(got.PriceTests))
	}
	if got.Verdict == VerdictDoubleDown {
		t.Error("an accepted price test is not community traction and must not trigger DOUBLE-DOWN on its own")
	}
}

func TestPriceTested_FalseWithoutAnyTest(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	events := []LaunchEvent{
		{TS: "2026-07-20T09:00:00Z", Product: "echo", Type: EventPlanned, Week: "2026-W30"},
	}
	launches := BuildLaunches(events, now)
	if len(launches) != 1 || launches[0].PriceTested {
		t.Fatalf("a product with no price test must report price_tested=false, got %+v", launches)
	}
}

func TestPriceTest_LedgerRoundTrip(t *testing.T) {
	led := LaunchLedger{Path: filepath.Join(t.TempDir(), "ledger.jsonl")}
	if err := led.Append(LaunchEvent{TS: "2026-07-25T09:00:00Z", Product: "cadence", Type: EventPlanned, Week: "2026-W30"}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	want := validPriceTest()
	if err := led.Append(want); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, err := led.Read()
	if err != nil || len(got) != 2 {
		t.Fatalf("read: %v %v", got, err)
	}
	got = got[1:]
	if got[0].Price != want.Price || got[0].Currency != want.Currency ||
		got[0].Response != want.Response || got[0].Offer != want.Offer {
		t.Fatalf("price test did not round-trip: %+v", got[0])
	}
}

func TestParsePriceTestResponse(t *testing.T) {
	for _, raw := range []string{"accepted", "ACCEPTED", " declined ", "countered", "no_response", "deferred"} {
		if _, err := ParsePriceTestResponse(raw); err != nil {
			t.Errorf("%q must parse: %v", raw, err)
		}
	}
	for _, raw := range []string{"", "maybe", "no response", "yes"} {
		if _, err := ParsePriceTestResponse(raw); err == nil {
			t.Errorf("%q must be rejected", raw)
		}
	}
}

func validPriceTestFor(product string, price float64, response PriceTestResponse) LaunchEvent {
	ev := validPriceTest()
	ev.Product = product
	ev.Price = price
	ev.Response = response
	return ev
}
