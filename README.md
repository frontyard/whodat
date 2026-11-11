<p align="center">
  <img src="internal/core/icon.svg" width="120" alt="WhoDat logo">
</p>
<h1 align="center">WhoDat</h1>
<p align="center">
  Unofficial <b>WhatsApp messaging app for Linux</b>, written in
  Go with Qt6/QML.
</p>
<p align="center">
  <a href="https://github.com/frontyard/whodat/releases"><img src="https://img.shields.io/github/v/release/frontyard/whodat" alt="Release"></a>
  <a href="https://github.com/frontyard/whodat/actions/workflows/release.yml"><img src="https://github.com/frontyard/whodat/actions/workflows/release.yml/badge.svg" alt="Build"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/frontyard/whodat" alt="Go version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/frontyard/whodat" alt="License"></a>
  <img src="https://img.shields.io/badge/platform-Linux-blue" alt="Platform: Linux">
</p>

WhoDat is a WhatsApp messenger — one app, one local store,
several accounts, talking the protocol directly (nothing proxied
through third parties). See [WhatsApp](#whatsapp) for how to sign in.

> ⚠️ **Unofficial client.** WhatsApp/Meta does not officially support
> third-party clients. Using one can, in principle, lead to account
> restrictions. Use at your own risk.

## Motivation

WhatsApp has no official Linux client, so the usual answers are a
browser tab or an Electron wrapper around the web app — a siloed
window with its own quirks and its own idea of desktop integration.

WhoDat is an attempt at a native app instead: the protocol is
spoken directly (no embedded browser, nothing proxied through third
parties), everything lands in one local store you can browse
offline, and the UI is a proper desktop citizen — tray,
notifications, and a theme that follows your system palette.

## Features

- **Accounts** — staged login (QR code or pairing code); multiple
  WhatsApp accounts supported
- **Messaging** — text, replies, quotes, emoji reactions, edit, delete,
  forward; delivery + read receipts; typing indicators; presence
- **History** — full message history synced into a local SQLite DB
- **Media** — images, video, stickers, documents, audio; downloaded to a
  local cache, inline previews, built-in viewer/player
- **Voice notes** — record from mic, waveform preview, send as PTT opus
- **Contacts & groups** — contact list, group info, start new chats
- **Calls** — 1:1 and group **audio + video** calls via
  [meowcaller](https://github.com/suhwr/meowcaller) (experimental)
- **Desktop integration** — notifications, system tray
  (StatusNotifierItem), unread badge, autostart, dark/light theme
- **Theming** — Material 3 UI that picks up your
  [DankMaterialShell](https://github.com/AvengeMedia/DankMaterialShell)
  palette live; falls back to the desktop/Qt palette elsewhere
- **Offline** — everything is stored locally; browse history without a
  connection

## WhatsApp

Pairs with your account as a **companion device** (like WhatsApp Web)
via [hypermeow](https://github.com/polymorfa/hypermeow), a
[whatsmeow](https://github.com/tulir/whatsmeow) fork: scan the QR code
in WhatsApp → **Linked Devices → Link a Device**, or enter your phone
number for an 8-digit pairing code. Everything syncs through your
phone's account, end-to-end encrypted. Multiple WhatsApp accounts are
supported; each keeps its session and crypto state in its own
`wastore[-<acct>].db`. Calls ride
[meowcaller](https://github.com/suhwr/meowcaller) (experimental — see
[Known limitations](#known-limitations)).

## Install

### Packages

Grab `whodat*.deb`, `whodat*.rpm`, `whodat*.pkg.tar.zst` or the
AppImage from a release.

```sh
sudo dpkg -i whodat_*.deb              # Debian/Ubuntu
sudo dnf install whodat_*.rpm          # Fedora
sudo pacman -U whodat-*.pkg.tar.zst    # Arch
./whodat-*.AppImage                    # any modern glibc distro — Qt6/QML bundled
```

### Runtime dependencies

The deb/rpm/Arch packages declare these automatically (Arch: recommends
land as optdepends). The AppImage bundles
everything Qt6/QML-related itself (including platform, multimedia, SVG
and theme plugins) — it only needs a host with a modern glibc and the
usual GL/fontconfig/X11-or-Wayland stack; host GStreamer **plugins** are
still used for calls (see below).

| Purpose | Debian/Ubuntu | Fedora | Arch |
|---|---|---|---|
| Qt6 base | `libqt6core6t64 libqt6gui6 libqt6widgets6` | `qt6-qtbase qt6-qtbase-gui` | `qt6-base` |
| Qt6 Quick/QML | `libqt6qml6 libqt6quick6 libqt6quickcontrols2-6 libqt6quicktemplates2-6` | `qt6-qtdeclarative` | `qt6-declarative` |
| Qt6 Multimedia | `libqt6multimedia6 libqt6multimediawidgets6` | `qt6-qtmultimedia` | `qt6-multimedia` |
| Qt6 SVG | `libqt6svg6` | `qt6-qtsvg` | `qt6-svg` |
| QML modules | `qml6-module-qtqml qml6-module-qtquick qml6-module-qtquick-controls qml6-module-qtquick-templates qml6-module-qtquick-layouts qml6-module-qtquick-dialogs qml6-module-qtquick-effects qml6-module-qtmultimedia` | bundled in `qt6-qtdeclarative`/`qt6-qtmultimedia` | bundled in `qt6-declarative`/`qt6-multimedia` |
| Wayland (recommended) | `libqt6waylandclient6 qt6-wayland` | `qt6-qtwayland` | `qt6-wayland` |
| GStreamer core (calls) | `libgstreamer1.0-0 libgstreamer-plugins-base1.0-0` | `gstreamer1 gstreamer1-plugins-base` | `gstreamer gst-plugins-base-libs` |
| Call/media plugins (recommended) | `gstreamer1.0-plugins-good gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly gstreamer1.0-libav gstreamer1.0-pipewire` | `gstreamer1-plugins-good gstreamer1-plugins-bad-free gstreamer1-plugins-ugly-free gstreamer1-libav pipewire-gstreamer` | `gst-plugins-good gst-plugins-bad gst-plugins-ugly gst-libav gst-plugin-pipewire` |
| Theme integration (recommended) | `qt6ct adwaita-qt6 qt6-gtk-platformtheme` | `qt6ct` | `qt6ct adwaita-qt6` |

Without the theme-integration packages Qt falls back to its default font
and widget style; `qt6ct`/`adwaita-qt6` pick up qt6ct.conf styling and
`qt6-gtk-platformtheme` mirrors GTK settings on GNOME (deb/rpm only — the
AppImage ships qt6ct + adwaita and skips the GTK plugin on purpose).

### Build from source

Build deps: Go ≥ 1.26, `gcc` **and** `g++` (miqt compiles C++17 and
pkg-configs `Qt6Widgets`, `Qt6Qml`, `Qt6MultimediaWidgets`), `pkg-config`,
Qt6 dev headers (`qt6-base-dev`, `qt6-declarative-dev`,
`qt6-multimedia-dev` or the equivalents), plus `libgstreamer1.0-dev`.

```sh
# Debian/Ubuntu build deps
sudo apt install golang pkg-config gcc g++ \
  qt6-base-dev qt6-declarative-dev qt6-multimedia-dev \
  libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev

make            # builds build/whodat (Qt UI)
make run        # build + launch
sudo make install   # install to /usr/local (PREFIX=… to override)
```

No Qt dev headers and no root? `./packaging/qt6-sysroot.sh` extracts the
needed dev packages into a local `.sysroot-qt/` which `make` picks up
automatically.

Other targets: `make test`, `make lint`, `make qmllint`, `make pkg`
(deb+rpm+Arch via [nfpm](https://nfpm.goreleaser.com/)), `make appimage`.

For a **self-contained AppImage** (what the release workflow produces),
`make appimage` additionally wants on PATH:

- `linuxdeploy` + `linuxdeploy-plugin-qt` — bundles the Qt6 runtime,
  platform plugins and QML modules
- `qmlimportscanner` (`qt6-declarative-dev-tools`) — required by the qt
  plugin; scans `internal/qtui/qml` (`QML_SOURCES_PATHS`) since the QML
  is embedded via `go:embed`
- `qt6ct` + `adwaita-qt6` installed — their theme plugins get copied into
  the image so the app follows your desktop font/style settings
- `appimagetool` — final squashfs packing (linuxdeploy can substitute)
- `rsvg-convert`/`magick` (optional) — renders a PNG icon so
  appimagetool can create `.DirIcon`

Without linuxdeploy the script falls back to a thin binary-only image
that expects Qt6 on the host.

## Usage

1. Launch `whodat` (or from your app launcher).
2. **Add account** on the first-run screen (or app menu → **Accounts**
   → **Add account**), then scan the QR code or enter your phone
   number — see [WhatsApp](#whatsapp) for the login flow.
3. Chat. Everything syncs through your phone's account as usual.

Session data lives in `~/.local/share/whodat/` (SQLCipher-free SQLite),
media cache in `~/.cache/whodat/`, settings in `~/.config/whodat/`.
See [ENCRYPTION.md](ENCRYPTION.md) for what's encrypted on the wire —
and what isn't, at rest.

Log out per account via the app menu → **Accounts**, or remove the
linked device from your phone — either way local data for that account
is erased.

## Architecture

```
cmd/whodat/        entrypoint
internal/
  config/          settings (JSON) + XDG paths
  store/           app SQLite DB: accounts, chats, messages, contacts
  protocol/        backend registry, capability flags, event bus
  wa/              hypermeow/whatsmeow wrapper: session, events, send ops
  media/           media download cache, voice-note recorder
  call/            call provider (meowcaller) +
                   GStreamer pipelines (mic/spk/cam, H.264/Opus)
  core/            UI-agnostic helpers shared with the frontend
  qtui/            Qt6/QML UI (models, bridge, theme, tray, calls)
```

Each account's protocol state stays strictly separate from WhoDat's
`whodat.db` (accounts, chats, messages, contacts — including
serialized protocol messages for media re-download and edit/revoke):
whatsmeow keeps crypto keys, sessions and app-state in
`wastore[-<acct>].db`.

The Qt UI runs its `QApplication`/`QQmlApplicationEngine` from Go via
[miqt](https://github.com/mappu/miqt): chats/messages are
`QAbstractListModel` subclasses, QML↔Go commands ride a
`QQmlPropertyMap` bridge, and backend callbacks are marshalled onto the
Qt thread with `mainthread.Start`. QML is embedded in the binary and
extracted to `~/.cache/whodat/qml/` on start (override with
`WHODAT_QML_DIR` for development).

When DankMaterialShell is installed, WhoDat reads its resolved
Material-You palette (`~/.cache/DankMaterialShell/dms-colors.json`) and
re-themes live on wallpaper/scheme changes; elsewhere it follows
system/light/dark with the Qt palette + built-in fallbacks. A tiny
`~/.local/state/whodat/status.json` exposes unread/focused-chat state
for shell plugins.

## Known limitations

- **Calls are experimental.** meowcaller implements the WhatsApp call
  protocol — group calls especially may be flaky.
- **Echo cancellation** depends on `gstreamer1.0-plugins-bad`
  (`webrtcdsp`); without it calls may feed back on speakers.
- **AppImages** are built with linuxdeploy + the qt plugin and bundle the
  Qt6 runtime, QML modules and theme plugins — but host GStreamer
  *plugins* are still dlopened (they aren't ELF-linked), so calls inside
  an AppImage need `gstreamer1.0-plugins-*` installed on the host.
  libstdc++/libc aren't bundled either: the image needs a host glibc at
  least as new as the build environment's.
- Ban risk: see warning above.

## License

WhoDat is free software: you can redistribute it and/or modify it under
the terms of the **GNU General Public License v3.0** — see
[LICENSE](LICENSE).

Copyright (C) 2026 WhoDat

Third-party components:

| Component | License |
|---|---|
| [hypermeow](https://github.com/polymorfa/hypermeow) | MPL-2.0 |
| [meowcaller](https://github.com/suhwr/meowcaller) | MIT |
| [miqt](https://github.com/mappu/miqt) | MIT |
| [go-gst](https://github.com/go-gst/go-gst) | LGPL-2.1 |
| [go-qrcode](https://github.com/skip2/go-qrcode) | MIT |
| [zerolog](https://github.com/rs/zerolog) | MIT |
| [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) | BSD-3-Clause |
| [protobuf](https://github.com/protocolbuffers/protobuf-go) | BSD-3-Clause |
| [Material Symbols](https://fonts.google.com/icons) (bundled UI icons) | Apache-2.0 |
| Qt6 / GStreamer (system libraries) | LGPL-3.0 / LGPL-2.1+ |

See [NOTICE](NOTICE) for third-party asset attribution.

"WhatsApp" is a trademark of Meta Platforms, Inc.; WhoDat is an
independent, unofficial project not affiliated with or endorsed by Meta.
