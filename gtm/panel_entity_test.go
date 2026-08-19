package gtm

import (
	"strings"
	"testing"
)

func TestRunEntityNamePanel_CollisionKills(t *testing.T) {
	p := RunEntityNamePanel("Nicos Labs LLC", nil, "Q1 (instance of \"software\")")
	if p.Survives {
		t.Fatal("collision should not survive")
	}
	if p.Title != "Legal-name screen" {
		t.Fatalf("title=%q", p.Title)
	}
	var critics []string
	for _, v := range p.Verdicts {
		critics = append(critics, v.Critic)
	}
	if !strings.Contains(strings.ToLower(strings.Join(critics, " ")), "collision") {
		t.Errorf("missing collision critic: %v", critics)
	}
}

func TestRunEntityNamePanel_NoCollisionDoesNotUseDomain(t *testing.T) {
	ev := []Evidence{{ID: "hackernews:0", Feed: "hackernews", Metric: "mentions", Synthetic: false}}
	p := RunEntityNamePanel("Nicos Software LLC", ev, "")
	for _, v := range p.Verdicts {
		if v.Critic == "Category Clarity" {
			t.Fatal("entity panel must not use product category clarity")
		}
	}
}
