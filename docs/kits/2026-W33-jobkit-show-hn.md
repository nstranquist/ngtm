# jobkit — 2026-W33 Show HN draft

## Show HN draft (launch)

**Title:** Show HN: jobkit – one Go binary for offline-first job applications

I built jobkit. It is a single Go binary for software-engineering job hunts: human-authored YAML for companies and searches, crawls of Greenhouse / Ashby / Lever, ATS-safe tailored resumes, and an embedded localhost coach UI. Skills and bullets are lexicon-driven, so the same engine works for any field you can describe.

How it works: state lives in `~/.jobkit/` as strict YAML. History is locked append-only JSONL. The coach is local HTML with ephemeral auth, not a hosted SaaS. Optional AI adapters are argv-only JSON; deterministic scoring stays authoritative.

Why I built it: job-search tools want a browser tab and a cloud account. I wanted one binary I can run offline, that an agent can read without inventing applications I did not send.

It's live: https://github.com/nstranquist/jobkit

I'll be in the comments — what would make this useful for you?
