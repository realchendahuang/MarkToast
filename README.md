<div align="center">

# MarkToast · 马克吐司

**Every agent's memory. One place. Yours.**

Local-first, Git-versioned Markdown memory for every coding agent.<br>
ZCode, Hermes and DSH first — Codex, Claude Code, Pi and the rest next.

[中文](README.zh-CN.md) · [Product](docs/PRODUCT.md) · [Tech stack](docs/TECH-STACK.md) · [Architecture](docs/ARCHITECTURE.md) · [Roadmap](docs/ROADMAP.md)

</div>

---

## Why MarkToast

Every harness remembers differently. Claude Code keeps notes, Codex keeps a
different file, ZCode invents a third format, DSH plugins invent a fourth — and
when you switch tools, the memory stays behind. Your memory ends up fragmented,
tied to one tool, and invisible to the next agent.

MarkToast treats memory as what it should be: **a repository you own.**

- **One Markdown repository is the source of truth.** Readable, greppable,
  diffable, editable in Obsidian.
- **Git is the workflow.** Every memory change is a diff. Branches are memory
  experiments. History is an audit trail. Rollback is `git revert`, not a
  feature request.
- **A local hub serves every harness.** Instruction-file projection where the
  harness reads files, MCP where it speaks MCP, native plugins where it allows
  one.
- **Federation over migration.** Adapters talk to each harness's own memory
  where possible; import is a fallback, not a requirement.
- **Local-first, zero cloud.** No account, no telemetry. Cross-machine sync is
  your own Git remote.
- **Chinese-first.** CJK-aware full-text search is a first-class feature, not
  an afterthought.

## What it is (and is not)

| It is | It is not |
|---|---|
| A local memory hub + desktop app | A cloud memory API |
| Markdown files + Git history | An opaque vector database |
| Adapters for the harnesses you use | A model gateway / router |
| A small, focused tool | A framework with 50 integrations |

## The name

**MarkToast** = *Mark* + *Toast*.

- **Mark** — mark what matters; Markdown is the source of truth.
- **Toast** — raise a toast when the next agent picks up where you left off.
- **马克吐司** — a wink at **马克·吐温** (Mark Twain). Chinese name, English soul.

## Status

Private **pre-alpha** (M0: foundations). The public repo, releases and docs
site come after the core is usable. See [docs/ROADMAP.md](docs/ROADMAP.md).

## License

TBD — private during development, to be open-sourced later.
