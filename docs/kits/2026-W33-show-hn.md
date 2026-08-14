# Nicos GTM — 2026-W33 Show HN draft

Public name: **Nicos GTM**. CLI: `ngtm`.

## Show HN draft (launch)

**Title:** Show HN: Nicos GTM – a local CLI that refuses to invent launch facts

I built Nicos GTM (`ngtm`). It is a Go CLI that turns go-to-market work into an append-only ledger: plan a weekly cohort, generate a channel kit, record a public placement URL, then measure HN/Reddit (or operator conversions) and emit DOUBLE-DOWN / ITERATE / KILL.

How it works: one dispatcher (`gtmcli.Dispatch`) is shared by `ngtm`, `ndev gtm`, and `nship gtm`. Live feeds (HN, Reddit, self-hosted SearXNG, optional cheap SERP keys) become evidence rows. Every claim is tagged `grounded`, `inferred`, or `speculative`. A Show HN draft that lacks a real URL leaves the link slot empty rather than minting a news.ycombinator.com item. Posted receipts must be absolute public http(s); localhost and private IP literals fail closed. Kit, posted, signal, verdict, and price-test refuse to write without an active plan, and refuse again after a KILL or retirement until you re-plan.

Why I built it: agents will write a “we launched on HN” paragraph that never happened. I wanted the factory I already run (launch loop, SEO research, brand screens) to fail the same way a test fails — not the way a slide deck lies.

It's live as the public product repo: https://github.com/nstranquist/ngtm
Clone:

```
git clone https://github.com/nstranquist/ngtm.git
```

I'll be in the comments — what would make this useful for you?

*Channel contract:* must start with "Show HN: "; first-person builder voice; lead with what it does and how it works; technical substance over benefit language; no marketing superlatives; text post: link plus a short story of why you built it; be present in the comments for the first 3 hours
*Best slot:* Tue-Thu 08:00-10:00 ET

**Claims**
- `[grounded]` Public product: https://github.com/nstranquist/ngtm
- `[grounded]` Shared dispatcher is the documented front door for `ngtm` / `ndev gtm`
- `[inferred]` Operators who already run agent GTM copy need a fail-closed ledger more than another template pack
