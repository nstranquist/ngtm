# GEO playbook — measure, then engineer

This is the written form of the Tally / Mentions.so digest
([Jake Ward, 2026-08-19](https://x.com/jakezward/status/2090080908841779617)
and the quoted playbook). It is the contract for `ngtm geo`.

## Two objects, not one tactic

1. **The playbook** is Tally's loop: internet signal → prompt set →
   answer-shaped pages → citation gaps → product inside the assistant.
2. **The screenshot** is Mentions.so: one row per buyer prompt with
   position, sentiment, visibility, and competitors.

Do not ship `llms.txt` and call the work done. The row is the object.

## Causal chain

```
the internet already recommends the product
        ↓
measure the exact prompts that produce that recommendation
        ↓
publish pages that look like the answer the model wants to emit
        ↓
close citation gaps (pages that name a competitor but not us)
        ↓
put the product inside the assistant (MCP / ChatGPT app / Claude connector)
        ↓
refresh, harvest the next prompt, repeat
```

Tally said this plainly: ChatGPT recommends Tally because the internet
already did. They then engineered around that signal.

## Six jobs

| Job | Tally | `ngtm geo` |
|---|---|---|
| Prompt set | Ask new users for the exact prompt | Tracked YAML. `geo research` |
| Per-prompt probe | Mentions.so / Qwairy | `geo probe` via official APIs only |
| Answer-shaped pages | `/help/compare` + "best" / "alternative" | `geo emit-compare` (noindex until approved) |
| Machine-readable truth | `/ai-info` + `/llms.txt` | `geo emit-ai-info` / `geo emit-llmstxt` |
| Third-party citations | Reddit inbox, G2/PH, gap DB | Later. `ngtm launch signals` is the HN/Reddit half |
| Product in the model | Free remote MCP + ChatGPT app | Later. Do not start here |

## Honest labels

Mentions.so probes ChatGPT-the-product (often with search). A chat-completions
call is not that. Every row records `engine` as what we actually called:

- `openai-chat` — OpenAI Chat Completions
- `openai-search` — OpenAI search-preview model, when it exists
- `gemini` — Gemini generateContent
- `grok` — xAI Chat Completions
- `fixture` — hermetic eval only

Never write `chatgpt` unless the ChatGPT product was the engine.

## What we will not cargo-cult

- Do not apply this to the whole portfolio.
- Do not publish `/ai-info` and expect recommendations.
- Do not scrape chat UIs. Official APIs only. Fail closed if the key is missing.
- Do not add a new state root. Artifacts reuse the SEO store
  (`NGTM_SEO_WORKSPACE` or `~/.nicos-dev/gtm/seo/<project>/`) with kinds
  `geo-prompt-set`, `geo-probe`, `geo-measure`.
- Ranking pages stay `noindex` until a human approves them.

## First product

**docs-puller.** It is public, launched, and has a buyer sentence
("local docs corpus for agents"). Not Factory. Not the whole nicos-tools tree.

Tracked set: [`docs-puller.geo.yaml`](docs-puller.geo.yaml).

## Command surface

```sh
ngtm geo research docs-puller --config docs/geo/docs-puller.geo.yaml
ngtm geo probe docs-puller --engines openai-chat,gemini
ngtm geo measure docs-puller --strict
ngtm geo emit-ai-info docs-puller --out path
ngtm geo emit-llmstxt docs-puller --out path
ngtm geo emit-compare docs-puller --out-dir path
ngtm geo eval --strict --json
```

`ndev gtm geo` is the same engine.

`ndev vault run` can strip `GOPATH`/`GOMODCACHE`. Build `ngtm` first, then
vault-run the binary. Do not `go run` inside a stripped child.

## Sequence after this slice

1. Keep the prompt table live. Re-probe on a schedule.
2. Harvest real prompts from users ("how did you find us").
3. Internet signal: reviews, third-party lists, mention inbox.
4. Public remote MCP for the one product, only after the table is not empty.
