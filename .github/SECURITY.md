# Security Policy

## Supported versions

WhoDat is pre-1.0 and releases move fast: only the **latest release**
receives security fixes. If you're on an older build, update first and
check whether the issue still reproduces.

| Version | Supported |
| --- | --- |
| Latest release | ✅ |
| Older releases | ❌ |

## Reporting a vulnerability

Please report privately through GitHub's
[private vulnerability reporting][report] — **Security → Advisories →
Report a vulnerability** on this repository. Do not file public issues
for unpatched vulnerabilities.

[report]: https://github.com/frontyard/whodat/security/advisories/new

Include what you can:

- Affected version/commit and install method (deb, rpm, AppImage,
  source)
- Steps to reproduce, or a proof of concept
- Impact — what an attacker gains (session keys, message contents,
  code execution, …)

Reports are acknowledged as time permits — WhoDat is maintained by a
small team, not a security org. Fixes land on `main` and ship in the
next release; crediting in the release notes is opt-in.

## Scope

**In scope** — WhoDat's own code: the local store and media cache
(`~/.local/share/whodat/`, `~/.cache/whodat/`), the Go↔QML bridge,
media decoding and voice-note recording, link-preview fetching,
session/credential handling, and the packaging + release pipeline.

**Upstream** — the WhatsApp protocol, calls and Signal crypto live in
dependencies. Issues in
[hypermeow](https://github.com/polymorfa/hypermeow)/whatsmeow,
[meowcaller](https://github.com/suhwr/meowcaller) or
libsignal-protocol-go belong on their own trackers — though feel free
to open a WhoDat issue too if the fix needs a dependency bump here.

**Known / by design** — documented limitations, not vulnerabilities:

- Local message history and media cache are stored **unencrypted**
  (plaintext SQLite and files); see [ENCRYPTION.md](../ENCRYPTION.md)
  for the full threat model.
- WhoDat is an **unofficial client** — account restrictions by Meta
  are a risk of use, not a bug.
- `WHODAT_QML_DIR` loads arbitrary QML as a development override; it
  is not a privilege boundary for whoever runs the binary.
