# ncli — 2026-W33 Show HN draft

ncli’s GitHub remote is private (unauthenticated 404). This draft does not
invent a public clone URL.

## Show HN draft (launch)

**Title:** Show HN: ncli – hermetic clap+ratatui CLI over ndev/nship/ngtm

I built ncli. It is a Rust clap + ratatui + crossterm CLI that ships the
essential ndev / nship / ngtm surfaces as `ncli`, `ncli-ndev`, `ncli-nship`,
and `ncli-ngtm` so host binaries on PATH stay untouched.

How it works: default mode is hermetic fixtures with typed JSON contracts.
`--live` / `NCLI_MODE=live` promotes to the host when available. Long-tail
verbs passthrough. Fail-closed if a wrapped host tool is missing.

Why I built it: agents and operators need a small board that will not clobber
production `ndev` / `nship` / `ngtm`, and that still works offline.

There is no public git remote yet. Do not paste a github.com/nstranquist/ncli
clone as if strangers can fetch it.

I'll be in the comments — what would you need before this is worth a public repo?
