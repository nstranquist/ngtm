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

## Operator leftovers (not engine bugs)

- Create the GitHub remote for `~/dev/ngtm`.
- Post the next launch cohort (ledger is plan/kit/retire-heavy).
- Stand up a free SERP (SearXNG) so brand/SEO can ground.
- `ndev catalog initiative refresh ngtm-quality`.
