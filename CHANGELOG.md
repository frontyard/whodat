# Changelog

All notable changes to WhoDat are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
releases are `vX.Y.Z` git tags.

<!--
Cutting a release: run the Release workflow with version X.Y.Z. It renames
[Unreleased] to [X.Y.Z] - <date>, opens a fresh [Unreleased] on top,
commits, tags vX.Y.Z, and attaches this section to the GitHub release notes.
-->

## [Unreleased]

Initial release.

### Added
- WhatsApp-only messenger — companion device on the multidevice
  protocol (hypermeow/whatsmeow): QR code or phone-number pairing,
  end-to-end encrypted, nothing proxied through third parties
- Multiple WhatsApp accounts through a pluggable backend registry —
  account list with live connection state, per-account log out, and
  per-account settings (linked devices, privacy, QR code)
- 1:1 and group chats — text, replies/quotes, emoji reactions
  (re-tap to remove), edit, delete, forward; delivery and read
  receipts, typing indicators, presence
- Full message history synced into a local SQLite database; browse
  offline
- Media: photos, video, audio, documents, stickers and polls with
  inline previews, built-in viewer/player and configurable
  auto-download
- Voice messages — record from the mic, waveform preview, sent as
  PTT opus
- Voice and video calls, 1:1 and group (experimental; meowcaller +
  GStreamer)
- Contacts and groups: contact list, new-chat picker, group info and
  admin UI, block, security-code verification, disappearing-message
  timers
- Starred messages with jump-to
- @mentions in group chats — the composer offers a member picker and
  sends real WhatsApp mentions (MentionedJID with LID/PN
  counterparts); received "@<digits>" tokens render as "@Name"
- Per-chat composer drafts — switching chats stashes the in-progress
  text, cursor, quoted/edit state and mentions, and restores them on
  return
- "Forwarded" marker on message rows
- Link previews — incoming cards render title, description, site and
  thumbnail (persisted locally, follow edits/deletes, open
  externally); outgoing texts fetch the link's Open Graph data
  sender-side and attach an ExtendedTextMessage, with private and
  loopback targets excluded
- Per-chat encryption indicator — a lock glyph beside the chat title
  for end-to-end encrypted chats (every WhatsApp chat except
  newsletters)
- Clear (×) button on the chat-search fields
- Native Qt6/QML interface in Material 3 style
- Live Material-You theming from DankMaterialShell (`dms-colors.json`,
  re-applies on wallpaper/scheme changes) with Qt-palette and built-in
  fallbacks on other desktops; system/light/dark
- Desktop integration: notifications, system tray
  (StatusNotifierItem), unread badge, autostart, close-to-tray
- `~/.local/state/whodat/status.json` — unread/focused-chat state
  for shell plugins (DMS widgets, waybar, …)
- Local-only storage under XDG dirs; logging out erases all local
  data (messages, media, avatars, session keys), including on remote
  unlink
- Packages: `.deb`, `.rpm`, Arch `.pkg.tar.zst`, and a self-contained
  AppImage (Qt6/QML bundled)
