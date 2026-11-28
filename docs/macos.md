# macOS support

**Status: not a development target.** line-cli is a Windows tool. macOS is
supported only in a very limited, read-what's-on-screen form, and the
functionality that makes line-cli useful on Windows is **not** planned for
macOS. This document records why, so the decision does not have to be
re-investigated later.

## Short version

On Windows, line-cli reads your locally cached LINE database and decrypts it,
so it can read full history, search everything, list unread, download files,
and (in the official build) send. It can do this because it can obtain the
database key by scanning LINE's process memory.

On macOS **none of that is possible**, because the database key cannot be
obtained on a standard (SIP-enabled) Mac — not by line-cli and not by the user.
The encrypted messages are all cached locally, but without the key they are
just ciphertext.

## What works on macOS without the key

These do not touch encrypted content, so they work today:

- `line status` — whether LINE is running and how fresh the local data is
  (reads file size / mtime and process state only).
- `line version`, `line help`, `line skill install` — unrelated to LINE data.
- `line read --current` / `line search --current` — read the chat **currently
  open in LINE's main window** by scraping the on-screen message list via macOS
  Accessibility (AXUIElement). No database, no key.

### Limits of `read --current`

It reads only what LINE has **rendered on screen** — roughly the visible rows
plus a small buffer (measured: ~37 rows with `--limit 200`). It deliberately
does **not** scroll, change focus, or touch the message editor. So:

- It cannot "just read 200 messages." To see older messages you must scroll up
  in LINE by hand and run it again.
- The LINE window must be **open and rendered**. A window minimized to the Dock
  or hidden (`⌘H`) exposes no rows; a smaller window exposes fewer rows. It may
  be in the background / behind other windows.
- Requires Accessibility permission for the terminal running `line`, and LINE
  running, signed in, with one chat open.

This covers "summarize / reply to the chat I'm looking at right now," and
nothing broader.

## What does NOT work on macOS

Everything that needs to decrypt the database (`chats`, `contacts`, `members`,
`read` history, `search` across all chats, `unread`, `stats`, `find-file`,
`preview`, `watch`) returns `UNSUPPORTED_PLATFORM`. The online/write actions
(`download`, `notes`, `albums`, `react`, `unsend`, `send`, `update`) do not
exist in the open build on any platform.

A manual path exists — `line init --key-stdin` validates a user-supplied 32-hex
passphrase and caches it in the Keychain, which would unlock the read-only
commands above — but on a standard Mac there is no supported way for the user to
obtain that passphrase in the first place (see below). It is only usable if you
already have the key from elsewhere (e.g. extracted on a Windows machine).

## Why the key cannot be obtained (verified)

LINE for Mac (`/Applications/LINE.app`, `jp.naver.line.mac`, Team `VUTU7AKEUR`)
is a Mac App Store build: hardened runtime, App Sandbox, no `get-task-allow`.
All three retrieval paths are closed on a SIP-enabled Mac:

1. **Process memory** (the Windows method). `lldb process attach` is denied with
   "Not allowed to attach to process" — even though lldb itself carries the
   `com.apple.security.cs.debugger` entitlement — because the target is hardened
   and lacks `get-task-allow` and SIP is on. line-cli has no debugger
   entitlement at all, so `task_for_pid` is impossible.
2. **Plaintext on disk** — none. `qci_3.bin` is LINE's LEGY server-config cache,
   not the key. The only unencrypted SQLite file in `db/` is sticker metadata.
   The real `.edb` is encrypted (its first bytes are random; `sqlite3` cannot
   open it).
3. **Keychain** — the key machinery (`line::StorageKey` / UICKeyChainStore) uses
   the data-protection keychain under access group `VUTU7AKEUR.jp.naver.line.mac`.
   Querying that access group from any other process returns OSStatus **-34018**
   ("A required entitlement is not present"), and there is no login-password
   override for data-protection access groups. LINE has zero items in the legacy
   login keychain.

The conclusion holds without identifying the exact keychain item: the whole
access group is sealed, the disk has no plaintext key, and memory is unreadable.

## The only path to full macOS support, and its cost

The sole way to replicate the Windows experience would be:

1. The user disables SIP — from Recovery (`csrutil disable`), **not** via `sudo`
   in the running system — and likely reduces the security policy / sets
   `amfi_get_out_of_my_way=1`, then reboots.
2. line-cli gains new code: a Mach memory scanner (`task_for_pid` +
   `mach_vm_region` + `mach_vm_read`) mirroring the Windows scanner. This code
   does **not** exist in the macOS build; disabling SIP alone changes nothing.
3. Each key fetch must run as `root` (`sudo line …`); once cached, later reads do
   not need `sudo`.

This trades the security of the **entire machine** (any process could then read
any other process's memory; Gatekeeper/AMFI enforcement is weakened) for one CLI
tool. That is not an acceptable default, so it is not implemented. If it is ever
wanted, it must be an explicit, opt-in, clearly-labeled advanced feature.
