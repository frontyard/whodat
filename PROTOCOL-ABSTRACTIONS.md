# Protocol abstractions

How WhoDat abstracts messaging protocols, and how each backend implements
the contract. Companion to the [README architecture section](README.md#architecture).

- The contract lives in `internal/protocol` — one package the backend and
  every frontend piece agrees on, with no protocol-library imports.
- The implementation lives in `internal/wa` (WhatsApp, via
  [hypermeow](https://github.com/polymorfa/hypermeow), a whatsmeow fork) —
  the only backend currently shipped.
- The consumer lives in `internal/core` (the account registry that owns and
  routes between services) and `internal/qtui` (type-asserts optional
  interfaces, hides UI per capability flags).

```
internal/qtui         Qt UI — type-asserts the optional interfaces,
      │                hides UI elements whose Caps flag is false
      ▼
internal/core.App      account registry + id-prefix routing (SvcFor),
      │                AddAccount / LoadAccounts / RemoveAccount
      ▼
internal/protocol      the contract: Service, Backend registry, Caps,
      ▲                Bus/Event, Sink, optional interfaces
      │ implemented by
      ├──────────────  internal/wa  — hypermeow (WhatsApp companion device)
      └──────────────  … future backends register the same way

All backends persist and notify exclusively through protocol.Sink
→ store.DB (the app SQLite) + protocol.Bus (fan-out events).
```

---

## 1. The contract — `internal/protocol`

Package doc (`bus.go`): *"defines the contract between a messaging backend
(WhatsApp, …) and the app core: the event bus, the Service
interface every account satisfies, capability flags, and the normalized
group/auth types services exchange with the UI."*

| File | Contents |
|---|---|
| `service.go` | `Service` interface + normalized types (`AuthStage`, `GroupInfo`, `GroupParticipant`, `ParticipantChange`) |
| `backend.go` | `Backend` descriptor, backend registry, `NextAcct` |
| `ext.go` | Optional interfaces (`HistoryFetcher`, `PeerResolver`, `SessionLister`, `ContactLinker`, `PrivacyManager`, `Verifier`, `PeerIDs`) |
| `caps.go` | `Caps` feature flags + `FullCaps()` |
| `bus.go` | `Event`, `EventKind`, `TypingInfo`, `Bus`, `StampedBus` |
| `sink.go` | `Sink` — the persist-then-notify helper every backend holds |

### 1.1 `Service` — one logged-in account

`Service` (`service.go`) is **one account on one protocol**, not one
protocol: a second WhatsApp account is a second `Service` with its own
`Acct()`. Chat-scoped ids passed to and produced by these methods carry the
account prefix (`"wa:1234@s.whatsapp.net"`, `"wa2:1234@s.whatsapp.net"`), carried as the
`store.PeerID` type — a named string that can't exist without a valid
account prefix, so a bare wire id is a compile error, not a convention
violation. Construct via `store.NewID(acct, wire)` service-side or
`store.ParsePeerID` for untrusted input; `id.Wire()` strips the prefix at
the wire boundary.

Method groups:

| Group | Methods | Notes |
|---|---|---|
| Identity + lifecycle | `Acct`, `Protocol`, `Paired`, `OwnID`, `Caps`, `Connect`, `Disconnect`, `Reset`, `Logout` | `Logout` unlinks **and erases this account's local data only**; `Reset` rebuilds a dead client without deleting the session |
| Messaging | `SendText`, `SendMedia`, `SendReaction`, `SendStar`, `SendEdit`, `SendRevoke`, `Forward`, `MarkRead`, `SendTyping`, `SetPresence`, `SubscribePresence` | Message-creating sends return the stored `*store.Message` row so the UI can render immediately |
| Chat + contact ops | `SetChatPin`, `SetChatArchive`, `SetChatMute`, `SetDisappearing`, `MarkChatUnreadSync`, `DeleteChat`, `LeaveGroup`, `UpdateChatName`, `GetProfilePictureURL`, `SetBlocked`, `IsBlocked` | Chat-level flags must sync to the account's *other devices*, not just locally |
| Polls | `SendPoll`, `VotePoll`, `PollBallot` | `PollBallot` returns option → voter-ids |
| Groups | `CreateGroup`, `GetGroupInfo`, `GroupInviteLink`, `SetGroupName`, `SetGroupDescription`, `UpdateGroupParticipants` | All exchange the normalized `GroupInfo`/`GroupParticipant`, never protocol types |
| Media | `FetchMedia`, `CanFetchMedia` | Lazy download: resolve a row's attachment to a local path |
| Auth | `StartAuth`, `SubmitAuth`, `AuthStages` | Stage-driven login; the service advances via `EvAuthStage` events (stages listed below) |

Normalized supporting types:

- `AuthStage` — `"qr"`, `"phone"`, `"paircode"`, `"code"`, `"password"`,
  `"done"`, `"failed"`. `StagePairCode`/`StageCode`/`StagePassword` want a
  `SubmitAuth` value; `StageQR` carries the payload in `Event.QRCode`;
  `StageFailed` carries the error in `Event.AuthHint`.
- `ParticipantChange` — add / remove / promote / demote.
- `GroupParticipant` — `ID` (id the service accepts for ops), `Phone`,
  `Display`, `IsAdmin`, `IsMe`.
- `Mention` — `{Token, ID}`: a "@…" span in outgoing text bound to a
  `GroupParticipant` id. `SendText` maps it to the wire form —
  `ContextInfo.MentionedJID`.

### 1.2 `Backend` — protocol descriptor & registry (`backend.go`)

Backends **self-register** from their package `init()`, so the protocol
picker lists whatever is compiled in — adding a protocol is one import +
one `RegisterBackend` call.

| Field | Meaning |
|---|---|
| `Proto` | Stable key persisted in the accounts table (`"whatsapp"`) |
| `Name` | Display name for the picker |
| `AcctBase` | Account-id prefix allocated by `NextAcct` (`"wa"`) |
| `Multi` | Allows more than one live account of this protocol. A backend opts in **only once its ids are fully namespaced** (`"wa2:<id>"`) |
| `ScanHint` | Instruction shown under the pairing QR |
| `New` | `func(ctx, acct, dataDir, *store.DB, *Bus) (Service, error)` — instantiates one account; the result's `Acct()` must equal `acct`; it publishes through `bus.For(acct)` |

Registry API:

- `RegisterBackend(Backend)` — called from each backend's `init()`.
- `Backends()` — every registered protocol, sorted by display name.
- `BackendOf(proto)` — lookup by key.
- `NextAcct(base, taken)` — allocates an account id: the bare base (`"wa"`)
  when free, else the smallest `base+N` not in use (`"wa2"`, `"wa3"`).

### 1.3 `Caps` — feature advertisement (`caps.go`)

A struct of booleans covering messaging (reactions/edit/revoke/star/polls/
typing/presence/read receipts), chat ops (pin/archive/mute/disappearing/
block/leave/delete), groups, calls, and protocol extras (contact QR, linked
devices, privacy settings, E2E verification, directory search, QR auth,
server-side history paging). **The UI hides menu items, dialogs and buttons
whose capability is false** — the method may still exist on `Service`.

`FullCaps()` returns everything set except `ServerHistory` — WhatsApp's
current surface; a backend with server-side history paging would build
its caps by hand and add that flag.

### 1.4 Event bus (`bus.go`)

`Bus` is a fan-out pub/sub: `Subscribe(size)` returns a buffered channel and
an unsubscribe func; `Publish` never blocks — a slow consumer **drops
events** rather than stalling a backend handler goroutine. `Bus.For(acct)`
returns a `StampedBus` that stamps `Event.Acct` on every publish, so
subscribers can attribute events to an account.

`Event` fields: `Kind`, `Acct`, `QRCode`, `ChatJID`, `Message`, `Chat`,
`Typing` (`*TypingInfo`), `Err`, `AuthStage`, `AuthHint`, `Remove`.
Subscribers must not mutate shared fields.

| Kind | Meaning |
|---|---|
| `EvQRCode` | QR payload to display/refresh |
| `EvPaired` | pairing succeeded, waiting for connect |
| `EvConnected` / `EvDisconnected` | online / lost connection (auto-retry) |
| `EvLoggedOut` | session revoked — needs re-auth |
| `EvSyncStart` / `EvSyncDone` | offline/history sync running / finished |
| `EvMessage` | new incoming/outgoing message row |
| `EvMessageUpdated` | existing row changed (star/react/edit/revoke) — refresh views, never notify |
| `EvReceipt` | delivery status changed (only ID/status meaningful) |
| `EvChatUpdated` | chat row changed (name/avatar/unread/flags) |
| `EvChatGone` | chat cleared/deleted — open views must reload |
| `EvTyping` | typing state (`TypingInfo`) |
| `EvPresence` | contact online state (`TypingInfo` reused with `Online`/`LastSeen`) |
| `EvContactsUpdated` | contact list refreshed |
| `EvAvatarChanged` | peer profile picture changed — re-fetch (`Remove` set when the picture was taken down) |
| `EvAuthStage` | auth flow advanced (`AuthStage`/`AuthHint`/`QRCode` set) |

`TypingInfo` carries `ChatJID`, `SenderJID`, `SenderName`, `Composing`,
`Recording`, `Online`, `LastSeen` (zero = hidden).

### 1.5 `Sink` — persist, then notify (`sink.go`)

Every backend holds one `Sink` instead of a bare `StampedBus`. It couples
store writes with the bus events views listen for, so a send path can't
persist a row and forget to notify — the write and the event are one call.

- `Do(fn)` — enqueues DB-bound work on the shared **serial writer**
  (`store.DB.Do`), **gated on the account's wipe flag** (`store.DB.DoFor`):
  during a logout wipe, writes from a dead session are dropped instead of
  resurrecting purged rows (an in-flight history sync can enqueue for
  seconds). The gate is opened by `store.DB.SetWiping(acct, true)` before the
  purge and closed when a new session pairs.
- `Publish(e)` — stamps `Event.Acct` and fans out; the raw escape hatch for
  non-store events (QR codes, typing, lifecycle).
- `MessageSaved(ctx, m, preview, bumpUnread)` — insert the row, bump the chat
  summary, publish `EvMessage` + `EvChatUpdated`.
- `MessageChanged(chatID, m)` — republish an already-applied row change as
  `EvMessageUpdated`.
- `ChatChanged(chatID)` — publish `EvChatUpdated`.
- `AvatarChanged(chatID, removed)` — publish `EvAvatarChanged`.

### 1.6 Optional capability interfaces

Backends implement the subset their protocol supports; **the UI
type-asserts before offering the feature** (e.g. `internal/qtui/dialogops.go`,
`authops.go`, `models.go`). Both backends assert conformance at compile time
(`var _ protocol.X = (*Service)(nil)`).

| Interface | Method(s) | Purpose |
|---|---|---|

| `HistoryFetcher` (ext.go) | `FetchEarlier(ctx, chatID, before, limit) (int, error)` | Pull older history from the server when local store runs out (scroll-back) |
| `PeerResolver` | `ResolvePeer(ctx, query) (string, error)` | Turn a directory handle (phone number, `@username`) into a chat id |
| `SessionLister` | `Sessions(ctx) ([]string, error)` | Enumerate the account's active sessions/devices as display strings |
| `ContactLinker` | `ContactLink(ctx) (string, error)` | Scannable link to one's own profile (personal QR) |
| `PrivacyManager` | `PrivacySettings(ctx) map[string]string` / `SetPrivacy(ctx, name, value)` | Read/write account privacy toggles; names are backend-defined |
| `Verifier` | `VerifyCode(ctx, chatID) (string, error)` | E2E verification material for a peer (numeric safety number) |
| `PeerIDs` | `PeerIDForms(user) []string` / `AltJID(ctx, jid)` / `NumberJID(ctx, jid)` | Expose peer-id forms so display code can resolve aliases without importing protocol libraries (WhatsApp's LID↔PN split is the motivating case) |

### 1.7 Account ids & namespaces

- `Acct()` is simultaneously the account id, the chat-id prefix, and the
  wipe-scope key.
- **Every stored id is account-namespaced** — `"wa:<wire-jid>"`,
  `"wa2:<wire-jid>"` — so any number of WhatsApp accounts coexist.
  The `store.PeerID` type enforces it: `ParsePeerID` rejects prefixes that
  aren't account-shaped (a digit-led device part `"1234:40@…"` is wire, not
  account), and `id.Wire()` is the only way to the backend's raw form —
  hypermeow only ever sees raw JIDs.
- `store.DB.PurgeAccount(ctx, acct)` is a pure prefix match — it deletes
  rows keyed `"<acct>:%"` and touches nothing else, which is what makes
  per-account logout sound.
- Routing (`core.App.SvcFor`): the prefix before the first `:` names a
  registered account → that service. Unprefixed ids — and prefixes that
  aren't accounts (e.g. a WhatsApp device JID `"1234:7"`) — fall through to
  the primary account.

### 1.8 The consumer — `internal/core.App`

`core.App` is the account registry the UI talks to:

- `AddAccount(proto)` — under one lock: multi-account check, `NextAcct`
  allocation, `Backend.New`, registration, `OnRegister` hooks (call
  providers attach here), persistence.
- `LoadAccounts(recs)` — startup path: rebuild every persisted account.
- `RemoveAccount` — must be called **before** the service publishes
  `EvLoggedOut`, or the restart path resurrects the account.
- `Svc` / `EachSvc` / `Primary` / `SvcFor` — thread-safe lookups; `order`
  fixes a deterministic primary (first registered).
- `MediaPath` — routes a row's lazy download to the owning service via
  `CanFetchMedia` / `FetchMedia`.
- `Accounts()` — settings UI listing.

---

## 2. WhatsApp implementation — `internal/wa`

Built on [hypermeow](https://github.com/polymorfa/hypermeow) (a whatsmeow
fork): the account pairs as a **companion device** (like WhatsApp Web), and
everything syncs through the phone's account, end-to-end encrypted.

| File | Role |
|---|---|
| `service.go` | Backend registration, client/session lifecycle, the single `dispatch` event handler, logout wipe |
| `auth.go` | `AuthStages` / `StartAuth` / `SubmitAuth` |
| `send.go` | Text/media/reactions/edits/revoke/forward/read/typing/presence + `PeerIDs` + `ResolvePeer` |
| `ops.go` | Chat flags via app-state, polls, groups, blocking, account extras (sessions/privacy/verify/contact link) |
| `message.go` | Incoming message/receipt handling, reaction folding, call-log stubs, LID↔PN learning |
| `convert.go` | `waE2E.Message` → `store.Message` + `msgAction` (insert/react/revoke/edit/vote/disappearing) |
| `history.go` | Post-pair `HistorySync` dump ingestion |
| `media.go` | `FetchMedia` / `CanFetchMedia` |
| `bus.go` | Package-local aliases for the protocol event names |

### 2.1 Registration & session storage

```go
protocol.RegisterBackend(protocol.Backend{
    Proto: "whatsapp", Name: "WhatsApp", AcctBase: "wa",
    Multi:    true, // every stored id carries the account prefix
    ScanHint: "Scan the QR code with WhatsApp → Linked devices.",
    New: func(ctx, acct, dataDir, db, bus) (protocol.Service, error) { return New(...) },
})
```

`New` opens a whatsmeow `sqlstore.Container` over
`wastore[-<acct>].db` (the first account keeps the plain filename), gets
`GetFirstDevice`, builds a `whatsmeow.Client` presenting as "WhoDat,
desktop" (so the phone's Linked Devices list shows the app, not the
library), registers the single `dispatch` handler, and starts a 5-minute
ticker sweeping expired disappearing messages.

### 2.2 Lifecycle mapping

| `Service` | Implementation |
|---|---|
| `Paired` | `Client.Store.ID != nil` — a stored device row means a session |
| `OwnID` | Own JID without the device part (`ToNonAD()`), so it compares equal to sender JIDs stored elsewhere |
| `Connect` | No-op if connected; starts the QR channel first when unpaired; publishes `EvSyncStart`; `Client.Connect` |
| `Disconnect` | `Client.Disconnect` — session remains paired |
| `Reset` | Tears down the client and builds a fresh one from the container (a logged-out client can never reconnect) |
| `Logout` | Server unlink (`Client.Logout`) → `Store.Delete` → `wipeLocalData` → publish `EvLoggedOut` (hypermeow only emits that for server-side revocations, so it's published manually) |

`wipeLocalData` is the account-scoped erase: set the wipe gate,
`PurgeAccount` (prefix match on `"<acct>:%"` — rows owned by other
accounts are untouched), sweep every `whatsmeow_*` table (the LID map
and privacy tokens have no `ON DELETE CASCADE`; the schema version row is
captured and restored so the store reads as fresh), `RemoveAll` the
account's media and avatar dirs (they are per-account — `media/<acct>`,
`avatars/<acct>` — so this erases everything the account wrote and can
never touch a peer account's files), drop cached QRs. The gate is cleared on `events.PairSuccess` — a dead session writes
nothing.

### 2.3 Auth

- `AuthStages` → `["qr", "phone"]`.
- `StartAuth` → `startQR`: `Client.GetQRChannel` on a cancellable context;
  each code publishes `EvQRCode`, success publishes `EvPaired`, errors
  publish `EvLoggedOut`.
- `SubmitAuth("phone", number)` → `Client.PairPhone` → publishes
  `EvAuthStage(paircode)` with the 8-digit code in `AuthHint`.

### 2.4 Messaging

| Operation | Mechanism |
|---|---|
| `SendText` | `SendMessage` with `Conversation` or `ExtendedTextMessage` (quote context via `ContextInfo`); mention tokens rewrite to `@<digits>` + `ContextInfo.MentionedJID`; row persisted through the sink |
| `SendMedia` | `Client.Upload` → `buildMediaMessage` (image/video/audio/PTT/sticker/document proto) → `SendMessage`; the full marshaled proto is stored on the row for later re-download |
| `SendReaction` | `Client.BuildReaction` + send; mirrored locally through the same folding path as incoming reactions |
| `SendStar` | App-state `BuildStar` — stars sync to the account's other devices, like the phone would |
| `SendEdit` | `Client.BuildEdit` (own messages only) + local `ApplyEdit` |
| `SendRevoke` | `Client.RevokeMessage` + local `SetRevoked` (media file unlinked) |
| `Forward` | Media is **re-uploaded** when the file is still local (stored CDN direct paths expire); otherwise the original proto is replayed with quote context stripped and `IsForwarded` set |
| `MarkRead` | `Client.MarkRead` for the non-own ids, anchored on the newest sender |
| `SendTyping` / `SetPresence` | `SendChatPresence` / `SendPresence` |
| `SubscribePresence` | `Client.SubscribePresence` (DMs) |

### 2.5 Chat & contact ops

Pin/archive/mute/mark-unread/delete are **app-state patches**
(`SendAppState` with `BuildPin` / `BuildArchive` / `BuildMute` /
`BuildMarkChatAsRead` / `BuildDeleteChat`), anchored on the last message's
timestamp + key so other devices apply them consistently; each is mirrored
locally (`SetChatFlags` / `SetUnread`) and followed by `EvChatUpdated`.
`DeleteChat` additionally drops local rows/media and publishes `EvChatGone`.

- `SetDisappearing` → `Client.SetDisappearingTimer` + local seconds.
- `SetBlocked`/`IsBlocked` → `Client.UpdateBlocklist` / contacts table;
  server `Blocklist` events are folded back via `dispatch`.
- `UpdateChatName` → group metadata or the contact's push name, probing the
  LID↔PN counterpart when the DM key has no name.
- `GetProfilePictureURL` → `Client.GetProfilePictureInfo` — returns a URL.

### 2.6 Polls

`SendPoll` → `BuildPollCreation`; `VotePoll` → `BuildPollVote`. Incoming
votes arrive encrypted: `applyPollVote` decrypts with
`Client.DecryptPollVote` and matches option hashes from
`HashPollOptions`. `PollBallot` reads `db.PollVotes`.

### 2.7 Groups

`CreateGroup` (accepts JIDs or bare phone digits), `GetGroupInfo`
(participant ids **prefer the PN over the LID** — group ops address the
number), `GroupInviteLink` (optional reset), `SetGroupName` /
`SetGroupDescription`, `UpdateGroupParticipants` mapping
`protocol.ParticipantChange` → the whatsmeow equivalent. Extra beyond the
interface: `JoinGroupByLink`.

### 2.8 Media

`FetchMedia` unmarshals the row's stored proto payload (it carries the
upload keys), downloads via `Client.DownloadAny`, and writes staged
(`.part` then rename — a crash must not leave a partial file served as the
real attachment). `CanFetchMedia` = proto present + a media row type.

### 2.9 Incoming events

One `dispatch` handler switches on whatsmeow events; DB-bound cases run
through `sink.Do`. Notable mappings: `Message` → `onMessage` (convert →
`msgAction`; call-log stubs become `TCall` rows; push names and LID↔PN pairs
learned from traffic); `Receipt` → status upgrades that never downgrade
(`statusRank`); `HistorySync` → background ingestion; `ChatPresence` /
`Presence` → `EvTyping` / `EvPresence`; `Archive`/`Pin`/`Mute`/
`MarkChatAsRead` mirror other devices' flags; `ClearChat`/`DeleteChat` →
`EvChatGone`; `UndecryptableMessage` → placeholder row so the transcript gap
is visible; `IdentityChange` → "security code changed" system row, deduped
to once/day and skipped entirely when the chat holds no real (non-system)
message — a notice-only chat would be a phantom sidebar entry; `LoggedOut` /
`ManualLoginReconnect` → wipe + `EvLoggedOut`.

### 2.10 History

No `HistoryFetcher` (cap `ServerHistory` false) — WhatsApp delivers the
post-pair `HistorySync` dump instead. `onHistorySync` ingests each
conversation in a single transaction (checking for cancellation every 400
messages), applies deferred edits/revokes/reactions in a second pass
**oldest-first** (a mutation precedes its target in newest-first order and a
sender's latest reaction must win), seeds inline reactions before insert,
and learns push names for senders missing from the contact store.

### 2.11 LID↔PN identity handling

WhatsApp addresses peers under two servers: the phone-number JID (`@s.whatsapp.net`)
and the opaque LID (`@lid`). The service keeps both alive:
`learnAlt` records pairs from traffic; `syncContacts` batch-resolves contacts
both directions; `canon` maps events to the canonical (PN) chat row;
`altJID` consults the session LID store (with a fallback probe for LID
digits carried under the PN server).

### 2.12 Optional interfaces & caps

Implements `SessionLister` (companion devices),
`ContactLinker` (`wa.me` QR link), `PeerResolver` (phone → JID via
`IsOnWhatsApp`), `PrivacyManager` (typed settings flattened to the dialog's
keys), `Verifier` (numeric safety code; PN JIDs resolve through the LID
store first because the API keys identities by LID), `PeerIDs` (PN + LID
forms, LID↔PN counterpart, number-carrying form). Not `HistoryFetcher`.

`Caps` = `protocol.FullCaps()` — everything except `ServerHistory`.

---

## 4. Feature matrix

Optional interfaces implemented by the WhatsApp backend (`✓` implemented,
`—` not):

| Interface | WhatsApp |
|---|---|
| `HistoryFetcher` | — |
| `PeerResolver` | ✓ phone → JID |
| `SessionLister` | ✓ companion devices |
| `ContactLinker` | ✓ `wa.me` link |
| `PrivacyManager` | ✓ |
| `Verifier` | ✓ safety number |
| `PeerIDs` | ✓ PN + LID forms |

`Caps` = `protocol.FullCaps()` — everything except `ServerHistory`.

Auth surface:

| | WhatsApp |
|---|---|
| `AuthStages` | `qr`, `phone` |
| QR flow | whatsmeow QR channel (companion link) |
| Phone flow | 8-digit pairing code (`paircode` stage) |
| Account ids | `"wa"`, `"wa2"`, … `Multi: true` |

---

## 5. Adding a new protocol

1. Create `internal/<key>/` and register a `protocol.Backend` in `init()`
   (`Proto`, `Name`, `AcctBase`, `ScanHint`, `New`; set `Multi: true` only
   once every chat id you store carries the account prefix).
2. Implement `protocol.Service`, with a compile-time assertion:
   `var _ protocol.Service = (*Service)(nil)`.
3. Hold a `protocol.NewSink(acct, db, bus)` and route **all** DB writes
   through `sink.Do` — that's what makes the logout wipe sound.
4. Implement the optional interfaces you support, with assertions; return a
   truthful `Caps()` — the UI hides whatever is false.
5. Drive login through `StartAuth`/`SubmitAuth` + `EvAuthStage` events.
6. Keep protocol state in your own sidecar under `dataDir` (one file per
   account), never in the app DB — logout then reduces to purging your
   app-DB rows and deleting the sidecar.
7. The protocol picker, account registry, persistence and routing pick the
   backend up automatically via `protocol.Backends()`; no UI changes needed
   beyond what your `Caps`/optional interfaces enable.

## 6. Tests

- `internal/protocol/bus_test.go` — bus fan-out/drop semantics.
- `internal/wa/convert_test.go`, `wipe_test.go` — message conversion,
  account-scoped wipe.
- `internal/core/accounts_test.go`, `registry_test.go`, `helpers_test.go` —
  registry behavior against fake services, plus the UI-facing helpers.
