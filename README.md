# Nicos GTM (`ngtm`)

Public name: **Nicos GTM**. Short name / CLI: **ngtm**.

A local go-to-market factory. It pairs live data feeds with an LLM and tags
every claim `grounded` / `inferred` / `speculative`. Unbacked theses are
rejected. Placement receipts must be real public URLs.

**Public product:** https://github.com/nstranquist/ngtm  
**Landing:** [`docs/human/index.html`](docs/human/index.html) (same page as [`docs/index.html`](docs/index.html))

## Clone the public product

```sh
git clone https://github.com/nstranquist/ngtm.git
cd ngtm
```

Open `docs/human/index.html` in a browser. That is the stranger-reachable
object: name, license, landing, SEO project, and launch kit.

This repository is the catalog identity for `product.ngtm`. It does not
re-host the Go module. The `ngtm` binary is built from the operator's
existing engine checkout (`make install` below), not from a second public
clone.

## What the CLI covers (when installed)

```sh
ngtm social <product> --pitch "…" --channels show-hn,x,reddit
ngtm launch plan <product> --week 2026-W33
ngtm launch kit <product> --pitch "…" --channels show-hn
ngtm launch open <product> --channel show-hn --json
ngtm --json seo research <product> --config docs/seo-project.yaml
ngtm feeds doctor
```

`--json` and `--offline` may lead the verb or follow it.
`--offline` is rejected on verbs that have no hermetic path.
`seo measure --offline` requires `--fixture`.

MCP (`ngtm mcp`) is analysis and read-only launch. Ledger writes stay on the CLI.

## Operator make (optional)

`make install` builds the engine from `$NICOS_TOOLS` / `~/dev/nicos-tools` if
that private checkout already exists on this machine. It is not a public
clone-and-run path.

## License

MIT. See [LICENSE](LICENSE).
