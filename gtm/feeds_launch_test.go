package gtm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const showHNFixture = `{
  "hits": [
    {
      "title": "Show HN: Radarbox",
      "url": "https://github.com/acme/radarbox",
      "points": 42,
      "num_comments": 11,
      "objectID": "999001"
    },
    {
      "title": "Show HN: Paperclip",
      "url": "https://paperclip.example",
      "story_text": "Source is at https://github.com/paper/clip",
      "points": 7,
      "num_comments": 2,
      "objectID": "999002"
    }
  ]
}`

const productHuntAtomFixture = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>tag:www.producthunt.com,2005:Post/1</id>
    <title>Whisperstream</title>
    <link rel="alternate" href="https://www.producthunt.com/products/whisperstream"/>
    <author><name>D Jackson</name></author>
    <published>2026-06-03T08:11:43-07:00</published>
    <updated>2026-08-18T13:16:18-07:00</updated>
    <content type="html"><![CDATA[<p>Local AI dictation for Windows</p><p><a href="https://www.producthunt.com/r/p/1?app_id=339">Link</a></p>]]></content>
  </entry>
  <entry>
    <id>tag:www.producthunt.com,2005:Post/2</id>
    <title>OpenTrade</title>
    <link rel="alternate" href="https://www.producthunt.com/products/opentrade"/>
    <content type="html"><p>Open-source trading harness. https://github.com/opentrade/opentrade</p></content>
  </entry>
</feed>`

func TestParseShowHNBrowse_ExtractsLaunchItems(t *testing.T) {
	ev, err := ParseShowHNBrowse([]byte(showHNFixture), "2026-08-18T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 2 {
		t.Fatalf("len=%d want 2", len(ev))
	}
	if ev[0].Title != "Show HN: Radarbox" || ev[0].Metric != metricLaunch || ev[0].Value != "42" {
		t.Fatalf("first = %+v", ev[0])
	}
	if ev[0].URL != "https://news.ycombinator.com/item?id=999001" {
		t.Fatalf("discussion = %q", ev[0].URL)
	}
	if ev[0].Extra["github_repo"] != "acme/radarbox" {
		t.Fatalf("github = %q", ev[0].Extra["github_repo"])
	}
	if ev[1].Extra["github_repo"] != "paper/clip" {
		t.Fatalf("story_text github = %q", ev[1].Extra["github_repo"])
	}
	if ev[1].Title != "Show HN: Paperclip" {
		t.Fatalf("non-github item dropped: %+v", ev[1])
	}
}

func TestParseProductHuntAtom_KeepsItemsWithoutGitHub(t *testing.T) {
	ev, err := ParseProductHuntAtom([]byte(productHuntAtomFixture), "2026-08-18T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 2 {
		t.Fatalf("len=%d want 2", len(ev))
	}
	if ev[0].Title != "Whisperstream" || ev[0].Snippet != "Local AI dictation for Windows" {
		t.Fatalf("first = %+v", ev[0])
	}
	if ev[0].URL != "https://www.producthunt.com/products/whisperstream" {
		t.Fatalf("url = %q", ev[0].URL)
	}
	if ev[0].Extra["github_repo"] != "" {
		t.Fatalf("whisperstream invented github %q", ev[0].Extra["github_repo"])
	}
	if ev[0].Extra["published"] != "2026-06-03T08:11:43-07:00" {
		t.Fatalf("published = %q", ev[0].Extra["published"])
	}
	if ev[0].Extra["product_url"] != "https://www.producthunt.com/r/p/1?app_id=339" {
		t.Fatalf("product_url = %q", ev[0].Extra["product_url"])
	}
	if ev[1].Extra["github_repo"] != "opentrade/opentrade" {
		t.Fatalf("opentrade github = %q", ev[1].Extra["github_repo"])
	}
}

func TestShowHNFeed_EmptySubjectReturnsItems(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Query().Get("tags") != "show_hn" {
			t.Errorf("tags=%q", r.URL.Query().Get("tags"))
		}
		_, _ = w.Write([]byte(showHNFixture))
	}))
	t.Cleanup(srv.Close)
	f := &showHNFeed{now: func() time.Time { return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC) }, apiURL: srv.URL}
	ev, err := f.Query(context.Background(), FeedQuery{Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 2 {
		t.Fatalf("empty subject browse len=%d", len(ev))
	}
	ev, err = f.Query(context.Background(), FeedQuery{Subject: "nvault", Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 0 {
		t.Fatalf("named subject must not attach launch items, got %d", len(ev))
	}
	if hits != 1 {
		t.Fatalf("named subject hit the network %d times", hits)
	}
	ev, err = f.Query(context.Background(), FeedQuery{Subject: "OpenAI", Browse: true, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 2 {
		t.Fatalf("Browse=true must fetch, got %d", len(ev))
	}
}

func TestProductHuntFeed_EmptySubjectReturnsItems(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(productHuntAtomFixture))
	}))
	t.Cleanup(srv.Close)
	f := &productHuntFeed{now: func() time.Time { return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC) }, feedURL: srv.URL}
	ev, err := f.Query(context.Background(), FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 2 {
		t.Fatalf("len=%d", len(ev))
	}
	ev, err = f.Query(context.Background(), FeedQuery{Subject: "OpenAI"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 0 {
		t.Fatalf("named subject must not attach PH items, got %d", len(ev))
	}
	if hits != 1 {
		t.Fatalf("named subject hit the network %d times", hits)
	}
}

func TestHackerNewsMentionFeed_DropsIrrelevantTitles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("tags") != "story" {
			t.Errorf("mention feed tags=%q want story", r.URL.Query().Get("tags"))
		}
		_, _ = w.Write([]byte(`{"hits":[
			{"title":"nvAlt is a notes app","url":"https://example.com/nvalt","points":10,"num_comments":1,"objectID":"1"},
			{"title":"nvault secret store","url":"https://example.com/nvault","points":20,"num_comments":3,"objectID":"2"}
		]}`))
	}))
	t.Cleanup(srv.Close)
	f := &hackerNewsFeed{now: func() time.Time { return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC) }, apiURL: srv.URL}
	ev, err := f.Query(context.Background(), FeedQuery{Subject: "nvault"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 1 || ev[0].Title != "nvault secret store" {
		t.Fatalf("mention filter = %+v", ev)
	}
}

func TestPromoteGitHubURL_CommandsAndReject(t *testing.T) {
	plan, err := PromoteGitHubURL("https://github.com/acme/radarbox")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Repo != "acme/radarbox" || plan.CloneSlug != "radarbox" {
		t.Fatalf("plan = %+v", plan)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("commands = %v", plan.Commands)
	}
	if !strings.Contains(plan.Commands[0], "ndev refs cref https://github.com/acme/radarbox") {
		t.Fatalf("cref = %q", plan.Commands[0])
	}
	if _, err := PromoteGitHubURL("https://www.producthunt.com/products/whisperstream"); err == nil {
		t.Fatal("non-github URL must fail closed")
	}
	if _, err := PromoteGitHubURL("https://github.com/topics/ai"); err == nil {
		t.Fatal("reserved github owner must fail closed")
	}
}

const productHuntGraphQLFixture = `{
  "data": {
    "posts": {
      "edges": [
        {
          "node": {
            "id": "1",
            "name": "Whisperstream",
            "tagline": "Local AI dictation",
            "votesCount": 128,
            "commentsCount": 20,
            "url": "https://www.producthunt.com/posts/whisperstream",
            "website": "https://whisperstream.app",
            "slug": "whisperstream",
            "createdAt": "2026-08-18T12:00:00Z"
          }
        },
        {
          "node": {
            "id": "2",
            "name": "OpenTrade",
            "tagline": "Trading harness",
            "votesCount": 40,
            "commentsCount": 4,
            "url": "https://www.producthunt.com/posts/opentrade",
            "website": "https://github.com/opentrade/opentrade",
            "slug": "opentrade",
            "createdAt": "2026-08-18T13:00:00Z"
          }
        }
      ]
    }
  }
}`

func TestParseProductHuntGraphQL_VotesWebsiteAndGitHub(t *testing.T) {
	ev, err := ParseProductHuntGraphQL([]byte(productHuntGraphQLFixture), "2026-08-18T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 2 {
		t.Fatalf("len=%d", len(ev))
	}
	if ev[0].Value != "128" || ev[0].Extra["transport"] != "graphql" || ev[0].Extra["product_url"] != "https://whisperstream.app" {
		t.Fatalf("first = %+v extra=%v", ev[0], ev[0].Extra)
	}
	if ev[0].Extra["github_repo"] != "" {
		t.Fatalf("whisperstream invented github %q", ev[0].Extra["github_repo"])
	}
	if ev[1].Extra["github_repo"] != "opentrade/opentrade" {
		t.Fatalf("opentrade github = %q", ev[1].Extra["github_repo"])
	}
}

func TestProductHuntFeed_GraphQLUsesTokenAndSkipsAtom(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("auth=%q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(productHuntGraphQLFixture))
	}))
	t.Cleanup(srv.Close)
	f := &productHuntFeed{
		now:        func() time.Time { return time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC) },
		graphqlURL: srv.URL,
		token:      "test-token",
		feedURL:    "http://127.0.0.1:1/atom-should-not-run",
	}
	ev, err := f.Query(context.Background(), FeedQuery{Browse: true, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 2 || hits < 1 {
		t.Fatalf("len=%d hits=%d", len(ev), hits)
	}
}

func TestGitHubHydrator_ExactNameOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[
			{"full_name":"acme/radarbox","name":"radarbox","html_url":"https://github.com/acme/radarbox","stargazers_count":12},
			{"full_name":"other/notes","name":"notes","html_url":"https://github.com/other/notes","stargazers_count":900}
		]}`))
	}))
	t.Cleanup(srv.Close)
	h := GitHubHydrator{SearchURL: srv.URL}
	repo, err := h.HydrateGitHubRepo(context.Background(), "Show HN: Radarbox", "")
	if err != nil {
		t.Fatal(err)
	}
	if repo != "acme/radarbox" {
		t.Fatalf("repo=%q", repo)
	}
	_, err = h.HydrateGitHubRepo(context.Background(), "Show HN: Unknown Product", "")
	if err == nil {
		t.Fatal("weak name must fail closed")
	}
}

func TestRegistry_RegistersLaunchBrowseFeeds(t *testing.T) {
	reg := NewFeedRegistry(func() time.Time { return time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC) })
	names := map[string]FeedTier{}
	for _, f := range reg.Feeds() {
		names[f.Name()] = f.Tier()
	}
	if names["showhn"] != TierFree || names["producthunt"] != TierFree {
		t.Fatalf("browse feeds = %v", names)
	}
	if names["hackernews"] != TierFree {
		t.Fatal("mention hackernews missing")
	}
}

func TestRadarCache_WriteReadGlanceAndStaleDay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "radar.json")
	now := time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)
	items := []RadarItem{
		{Feed: "showhn", Title: "Show HN: Radarbox", GitHubRepo: "acme/radarbox"},
		{Feed: "producthunt", Title: "Whisperstream"},
		{Feed: "showhn", Title: "Show HN: Paperclip"},
		{Feed: "showhn", Title: "dropped-fourth"},
	}
	if err := WriteRadarCache(path, items, now); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRadarCache(path, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Missing || got.Schema != radarCacheSchema || got.Day != "2026-08-18" || len(got.Items) != 4 {
		t.Fatalf("cache = %+v", got)
	}
	glance := GlanceRadarItems(got, 0)
	if len(glance) != 3 || glance[0].Title != "Show HN: Radarbox" || glance[0].GitHubRepo != "acme/radarbox" {
		t.Fatalf("glance should prefer github items first: %+v", glance)
	}
	reordered := RadarCache{Items: []RadarItem{
		{Feed: "showhn", Title: "NoRepo First"},
		{Feed: "showhn", Title: "HasRepo Later", GitHubRepo: "acme/later"},
		{Feed: "producthunt", Title: "Also none"},
	}}
	pref := GlanceRadarItems(reordered, 2)
	if len(pref) != 2 || pref[0].GitHubRepo != "acme/later" || pref[1].Title != "NoRepo First" {
		t.Fatalf("github-prefer glance = %+v", pref)
	}
	missing, err := ReadRadarCache(filepath.Join(dir, "nope.json"), now)
	if err != nil || !missing.Missing {
		t.Fatalf("missing = %+v err=%v", missing, err)
	}
	stale := now.Add(24 * time.Hour)
	wrongDay, err := ReadRadarCache(path, stale)
	if err != nil || !wrongDay.Missing {
		t.Fatalf("stale day = %+v err=%v", wrongDay, err)
	}
}
