# docs-puller — 2026-W33 Show HN draft

## Show HN draft (launch)

**Title:** Show HN: docs-puller – local-first docs retrieval you can actually eval

I built docs-puller. It mirrors vendor, reference, and local project docs into Markdown, builds a SQLite FTS5 index, and gives agents a private search surface. Retrieval quality is checked in: a 24-query sample corpus anyone can replay with no API key (BM25/FTS5 Hit@1 95.8% on 2026-07-03) plus a larger maintainer corpus you should treat as a claim until you rebuild it.

How it works: `docs-puller pull` fetches sources, `reindex` writes FTS5, `eval` diffs a fixture against a pinned baseline. The same binary serves a local search UI. No hosted retrieval vendor in the default path.

Why I built it: agents that “search the docs” by calling a third-party index send private queries off-box and have no replayable quality number. I wanted pull-any-docs, search on my machine, and a number I can re-run.

It's live: https://github.com/nstranquist/docs-puller

I'll be in the comments — what would make this useful for you?
