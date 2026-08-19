# First live probe — docs-puller — 2026-08-19

Engine: `openai-chat` (`gpt-4o-mini-2024-07-18`). 20 prompts.
`gemini` failed closed: `gemini-2.0-flash` is gone (HTTP 404). Default is now `gemini-2.5-flash`.

Mention rate: **0%**. That is the real result. Completions recommend Dash, Zeal, DevDocs, Context7, Mintlify, Algolia, MkDocs, Sphinx. They do not name docs-puller.

This is Tally's line: ChatGPT recommends Tally because the internet already does. docs-puller does not have that signal yet. `/ai-info` and `llms.txt` are machine-readable truth, not a ranking cheat.

Re-run:

```sh
ngtm geo research docs-puller --config docs/geo/docs-puller.geo.yaml
ndev vault run --only OPENAI_API_KEY,GEMINI_API_KEY -- \
  ngtm geo probe docs-puller --config docs/geo/docs-puller.geo.yaml --engines openai-chat,gemini
ngtm geo measure docs-puller
```
