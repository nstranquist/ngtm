# Contributing

This public repository is the source for Nicos GTM. Do not add a `replace`
that points at `nicos-tools` or any other private tree.

## Local gate

```sh
go test ./...
make test
```

`go.mod` must stay free of `replace` lines. CI fails if one appears.

## Rules

- Claims need evidence. Tag them `grounded`, `inferred`, or `speculative`.
- Placement records must be real public URLs.
- Do not commit live tokens, cookies, or a personal launch ledger.
- Keep `--offline` hermetic. Do not add a network call behind that flag.

Open a pull request against `main`. Publication to GitHub remains human-gated
for tags and pins.
