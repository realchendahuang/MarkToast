# MarkToast 0.2.0 — pre-alpha

One memory for every agent: a local-first, Git-versioned Markdown memory hub
that 40 coding harnesses read and write. Memories are plain `.md` files in a
repository you own; Git is the history, SQLite is the index, and the hub
speaks HTTP and MCP on `127.0.0.1` only.

## Highlights in 0.2.0

- **Web UI & Magpie UX upgrade**: Fixed Light theme contrast & segmented-control thumb alignment; added inline memory editing (with `previousPath` rename cleanup & unsaved-dirty guard), delete confirmation (`DELETE /api/memory`), Reader / Raw Markdown toggle, top-layer `⌘K` command palette, `?` shortcuts overlay, instant 40-adapter search/filter pills, IME composition guards, and full EN/ZH coverage.
- **Core engine & CJK search**: Rewrote FTS5 query building for non-adjacent CJK compound recall, eliminated punctuation/version false scans, made `lockfile` goroutine-reentrant + cross-goroutine exclusive, and hardened `gitx` (fallback identity, repo-root boundary checks, `/inbox/` exclusion), `ingest` (>64KB JSONL streaming), `backup.Restore` (atomic + Tar Slip protection), and `redact` (17 credential patterns).
- **MCP & HTTP hub compliance**: Added `drainingStdioTransport` (16 MiB frames + EOF in-flight drain), proper Streamable HTTP JSON-RPC `-32601` / `202 Accepted` notification handling, `committer` race & `AutoCommit=false` fixes, constant-time token comparison, and `config.Load`/`Save` path expansion & atomic writes.
- **CLI, CJK TUI & 40 adapters**: Closed repo-boundary path traversal in `get`/`diff`/adapter IDs, added `delete` (`rm`) and `--body -` stdin support, removed CWD `./adapters` hijacking, fixed JSONC/comment-preserving TOML merging and valid JSON staged snippets, added CJK double-width alignment in TUI, and expanded `doctor` diagnostics.

## Downloads

| Platform | Asset | Notes |
|---|---|---|
| macOS, Apple Silicon | `MarkToast-desktop-0.2.0-macos-arm64.dmg` / `.zip` | desktop app (hub included) |
| macOS, Intel | `MarkToast-desktop-0.2.0-macos-amd64.dmg` / `.zip` | same app, Intel build |
| Linux, arm64 / x86_64 | `MarkToast-desktop-0.2.0-linux-<arch>.tar.gz` | desktop app; needs GTK 4 + WebKitGTK 6.0 |
| Windows, arm64 / x86_64 | `MarkToast-desktop-0.2.0-windows-<arch>.zip` | desktop app; needs the WebView2 runtime |
| macOS, Linux, Windows | `marktoast-0.2.0-<os>-<arch>.tar.gz` (`.zip` on Windows) | CLI only, no window toolkit needed |

Both macOS apps are **ad-hoc signed, not notarized**: these builds use no
Developer ID. Gatekeeper will refuse the first launch. Either right-click → *Open*,
or:

```bash
xattr -d com.apple.quarantine /Applications/MarkToast.app
```

`codesign --verify --deep --strict` passes; `spctl --assess` does not.

## Quick start (CLI)

```bash
tar -xzf marktoast-0.2.0-<os>-<arch>.tar.gz
./marktoast init                 # creates ~/Memory and a git repo
./marktoast serve                # hub at http://127.0.0.1:3777
./marktoast tui                  # read and search in the terminal
./marktoast adapters             # which harnesses are on this machine
```

## Known limits

- The desktop window needs a system toolkit: GTK 4 + WebKitGTK 6.0 on Linux
  (Debian/Ubuntu: `sudo apt install libgtk-4-1 libwebkitgtk-6.0-4`), the
  WebView2 runtime on Windows.
- The Windows build is unsigned (SmartScreen will warn); macOS is
  ad-hoc signed, not notarized (see above).
- Pre-alpha: on-disk formats may still change. Run `marktoast backup` before
  upgrading.
