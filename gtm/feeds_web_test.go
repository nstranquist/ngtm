package gtm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTavilyFeed_ParsesResults(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Write([]byte(`{"results":[
			{"title":"Devin","url":"https://devin.ai","content":"AI software engineer"},
			{"title":"Factory","url":"https://factory.ai","content":"agent-native dev"}
		]}`))
	}))
	defer srv.Close()
	t.Setenv("TAVILY_API_KEY", "tvly-test")
	t.Setenv("TAVILY_API_URL", srv.URL)

	f := &tavilyFeed{now: fixedNow}
	if !f.Available() {
		t.Fatal("expected Available() with TAVILY_API_KEY set")
	}
	ev, err := f.Query(context.Background(), FeedQuery{Subject: "agent control plane", Keywords: []string{"autonomous coding agents"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(ev) != 2 {
		t.Fatalf("evidence = %d, want 2", len(ev))
	}
	if ev[0].Feed != "tavily" || ev[0].Tier != TierCheap || ev[0].Title != "Devin" || ev[0].URL != "https://devin.ai" {
		t.Errorf("ev[0] = %+v", ev[0])
	}
	if ev[0].Metric != "serp_rank" || ev[0].Value != "1" || ev[1].Value != "2" {
		t.Errorf("ranks wrong: %q %q", ev[0].Value, ev[1].Value)
	}
	if ev[0].Synthetic {
		t.Error("tavily evidence must not be synthetic")
	}
	// Keywords win over Subject for the query body.
	if gotBody["query"] != "autonomous coding agents" {
		t.Errorf("query = %v, want keywords", gotBody["query"])
	}
	if gotBody["api_key"] != "tvly-test" {
		t.Errorf("api_key not sent: %v", gotBody["api_key"])
	}
}

func TestTavilyFeed_Unavailable(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "")
	f := &tavilyFeed{now: fixedNow}
	if f.Available() {
		t.Error("expected unavailable without key")
	}
	if _, err := f.Query(context.Background(), FeedQuery{Subject: "x"}); err == nil {
		t.Error("expected error without key")
	}
}

func TestSearxngFeed_ParsesResults(t *testing.T) {
	var gotQuery, gotFormat string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/search") {
			t.Errorf("path = %s, want /search", r.URL.Path)
		}
		gotQuery = r.URL.Query().Get("q")
		gotFormat = r.URL.Query().Get("format")
		w.Write([]byte(`{"results":[
			{"title":"Sweep","url":"https://sweep.dev","content":"AI dev","engine":"google"},
			{"title":"Cognition","url":"https://cognition.ai","content":"makers of Devin","engine":"bing"}
		]}`))
	}))
	defer srv.Close()
	t.Setenv("SEARXNG_URL", srv.URL+"/") // trailing slash should be trimmed

	f := &searxngFeed{now: fixedNow}
	if !f.Available() {
		t.Fatal("expected Available() with SEARXNG_URL set")
	}
	if f.Tier() != TierFree {
		t.Errorf("searxng tier = %v, want free (self-hosted, no per-call cost)", f.Tier())
	}
	ev, err := f.Query(context.Background(), FeedQuery{Subject: "Devin alternative"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(ev) != 2 || ev[0].Feed != "searxng" || ev[0].Tier != TierFree {
		t.Fatalf("evidence = %+v", ev)
	}
	if ev[0].Title != "Sweep" || ev[0].Value != "1" || ev[0].Extra["engine"] != "google" {
		t.Errorf("ev[0] = %+v", ev[0])
	}
	if gotQuery != "Devin alternative" || gotFormat != "json" {
		t.Errorf("query=%q format=%q", gotQuery, gotFormat)
	}
}

func TestSearxngFeed_Unavailable(t *testing.T) {
	t.Setenv("SEARXNG_URL", "")
	f := &searxngFeed{now: fixedNow}
	if f.Available() {
		t.Error("expected unavailable without SEARXNG_URL")
	}
}

func TestRegistry_RegistersSearchFeeds(t *testing.T) {
	reg := NewFeedRegistry(fixedNow)
	names := map[string]FeedTier{}
	for _, f := range reg.Feeds() {
		names[f.Name()] = f.Tier()
	}
	if tier, ok := names["tavily"]; !ok || tier != TierCheap {
		t.Errorf("tavily not registered as cheap: %v %v", tier, ok)
	}
	if tier, ok := names["searxng"]; !ok || tier != TierFree {
		t.Errorf("searxng not registered as free: %v %v", tier, ok)
	}

	// SearXNG joins the FREE tier once configured (the owned, zero-cost path).
	t.Setenv("SEARXNG_URL", "http://localhost:8888")
	freeSel := reg.Selectable(map[FeedTier]bool{TierFree: true})
	found := false
	for _, f := range freeSel {
		if f.Name() == "searxng" {
			found = true
		}
	}
	if !found {
		t.Error("searxng should be selectable on the free tier when SEARXNG_URL is set")
	}

	// Tavily joins the CHEAP tier once the key is present.
	t.Setenv("TAVILY_API_KEY", "tvly-x")
	cheapSel := reg.Selectable(map[FeedTier]bool{TierCheap: true})
	found = false
	for _, f := range cheapSel {
		if f.Name() == "tavily" {
			found = true
		}
	}
	if !found {
		t.Error("tavily should be selectable on the cheap tier when TAVILY_API_KEY is set")
	}
}
