# Live probes — docs-puller

## 2026-08-19 first pass

Engine: `openai-chat` (`gpt-4o-mini-2024-07-18`). 20 prompts.
`gemini` failed closed: `gemini-2.0-flash` is gone (HTTP 404). Default is now `gemini-2.5-flash`.

The first measure table printed `0/2` as if Gemini had answered. That was a
scoring bug. Review issue 2. Fixed: failed engines no longer count as live runs.

## 2026-08-19 second pass (after review fixes)

Engines: `openai-chat` + `gemini` (`gemini-2.5-flash`). 20 prompts × 2 = 40 rows.
`passed=true`. Mention rate: **0%**. Visibility is `0/2` on every prompt (two
live answers, zero mentions).

Completions still recommend Dash, Zeal, DevDocs, Context7, Mintlify.
One Gemini excerpt also matched a too-generic competitor name `Context`; that
alias is now `Neuledge Context` only.

This is Tally's line: ChatGPT recommends Tally because the internet already
does. docs-puller does not have that signal yet.

Re-run:

```sh
ngtm geo research docs-puller --config docs/geo/docs-puller.geo.yaml
ndev vault run --only OPENAI_API_KEY,GEMINI_API_KEY -- \
  ngtm geo probe docs-puller --config docs/geo/docs-puller.geo.yaml --engines openai-chat,gemini
ngtm geo measure docs-puller
```
