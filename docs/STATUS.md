# ngtm status

Last updated: 2026-08-14

## What shipped

The factory engine lives in `nicos-tools` (`nicos-dev/internal/gtm`,
`nicos-dev/internal/gtmcli`, `nicos-dev/cmd/ngtm`). This repo is the
standalone product git root (`product.ngtm`).

2026-08-14 honesty pass (plan close-out):

- Leading `--json` / `--offline` work on `ngtm` and `ndev --json gtm`
  forwards JSON for launch, landing, and design.
- Offline/fixture paths do not call Recraft or live-crawl `site_url`.
- Social specificity requires the hook quantity to appear in evidence.
- Launch conversion is latest-wins; import cannot mint `source=operator`.
- Catalog/skill no longer claim process isolation or that MCP mirrors every verb.
- Ollama embeddings fail closed on non-200.

## Catalog

| id | kind | status |
|---|---|---|
| `product.ngtm` | product | shipped (external, owned, `~/dev/ngtm`) |
| `cli.ngtm` | command | shipped (`nicos-dev/cmd/ngtm`) |
| `plugin.ngtm` | plugin | shipped (`plugins/ngtm`, v0.3.2) |
| `skill.ngtm` | skill | shipped |
| `mcp-server.gtm` | mcp-server | shipped |
| `ndev.gtm` / `nship.gtm` | subcommand | shipped (same dispatcher) |
| `context.gtm-engine` | context | shipped (shared cell) |
| `initiative.ngtm-quality` | initiative | active, unstable (<10 operator samples) |
| `initiative.ngtm-seo-quality` | initiative | active, 0 live-evidence samples |

`ndev catalog external doctor --id product.ngtm`: checks green; only issue is
`distribution_backlog` (no git remote).

## 2026-W33 cohort

Planned + kitted (not posted — no invented receipts):

| product | public URL | brand screen |
|---|---|---|
| `ngtm` | no GitHub remote yet | `ngtm.com` taken; `ngtm.dev` available; SERP is National Great Teachers Movement / airport Wikidata Q1433892 |
| `ncli` | https://github.com/nstranquist/ncli | `ncli.com` and `ncli.dev` taken |
| `docs-puller` | https://github.com/nstranquist/docs-puller | `docspuller.com` / `docspuller.dev` available |
| `jobkit` | https://github.com/nstranquist/jobkit | `jobkit.com` taken |

Kits and brand/SEO artifacts: `~/.nicos-dev/gtm/kits/2026-W33/` and `~/.nicos-dev/gtm/seo/ngtm/`.
SEO project config: `docs/seo-project.yaml` (free tier, SearXNG SERP, no invented site_url).
`garrid-build` W29 stale plan retired as abandoned.

Operator next: resolve `[FILL:]` in the kits, submit via `ngtm launch open`, then `launch posted` with the live URL. Do not record a placement without a public receipt.

## SearXNG / brand / SEO ground (2026-08-14)

- `ndev ask deep web-up --json` reused `nicos-searxng` on `http://localhost:8888` and wrote `SEARXNG_URL`.
- `ngtm feeds doctor`: searxng live + reachable; grounding advisory is live SERP.
- Live `brand` / `seo` / `seo research` all cited `[searxng:N]` (not fixtures).
- Research artifact provenance `live`, providers `[searxng]`, 4 seed keywords ranked, volume/intent/difficulty 0 (no DataForSEO).
- Short name `ngtm` is a collision; public copy should lead with **Nicos ngtm** / `ngtm.dev`.

## Operator leftovers

- Create the GitHub remote for `~/dev/ngtm`.
- Post the W33 kits on a real distribution channel and record receipts.
- `export SEARXNG_URL=http://localhost:8888` in the operator shell / direnv.
- `ndev catalog initiative refresh ngtm-quality` after more operator runs.
