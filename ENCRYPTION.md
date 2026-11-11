# Encryption

What is and isn't encrypted in WhoDat — on the wire, end-to-end, and
at rest. The short version:

| Path | Transport encryption | End-to-end |
|---|---|---|
| WhatsApp messages | Noise socket (Curve25519) | ✅ Signal protocol |
| WhatsApp media | E2E-encrypted blob over HTTPS | ✅ per-file AES key |
| WhatsApp calls | SRTP (P2P) | ✅ |
| Link-preview fetches | HTTP(S) to third-party hosts | ⚠️ plaintext `http://` possible |
| Avatar fetches | HTTPS to WhatsApp CDN | n/a |
| Local store | — | ❌ plaintext SQLite/files |

## The header lock icon

The open chat's header shows a lock glyph beside the title, driven per
chat by the backend's `protocol.ChatEncryptor` report:

- **Solid accent lock** — end-to-end encrypted: WhatsApp chats other
  than newsletters.
- **Dimmed, struck lock** — not end-to-end. Transport encryption still
  applies; the icon means the *server can read* message contents, not
  that the traffic is plaintext.
- **No icon** — the backend can't say (lookup failed or interface
  unimplemented); hidden rather than guessing.

It refreshes on chat open and on chat updates, so a live change to the
chat's encryption state flips it mid-session.

## WhatsApp (`internal/wa`, hypermeow/whatsmeow)

Two stacked layers:

- **Transport**: the client connects to WhatsApp's servers over a
  Noise-encrypted socket (the standard WhatsApp Web companion-device
  handshake). Every stanza rides it.
- **Payload**: message contents are additionally Signal-protocol
  encrypted end-to-end — sessions for 1:1, sender keys for groups —
  using `libsignal-protocol-go`. WhatsApp's servers forward ciphertext;
  they can't read message bodies. History sync rides the same E2E
  channel through your phone's account.

**Media** is encrypted with a per-file AES key before upload, so blobs
on `mmg.whatsapp.net` are ciphertext; downloads come back over HTTPS
and are decrypted locally (`s.Client().DownloadAny`).

**Verification**: safety numbers are surfaced through `Verifier` /
`VerifyCodes` — the "verify encryption" dialog on a WhatsApp chat.

**Calls** (`internal/call/wa_provider.go` via meowcaller) implement the
WhatsApp call protocol: E2E-encrypted SRTP between the participants.

## Out-of-band HTTP (`internal/linkprev`, `internal/core`)

Paths that leave the WhatsApp socket entirely:

- **Link previews**: WhatsApp previews are generated sender-side —
  when your outgoing text contains a link, the app fetches the page's
  Open Graph tags directly. The fetcher dials only public addresses
  (SSRF guard), but `http://` links — and `http://` `og:image` targets
  or redirects — are fetched **plaintext**. The linked host also sees
  your IP; that's inherent to sender-side previews (official WhatsApp
  does the same).
- **Avatar fetch**: WhatsApp profile pictures are fetched over HTTPS
  from `mmg.whatsapp.net`. The code accepts any `http` scheme; in
  practice WhatsApp only returns HTTPS URLs.
- **DNS**: system resolver — plaintext lookups for WhatsApp hosts,
  preview targets, and STUN servers.

## At rest

Transport encryption ends at the process boundary — everything local
is plaintext:

- `~/.local/share/whodat/whodat.db` — chats, messages, contacts
  (SQLite, no SQLCipher)
- `wastore[-<acct>].db` — WhatsApp Signal keys and session state
- `~/.cache/whodat/` — downloaded media, avatars, QML cache

Anyone with read access to those files can read full history; the
`wastore` sidecar additionally carries the keys needed to impersonate
the account. Full-disk encryption is the intended answer.
