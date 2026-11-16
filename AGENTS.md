# AGENTS

## After making changes

Always rebuild and restart the app so the user sees the changes live:

1. Kill the running instance (`pgrep -f 'build/whodat'`).
2. `make` — rebuilds `./build/whodat` (CGO + Qt6 sysroot via `LD_LIBRARY_PATH`).
3. Relaunch detached:
   `LD_LIBRARY_PATH=$PWD/.sysroot-qt/usr/lib/x86_64-linux-gnu/ nohup ./build/whodat >/dev/null 2>&1 &`

## Verify

- `go test ./...`
- `make qmllint` — QML syntax check (pre-existing `unqualified access` warnings on `App.`/`Theme.` are normal)
- `make lint` — gofmt + go vet

## House rules

- No commits — the user commits manually.
- Remote servers are read-only; deploys are done by the user.
- Never write bare `@word` tokens in markdown (CHANGELOG.md, README, release
  notes) — GitHub autolinks them as user mentions, which lists squatted
  accounts (@unknown, @name, @digits) as release contributors and pings them.
  Always wrap literal mention examples in backticks, e.g. `` `"@<digits>"` ``.
