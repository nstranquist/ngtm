# Nicos GTM (`ngtm`)

Public name: **Nicos GTM**. Short name / CLI: **ngtm**.

A local go-to-market factory. It pairs live data feeds with an LLM and tags
every claim `grounded` / `inferred` / `speculative`. Unbacked theses are
rejected. Placement receipts must be real public URLs.

**Public product:** https://github.com/nstranquist/ngtm  
**Landing:** [`docs/human/index.html`](docs/human/index.html) (same page as [`docs/index.html`](docs/index.html))

This checkout is the product Go module (`github.com/nstranquist/ngtm`).
`make test` and `make install` run against this tree.

The public GitHub remote is still the identity/landing tree until this
module is pushed. Do not treat
`git clone https://github.com/nstranquist/ngtm.git` as a source install.

Open `docs/human/index.html` in a browser for the landing page.

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

## Build

```sh
make test
make install
```

## License

MIT. See [LICENSE](LICENSE).
