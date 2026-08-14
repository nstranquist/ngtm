package gtm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRedditFeedFallsBackToPublicRSS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search.json":
			http.Error(w, "blocked", http.StatusForbidden)
		case "/search.rss":
			w.Header().Set("Content-Type", "application/atom+xml")
			fmt.Fprint(w, `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><entry><category term="golang"/><id>t3_1</id><link href="https://www.reddit.com/r/golang/comments/1/docs_puller/"/><title>OpenAI agents using local docs retrieval</title></entry></feed>`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	feed := &redditFeed{now: fixedNow, baseURL: server.URL}
	evidence, err := feed.Query(context.Background(), FeedQuery{Subject: "OpenAI", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 1 || evidence[0].Feed != "reddit" || evidence[0].Extra["transport"] != "rss" || evidence[0].Value != "1" {
		t.Fatalf("RSS fallback evidence = %+v", evidence)
	}
}

func TestRedditFeedSurfacesRateLimitMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search.json" {
			http.Error(w, "blocked", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Ratelimit-Reset", "58")
		http.Error(w, "limited", http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	feed := &redditFeed{now: fixedNow, baseURL: server.URL}
	_, err := feed.Query(context.Background(), FeedQuery{Subject: "OpenAI"})
	var rateLimit *FeedRateLimitError
	if !errors.As(err, &rateLimit) || rateLimit.RetryAfter != 58*time.Second {
		t.Fatalf("rate limit error = %v, want retry-after 58s", err)
	}
}
