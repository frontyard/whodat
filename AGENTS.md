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

## i18n

- QML strings go through `qsTr("…")` (`.arg(x)` fills `%1`…); Go strings go
  through `i18n.T("…", args…)` / `i18n.Tc(ctx, …)` / `i18n.Tn(sing, plural, n)`
  from `internal/i18n` (Qt-free — safe to import anywhere).
- `make i18n` runs `tools/i18nextract`: rewrites `internal/qtui/i18n/whodat.ts`
  (English source template) and merges `whodat_<lang>.ts`, dropping dead
  strings and keeping finished translations.
- Translators are shipped inside `whodat_<lang>.ts` next to `whodat.ts`;
  adding a file there makes the language appear in Settings → Language.
- Locale pick order: `WHODAT_LANG` env → `language` pref → Qt system locale.
  `WHODAT_LANG=de ./build/whodat` for a quick check. Language applies on
  restart (Qt translators install before the QML engine loads).
- Errors that reach the UI must be built at return time
  (`errors.New(i18n.T(…))` or a small func like `errBusy()`), never as
  package-level `var` — package init runs before the catalog loads.

## House rules

- No commits — the user commits manually.
- Remote servers are read-only; deploys are done by the user.
- Never write bare `@word` tokens in markdown (CHANGELOG.md, README, release
  notes) — GitHub autolinks them as user mentions, which lists squatted
  accounts (@unknown, @name, @digits) as release contributors and pings them.
  Always wrap literal mention examples in backticks, e.g. `` `"@<digits>"` ``.
