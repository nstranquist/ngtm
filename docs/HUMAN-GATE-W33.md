# W33 human gate — Nicos GTM placement

Placement is operator-only. This environment must not invent a Hacker News,
Reddit, or X item URL. Do not run `ngtm launch posted` until a live public
thread exists.

Exact composer URL (Show HN):

https://news.ycombinator.com/submitlink?t=Show+HN%3A+ngtm+%E2%80%93+Nicos+GTM+%E2%80%93+a+local+CLI+that+refuses+to+invent+launch+facts&u=https%3A%2F%2Fgithub.com%2Fnstranquist%2Fnicos-tools

Filled draft (no FILL slots): `docs/kits/2026-W33-show-hn.md`

After you submit, record the live item:

```
ngtm launch posted ngtm --channel show-hn --url https://news.ycombinator.com/item?id=YOUR_ID --expect "Nicos GTM"
```
