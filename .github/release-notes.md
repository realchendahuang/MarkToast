# MarkToast — pre-alpha

One memory for every agent: a local-first, Git-versioned Markdown memory hub
that 40 coding harnesses read and write. Memories are plain `.md` files in a
repository you own; Git is the history, SQLite is the index, and the hub
speaks HTTP and MCP on `127.0.0.1` only.

This is the **first public build**. Expect rough edges — please open an issue
with the version, your OS, and what you did.

## Downloads

| Platform | Asset | Notes |
|---|---|---|
| macOS, Apple Silicon | `MarkToast-desktop-*-macos-arm64.dmg` / `.zip` | desktop app (hub included) |
| macOS, Intel | `MarkToast-desktop-*-macos-amd64.dmg` / `.zip` | same app, Intel build |
| Linux, arm64 / x86_64 | `MarkToast-desktop-*-linux-<arch>.tar.gz` | desktop app; needs GTK 4 + WebKitGTK 6.0 |
| Windows, arm64 / x86_64 | `MarkToast-desktop-*-windows-<arch>.zip` | desktop app; needs the WebView2 runtime |
| macOS, Linux, Windows | `marktoast-*-<os>-<arch>.tar.gz` (`.zip` on Windows) | CLI only, no window toolkit needed |

Both macOS apps are **ad-hoc signed, not notarized**: there is no Developer ID
in CI. Gatekeeper will refuse the first launch. Either right-click → *Open*,
or:

```bash
xattr -d com.apple.quarantine /Applications/MarkToast.app
```

`codesign --verify --deep --strict` passes; `spctl --assess` does not. That is
the only thing a paid Apple certificate would change.

## Quick start (CLI)

```bash
tar -xzf marktoast-<version>-<os>-<arch>.tar.gz
./marktoast init                 # creates ~/Memory and a git repo
./marktoast serve                # hub at http://127.0.0.1:3777
./marktoast tui                  # read and search in the terminal
./marktoast adapters             # which harnesses are on this machine
```

`marktoast update` now works: it reads this release from GitHub.

## In this build

- Markdown is the only source of truth; every change is a Git commit
- SQLite FTS5 index with CJK bigram tokenization; substring search for two-character Chinese
- Local hub: HTTP API + MCP over loopback, bearer token, no cloud, no telemetry
- The wall: equal-height card grid, rendered Markdown reader, popover filters,
  ⌘K search, reading size, auto-loading
- 40 harness adapters (Claude Code, Codex, Gemini CLI, DSH, Hermes, Pi, …) —
  descriptor-driven, so adding one is configuration
- Session capture into an inbox of candidate memories

## Known limits

- The desktop window needs a system toolkit: GTK 4 + WebKitGTK 6.0 on Linux
  (Debian/Ubuntu: `sudo apt install libgtk-4-1 libwebkitgtk-6.0-4`), the
  WebView2 runtime on Windows. The shell checks for these on Linux and tells
  you the package if they are missing. The CLI has no such dependency.
- The Windows build is signed by nobody (SmartScreen will warn); macOS is
  ad-hoc signed, not notarized (see above)
- Pre-alpha: on-disk formats may still change. Run `marktoast backup` before
  upgrading, and read the notes of the next release
