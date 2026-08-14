# W33 human gate — placements

Placement is operator-only. This environment must not invent a Hacker News,
Reddit, or X item URL. Do not run `ngtm launch posted` until a live public
thread exists.

## Nicos GTM

Draft: `docs/kits/2026-W33-show-hn.md`

https://news.ycombinator.com/submitlink?t=Show+HN%3A+Nicos+GTM+%E2%80%93+a+local+CLI+that+refuses+to+invent+launch+facts&u=https%3A%2F%2Fgithub.com%2Fnstranquist%2Fngtm

```
ngtm launch posted ngtm --channel show-hn --url https://news.ycombinator.com/item?id=YOUR_ID --expect "Nicos GTM"
```

## docs-puller (public repo)

Draft: `docs/kits/2026-W33-docs-puller-show-hn.md`

https://news.ycombinator.com/submitlink?t=Show+HN%3A+docs-puller+%E2%80%93+local-first+docs+retrieval+you+can+actually+eval&u=https%3A%2F%2Fgithub.com%2Fnstranquist%2Fdocs-puller

```
ngtm launch posted docs-puller --channel show-hn --url https://news.ycombinator.com/item?id=YOUR_ID --expect "docs-puller"
```

## jobkit (public repo)

Draft: `docs/kits/2026-W33-jobkit-show-hn.md`

https://news.ycombinator.com/submitlink?t=Show+HN%3A+jobkit+%E2%80%93+one+Go+binary+for+offline-first+job+applications&u=https%3A%2F%2Fgithub.com%2Fnstranquist%2Fjobkit

```
ngtm launch posted jobkit --channel show-hn --url https://news.ycombinator.com/item?id=YOUR_ID --expect "jobkit"
```

## ncli

Draft: `docs/kits/2026-W33-ncli-show-hn.md`

No public git remote (unauthenticated GitHub 404). Do not attach a clone URL
until that repository is public.
