<div align="center">

# MarkToast · 马克吐司

**Every agent's memory. One place. Yours.**

Local-first, Git-versioned Markdown memory for every coding agent.<br>
ZCode, Hermes and DSH first — Codex, Claude Code, Pi and the rest next.

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![Wails](https://img.shields.io/badge/Wails-v3-DF0000?logo=wails&logoColor=white)
![macOS](https://img.shields.io/badge/macOS-first-000000?logo=apple&logoColor=white)
![Status](https://img.shields.io/badge/status-pre--alpha-d8b26a)
![License](https://img.shields.io/badge/license-MIT%20(later)-lightgrey)

[中文](README.zh-CN.md) · [Product](docs/PRODUCT.md) · [Tech stack](docs/TECH-STACK.md) · [Architecture](docs/ARCHITECTURE.md) · [Roadmap](docs/ROADMAP.md) · [Credits](docs/CREDITS.md)

</div>

---

## Why MarkToast

Every harness remembers differently. Claude Code keeps notes, Codex keeps a
different file, ZCode invented a third format, DSH plugins a fourth — and when
you switch tools, the memory stays behind. Your memory ends up fragmented, tied
to one tool, and invisible to the next agent.

MarkToast treats memory as what it should be: **a repository you own.**

- **One Markdown repository is the source of truth.** Readable, greppable,
  diffable, editable in Obsidian.
- **Git is the workflow.** Every memory change is a diff. Branches are memory
  experiments. History is an audit trail. Rollback is `git revert`.
- **A local hub serves every harness.** Instruction-file projection where the
  harness reads files, MCP where it speaks MCP, native plugins where it allows
  one.
- **Federation over migration.** Adapters talk to each harness's own memory
  where possible; import is a fallback, not a requirement.
- **Local-first, zero cloud.** No account, no telemetry. Cross-machine sync is
  your own Git remote.
- **Chinese-first.** CJK-aware full-text search is a first-class feature.

## Architecture

```
                        ┌──────────────────────────────┐
                        │  Markdown memory repository  │  ← single source of truth
                        │  projects/<key>/memory/      │    (Git-versioned, human-readable)
                        │  global/                     │
                        └──────────────┬───────────────┘
                                       │  watchers / atomic writes / flock
                        ┌──────────────┴───────────────┐
                        │        core (Go)             │
                        │  store · index (FTS5+CJK)    │
                        │  git (batched commits)       │
                        │  hub (HTTP + MCP)            │
                        └──────┬───────────────┬───────┘
                               │               │
                      CLI `marktoast`     Wails v3 desktop (M4)
                               │
             ┌─────────────────┴──────────────────┐
             │  adapters (YAML descriptors)       │
             │  T1 instruction projection         │  AGENTS.md / CLAUDE.md marker blocks
             │  T2 session ingest                 │  read-only candidate memories
             │  T3 MCP tools                      │  any MCP client
             │  T4 native plugins                 │  ZCode hooks · Hermes provider · DSH plugin
             └────────────────────────────────────┘
                               │
                    optional: your own git remote (cross-machine, teams)
```

## How it works

1. `marktoast init` creates `~/Memory` (or `--root`) and `git init`s it.
2. Memories are one Markdown file each, with YAML frontmatter — the ZCode
   project-memory format, so existing memories import without conversion.
3. `MEMORY.md` is a generated index; the SQLite FTS5 index is derived and
   rebuildable (`marktoast reindex`).
4. `marktoast serve` exposes the local hub; each harness adapter projects the
   index into that harness's own instruction/memory surface.
5. `git log` is your memory's history. `git revert` is your undo.

## Status

**Private pre-alpha — M0 done, M1 in progress.**

- [x] Brand, product positioning, tech-stack decisions, architecture docs
- [x] Go module + CLI (`version`, `init`, `status`, `search`, `reindex`,
      `adapters list`, `serve`)
- [x] Embedded adapter descriptors (ZCode, DSH, Hermes, Codex, Claude Code)
- [x] Local hub with `/health` + `/api/status`; vanilla Web UI preview
- [ ] SQLite FTS5 + CJK indexing (M1 spike)
- [ ] Git batched commits + `diff`/`rollback` (M1)
- [ ] Adapter projection/import for the first three harnesses (M2)
- [ ] MCP server (M3), Wails v3 shell (M4)

See [docs/ROADMAP.md](docs/ROADMAP.md).

## Quick start (development)

```bash
make build
./bin/marktoast version
./bin/marktoast init                 # creates ~/Memory + git repo
./bin/marktoast status
./bin/marktoast search "发版"
./bin/marktoast serve                # http://127.0.0.1:3777 (web UI preview)
```

Requirements: Go 1.26+, git. macOS first; Windows/Linux keep building but are
not v1 targets.

## Standing on shoulders · 致敬

MarkToast is built in public debt to these projects — see
[docs/CREDITS.md](docs/CREDITS.md) for exactly what we borrow.

- **[magpie](https://github.com/yetone/magpie)** (MIT) — the product shape we
  aspire to: Go + Wails v3, system WebView, vanilla frontend, local hub, config
  writers for every agent.
- **[ai-memory](https://github.com/akitaonrails/ai-memory)** (MIT) — Markdown
  source of truth, derived index, Git-backed memory engine ideas.
- **[ZCode](https://github.com/zai-org/ZCode)** (Apache-2.0) — the project
  memory format MarkToast adopts (`MEMORY.md` index + one Markdown file per
  fact + frontmatter).
- **[Hermes Agent](https://github.com/NousResearch/hermes-agent)** (MIT) and
  **[DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness)** (MIT)
  — the harness integration surfaces (memory provider, plugin/MCP seams).
- **[EverOS](https://github.com/EverMind-AI/EverOS)** (Apache-2.0),
  **[memsearch](https://github.com/zilliztech/memsearch)** (MIT) — local-first
  Markdown memory and hybrid retrieval precedents.
- **[Wails](https://github.com/wailsapp/wails)** (MIT), **[moejs](https://github.com/MoeForge/moejs)**
  (Apache-2.0) — the desktop shell and future plugin engine.

## Docs

| Doc | What |
|---|---|
| [Product](docs/PRODUCT.md) | Positioning, users, non-goals, success metrics |
| [Tech stack](docs/TECH-STACK.md) | Every decision, with alternatives and risks |
| [Architecture](docs/ARCHITECTURE.md) | Components, storage layout, adapters, failure modes |
| [Roadmap](docs/ROADMAP.md) | M0–M5 |
| [Credits](docs/CREDITS.md) | Who we learned from, and under which license |
| [Desktop](docs/DESKTOP.md) | Wails v3 plan (M4) |
| [Adapters](adapters/README.md) | How to add a harness |

## License

Private during development; **MIT** when open-sourced. See the decision record
in [docs/TECH-STACK.md](docs/TECH-STACK.md).
