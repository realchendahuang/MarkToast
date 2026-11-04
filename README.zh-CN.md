<div align="center">

# MarkToast · 马克吐司

**每个 agent 的记忆，一处存放，归你所有。**

本地优先、Git 版本化的 Markdown 记忆中枢，适配所有 coding agent。<br>
先接 ZCode、Hermes、DSH，再扩到 Codex、Claude Code、Pi 等。

[English](README.md) · [产品定位](docs/PRODUCT.md) · [技术栈](docs/TECH-STACK.md) · [架构设计](docs/ARCHITECTURE.md) · [路线图](docs/ROADMAP.md)

</div>

---

## 为什么需要 MarkToast

每个 harness 都在记，但各记各的：Claude Code 有自己的一套笔记，Codex 是另一个文件，
ZCode 发明了第三种格式，DSH 的插件生态里又有第四种。你换个工具，记忆就留在上一个工具里
——它被工具绑架、被格式割裂、对下一个 agent 不可见。

MarkToast 把记忆还原成它本该有的样子：**一个你拥有的仓库。**

- **一份 Markdown 仓库就是唯一真源。** 可读、可 grep、可 diff，Obsidian 直接打开。
- **Git 就是工作流。** 每次记忆变更都是一次 diff；分支是记忆实验；历史是审计线；
  回滚是 `git revert`，不需要等一个功能。
- **一个本地中枢服务所有 harness。** 能读指令文件的就投影进去，支持 MCP 的走 MCP，
  有插件机制的上原生插件。
- **先联邦，后搬家。** 适配器优先对接各家原生记忆；导入只是兜底，不是前提。
- **本地优先，零云端。** 不要账号、不上报数据；跨机同步用你自己的 Git remote。
- **中文优先。** CJK 分词与子串检索是一等公民，不是补丁。

## 名字的来历

**MarkToast = Mark + Toast。**

- **Mark** —— 给值得记住的事打个标；Markdown 是记忆的载体。
- **Toast** —— 下一个 agent 接上你的活时，值得碰一杯。
- **马克吐司** —— 中文名字致敬 **马克·吐温**（Mark Twain）。中文的壳，英文的骨。

## 现状

私有 **pre-alpha**（M0：打地基）。核心可用之后再公开仓库、发版、上文档站。
进度见 [docs/ROADMAP.md](docs/ROADMAP.md)。

## 许可证

待定 —— 开发期私有，之后开源。
