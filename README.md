# ngtm

Standalone git root for the go-to-market factory.

The shipped engine lives in [nicos-tools](https://github.com/nstranquist/nicos-tools)
(`nicos-dev/internal/gtm`, `nicos-dev/internal/gtmcli`, `nicos-dev/cmd/ngtm`).
`ndev gtm` and this `ngtm` binary share that dispatcher so the surfaces cannot
drift. This repository is the product identity a remote can attach to; it does
not re-host the Go module graph.

## Run

```sh
ngtm version
ngtm --json feeds
ngtm feeds --json
ngtm --offline economics <product> --json --acv 30000 --cac 9000
ndev --json gtm launch cohort
```

`--json` and `--offline` may lead the verb or follow it.

## Build

Requires a sibling `~/dev/nicos-tools` checkout (or `NICOS_TOOLS`).

```sh
make install   # builds nicos-dev/cmd/ngtm → ~/.local/bin/ngtm
make test      # shipped GTM packages in nicos-tools
```

## Remote

Local git only until a GitHub remote is created by the operator.
Do not `gh repo create` from this tree unless asked.
