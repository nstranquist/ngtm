# Nicos GTM (`ngtm`)

Public name: **Nicos GTM**. Short name / CLI: **ngtm**.

A local go-to-market factory. It pairs live data feeds with an LLM and tags
every claim `grounded` / `inferred` / `speculative`. Unbacked theses are
rejected. Placement receipts must be real public URLs.

Landing (open this file in a browser): [`docs/human/index.html`](docs/human/index.html)

## Install (public clone-and-run)

The executable lives in the public [nicos-tools](https://github.com/nstranquist/nicos-tools)
tree. A stranger does **not** need this identity repository or an undocumented
sibling path.

```sh
git clone https://github.com/nstranquist/nicos-tools.git
cd nicos-tools/nicos-dev
go build -o "$HOME/.local/bin/ngtm" ./cmd/ngtm
ngtm version
ngtm --json feeds
```

`ndev gtm` is the same dispatcher once `ndev` is installed from that clone.

## What it covers

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

## This repository

Catalog identity for `product.ngtm` (name, license, landing, SEO project).
It does not re-host the Go module. Operator status notes live in
[`docs/STATUS.md`](docs/STATUS.md) and are not the public homepage.

## Local make (optional)

If you already have nicos-tools checked out, `make install` builds the sibling
engine. That is a convenience for this machine, not the public install path.

## License

MIT. See [LICENSE](LICENSE).
