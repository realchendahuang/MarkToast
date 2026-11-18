// MarkToast i18n — dependency-free DOM translation layer.
//
// Chinese is the authored UI source language; this layer maps exact UI phrases
// and structured status patterns to idiomatic English while leaving user-owned
// content ([data-no-i18n], Markdown bodies, code blocks, diffs) untouched.
(function () {
  "use strict";

  const SKIP_TEXT = [
    "script", "style", "pre", "textarea",
    ".markdown-body", ".raw-source-pre", ".diff-box", ".snippet-pre",
    ".expanded-project", ".alias-key", ".memory-project", "[data-no-i18n]",
  ].join(",");

  const SKIP_ATTR = "script, style, [data-no-i18n]";
  const TRANSLATED_ATTRS = ["placeholder", "aria-label", "title", "data-part"];
  const SINGULAR_PROJECT_CTX = ".type-pill, #type-filter-button, #type-filter, #memory-type, [data-singular-project]";

  const en = {
    // Navigation & Views
    "记忆": "Memory",
    "全部记忆": "All memories",
    "收件箱": "Inbox",
    "适配器": "Adapters",
    "历史": "History",
    "项目": "Projects",
    "设置": "Settings",
    "记忆库": "Library",
    "记忆仓库导航": "Memory repository navigation",
    "MarkToast 首页": "MarkToast home",
    "当前位置": "Current view",
    "主导航": "Main navigation",
    "侧栏": "Sidebar",
    "切换侧栏": "Toggle sidebar",
    "展开侧栏": "Expand sidebar",
    "收起侧栏": "Collapse sidebar",
    "侧栏宽度": "Sidebar width",

    // Connection & Status
    "连接中": "Connecting",
    "正在连接本地 hub": "Connecting to local hub",
    "已连接到本地 hub": "Connected to local hub",
    "等待本地 hub": "Waiting for local hub",
    "在线": "Online",
    "离线": "Offline",
    "本地 hub 未连接": "Local hub is offline",
    "无法连接本地 hub。": "Can't reach the local hub.",
    "无法连接本地 hub。请确认 `marktoast serve` 正在运行。": "Can't reach the local hub. Make sure `marktoast serve` is running.",
    "无法连接本地 hub。请运行 `marktoast serve` 后重试。": "Can't reach the local hub. Run `marktoast serve` and try again.",
    "本地 hub 暂时不可用，请稍后重试。": "The local hub is temporarily unavailable; try again shortly.",
    "本地 hub 返回了无法解析的响应。": "The local hub returned an unreadable response.",
    "未授权，请重新连接本地 hub。": "Unauthorized; reload to reconnect to the local hub.",
    "没有权限执行此操作。": "You don't have permission to perform this action.",
    "请求超时，请稍后重试。": "Request timed out; try again.",
    "请求已取消。": "Request cancelled.",
    "请求失败，请稍后重试。": "Request failed; try again.",
    "不可用": "Unavailable",
    "重试": "Retry",
    "重新连接": "Reconnect",
    "刷新": "Refresh",
    "刷新当前视图": "Refresh view",
    "已刷新": "Refreshed",
    "刷新失败，请确认本地 hub 正在运行": "Refresh failed; make sure the local hub is running",
    "处理中…": "Working…",
    "加载中…": "Loading…",
    "正在加载…": "Loading…",
    "加载更多": "Load more",
    "加载更多失败": "Failed to load more",
    "点击重试": "Retry",

    // Actions & Common Labels
    "新建": "New",
    "新建记忆": "New memory",
    "新建第一条记忆": "Write first memory",
    "编辑记忆": "Edit memory",
    "编辑": "Edit",
    "编辑这条记忆": "Edit this memory",
    "删除": "Delete",
    "删除记忆": "Delete memory",
    "删除这条记忆": "Delete this memory",
    "保存": "Save",
    "快速保存": "Quick save",
    "正在保存…": "Saving…",
    "已保存": "Saved",
    "未保存": "Unsaved",
    "记忆已保存": "Memory saved",
    "记忆已更新": "Memory updated",
    "记忆已删除": "Memory deleted",
    "当前本地 hub 版本暂未启用删除接口。": "This local hub build does not support deleting memories yet.",
    "放弃未保存的修改？": "Discard unsaved changes?",
    "保存失败，请检查表单和本地 hub。": "Save failed; check the form and the local hub.",
    "取消": "Cancel",
    "关闭": "Close",
    "复制": "Copy",
    "已复制": "Copied",
    "复制失败": "Copy failed",
    "复制失败，请稍后再试": "Copy failed; try again",
    "复制失败，请手动选择路径": "Copy failed; copy the path manually",
    "复制名称": "Copy name",
    "名称已复制": "Name copied",
    "复制正文": "Copy body",
    "正文已复制": "Body copied",
    "复制代码": "Copy code",
    "代码已复制": "Code copied",
    "复制补丁": "Copy diff",
    "补丁已复制": "Diff copied",
    "复制片段": "Copy snippet",
    "片段已复制": "Snippet copied",
    "复制命令": "Copy command",
    "命令已复制": "Command copied",
    "复制记忆路径": "Copy memory path",
    "路径已复制到剪贴板": "Path copied to clipboard",
    "复制 Markdown": "Copy Markdown",
    "复制这条记忆的 Markdown": "Copy this memory's Markdown",
    "Markdown 已复制到剪贴板": "Markdown copied to clipboard",
    "这条记忆还没加载完": "This memory is still loading",
    "根目录已复制": "Root path copied",
    "复制根目录": "Copy root path",
    "MCP 地址已复制": "MCP URL copied",
    "复制 MCP 地址": "Copy MCP URL",
    "Token已复制": "Token copied",
    "Token 已复制": "Token copied",
    "复制 MCP token": "Copy MCP token",

    // Search, Filters & Sort
    "搜索": "Search",
    "搜索记忆": "Search memories",
    "搜索记忆…": "Search memories…",
    "搜索结果": "Search results",
    "清空搜索": "Clear search",
    "清空输入": "Clear input",
    "记忆筛选": "Memory filters",
    "项目筛选": "Filter by project",
    "类型筛选": "Filter by type",
    "全部项目": "All projects",
    "全部类型": "All types",
    "搜索项目": "Search projects",
    "没有匹配的项目": "No matching projects",
    "没有匹配项": "No matches",
    "没有匹配": "No matches",
    "没有匹配的记忆": "No matching memories",
    "清除筛选": "Clear filters",
    "清除筛选条件": "Clear filters",
    "排序": "Sort",
    "切换排序": "Change sort",
    "相关度": "Relevance",
    "最近更新": "Recently updated",
    "标题": "Title",

    // Memory Types
    "用户": "User",
    "反馈": "Feedback",
    "参考": "Reference",
    "决策": "Decision",
    "未归类": "Unsorted",
    "未命名记忆": "Untitled memory",

    // Empty States
    "还没有记忆": "No memories yet",
    "先写一条。": "Write the first one.",
    "在这里沉淀偏好、架构决策与项目上下文，接入的 Agent 可随时按需检索。": "Capture preferences, architectural decisions, and project context here so every connected agent can recall them on demand.",
    "换个关键词或调整筛选条件试试。": "Try another keyword or clear active filters.",
    "换个关键词试试。": "Try another keyword.",
    "配置适配器": "Set up adapters",
    "查看适配器": "View adapters",
    "暂无项目": "No projects yet",
    "还没有项目": "No projects yet",
    "记忆加载失败": "Failed to load memories",

    // Form & Editor
    "关闭新建记忆弹层": "Close memory editor",
    "关闭记忆编辑弹层": "Close memory editor",
    "所属项目": "Project",
    "类型": "Type",
    "名称": "Name",
    "描述": "Description",
    "可选": "Optional",
    "正文": "Body",
    "编辑器视图": "Editor mode",
    "编写": "Write",
    "预览": "Preview",
    "暂无可预览的正文": "Nothing to preview yet.",
    "例如：api-error-conventions 或 发布检查清单": "e.g. api-error-conventions or release-checklist",
    "一句话概括上下文，便于检索命中": "One-line summary to improve search recall",
    "支持 Markdown 格式编写记忆正文…": "Write this memory in Markdown…",
    "为搜索补充一点上下文": "Extra context for search",
    "写下这条记忆…": "Write this memory…",
    "名称不能为空。": "Name is required.",
    "名称不能超过 200 个字符。": "Name cannot exceed 200 characters.",
    "描述不能超过 2,000 个字符。": "Description cannot exceed 2,000 characters.",
    "正文不能为空。": "Body is required.",
    "正文大小不能超过 2 MiB。": "Body cannot exceed 2 MiB.",

    // Memory Reading Sheet & Version History
    "关闭记忆详情": "Close memory details",
    "阅读": "Rendered",
    "源码": "Raw",
    "切换阅读与源码视图": "Switch rendered and raw views",
    "上一条记忆": "Previous memory",
    "下一条记忆": "Next memory",
    "按此项目筛选": "Filter by this project",
    "路径": "Path",
    "更新": "Updated",
    "节点": "Node",
    "会话": "Session",
    "字数": "Length",
    "图片": "Image",
    "没有正文。": "No body.",
    "收起": "Collapse",
    "展开": "Expand",
    "收起记忆": "Collapse memory",
    "展开记忆": "Expand memory",
    "版本历史": "Version history",
    "查看这条记忆的版本历史": "View version history",
    "还没有提交记录": "No commits yet",
    "恢复中…": "Restoring…",
    "恢复此版本": "Restore this version",
    "恢复这个版本？会生成一次新的提交。": "Restore this version? A new Git commit will be created.",
    "已恢复": "Restored",
    "没有差异内容": "No diff changes",

    // Inbox
    "收件箱加载失败": "Failed to load inbox",
    "收件箱是空的": "Inbox is empty",
    "会话结束后，候选记忆会出现在这里。": "Candidates show up here after a session ends.",
    "Agent 会话结束或触发摄取后，待审阅的候选记忆会出现在这里。": "Candidate memories captured from agent sessions appear here for review.",
    "归入项目": "Target project",
    "候选记忆": "Candidate",
    "保留": "Keep",
    "丢弃": "Discard",
    "已保留": "Kept",
    "丢弃这条候选记忆？": "Discard this candidate?",
    "已丢弃": "Discarded",

    // Adapters (40 Harnesses)
    "适配器加载失败": "Failed to load adapters",
    "没有适配器": "No adapters",
    "没有匹配的适配器": "No matching adapters",
    "本地 hub 没有报告可用的 Agent。": "The local hub reported no agents.",
    "适配器筛选": "Adapter filters",
    "筛选适配器": "Filter adapters",
    "筛选适配器（名称、ID、配置路径）…": "Filter adapters (name, ID, config path)…",
    "清空适配器筛选": "Clear adapter filter",
    "适配器状态与模式筛选": "Filter adapters by status or mode",
    "全部": "All",
    "已接入": "Connected",
    "待接入": "Needs setup",
    "已检测": "Detected",
    "未检测": "Not detected",
    "原生": "Native",
    "投影": "Projected",
    "只读": "Read-only",
    "自动": "Automatic",
    "片段": "Snippets",
    "仅 MCP": "MCP only",
    "自动接入": "Automatic install",
    "片段安装": "Snippet install",
    "自动 · 自动接入": "Automatic · Automatic install",
    "片段 · 片段安装": "Snippets · Snippet install",
    "仅 MCP · 仅 MCP": "MCP only",
    "接入": "Connect",
    "修复": "Repair",
    "写入": "Write",
    "重新写入": "Rewrite",
    "移除配置": "Uninstall",
    "生成片段": "Generate snippets",
    "生成合并片段": "Generate merge snippets",
    "已生成片段": "Snippets ready",
    "片段已生成": "Snippets generated",
    "片段已生成，路径已复制": "Snippets generated; path copied",
    "片段已准备，路径已复制": "Snippets ready; path copied",
    "查看说明": "Details",
    "关闭说明": "Close details",
    "接入说明": "Integration details",
    "说明": "Notes",
    "接入方式": "Install mode",
    "层级": "Tier",
    "状态": "Status",
    "检测路径": "Detected path",
    "Hook 事件": "Hook events",
    "Hook 目标文件": "Hook target",
    "注入方式": "Injection",
    "安装类型": "Install kind",
    "MCP 方式": "MCP transport",
    "MCP 目标文件": "MCP target",
    "片段目录": "Snippet directory",
    "已生成文件": "Generated files",
    "CLI 命令": "CLI command",
    "MCP 配置示例": "MCP config snippet",
    "配置已更新": "Configuration updated",
    "已移除 MarkToast 配置": "MarkToast configuration removed",
    "已降级为片段安装": "Downgraded to snippet install",
    "移除这家 harness 中的 MarkToast 配置？会先备份原文件。": "Remove MarkToast entries from this harness config? The file is backed up first.",
    "写入这家 harness 的配置？会先备份原文件。": "Write this harness's config? The current file is backed up first.",
    "重新写入这家 harness 的配置？会先备份原文件。": "Rewrite this harness's config? The current file is backed up first.",
    "MarkToast 直接合并这家 harness 的配置；写前备份，保留第三方条目。": "MarkToast merges this harness's config directly; it backs the file up first and preserves third-party entries.",
    "MarkToast 生成可直接粘贴的 hooks / MCP 片段，由你手动合并。": "MarkToast writes ready-to-paste hooks / MCP snippets for you to merge manually.",
    "这家只支持 MCP：生成 MCP 片段，不安装生命周期 hooks。": "This harness supports MCP only: MarkToast writes an MCP snippet without lifecycle hooks.",

    // Git History
    "Git 历史不可用": "Git history unavailable",
    "Git 不可用": "Git unavailable",
    "当前仓库没有可读取的 Git 历史。": "This repository has no readable Git history.",
    "还没有提交": "No commits yet",
    "保存记忆后会在这里出现。": "Commits appear here after you save a memory.",
    "每次保存或恢复记忆都会自动记录在本地 Git 历史中。": "Every saved or restored memory is automatically tracked in local Git history.",
    "无提交说明": "No commit message",
    "复制提交哈希": "Copy commit hash",
    "提交哈希已复制": "Commit hash copied",

    // Settings
    "常规": "General",
    "仓库": "Repository",
    "导入": "Import",
    "项目别名": "Project aliases",
    "查找设置": "Find a setting",
    "没有匹配的设置": "No matching settings",
    "外观": "Appearance",
    "主题": "Theme",
    "跟随系统": "System",
    "浅色": "Light",
    "深色": "Dark",
    "跟随系统时，随系统的浅色或深色外观自动切换": "Follows your system appearance when set to System",
    "跟随系统时，随 macOS 的浅色或深色自动切换": "Follows macOS when set to System",
    "语言": "Language",
    "界面文案的语言，不影响记忆内容": "Interface language; memory content stays as written",
    "阅读字号": "Reading size",
    "字号": "Text size",
    "还原字号": "Reset text size",
    "一篇记忆正文的字号，标题跟着一起放大": "Body font size when reading a memory; headings scale proportionally",
    "小": "Small",
    "标准": "Default",
    "大": "Large",
    "快捷键": "Shortcuts",
    "键盘快捷键": "Keyboard shortcuts",
    "快捷键速查": "Keyboard shortcuts",
    "关闭快捷键速查": "Close keyboard shortcuts",
    "窗口里随时可用；输入框里不抢键": "Available anywhere in the window without hijacking text fields",
    "全局与导航": "Global & Navigation",
    "记忆浏览与编辑": "Reading & Editing",
    "命令面板": "Command palette",
    "打开命令面板": "Open command palette",
    "命令面板与全局搜索": "Command palette & global search",
    "打开命令面板 / 全局搜索": "Open command palette / global search",
    "聚焦顶部搜索框": "Focus toolbar search",
    "上下选卡片": "Previous / next card",
    "前一条 / 后一条记忆": "Previous / next memory",
    "网格选卡片": "Navigate grid",
    "网格方向键选卡片": "Navigate card grid",
    "选卡片": "Pick a card",
    "编辑当前记忆": "Edit open memory",
    "编辑当前打开的记忆": "Edit open memory",
    "在编辑器中保存记忆": "Save in editor",
    "打开设置": "Open settings",
    "打开偏好设置": "Open preferences",
    "收起 / 展开侧栏": "Collapse / expand sidebar",
    "显示快捷键速查": "Show keyboard shortcuts",
    "放大 / 缩小 / 还原阅读字号": "Zoom in / out / reset reading size",
    "关闭弹层": "Close dialog",
    "关详情": "Close memory",
    "关闭当前弹层或清空搜索": "Close active dialog or clear search",
    "回到顶部": "Scroll to top",
    "滚到底部": "Scroll to bottom",
    "根目录": "Root",
    "记忆仓库所在的文件夹": "Folder storing the Markdown memory repository",
    "记忆总数": "Total memories",
    "仓库里的记忆总数": "Total memories stored in the repository",
    "索引": "Search index",
    "重建索引": "Rebuild index",
    "用于全文搜索，重建不会改动记忆": "SQLite FTS index for search; rebuilding never alters Markdown files",
    "未建立": "Not built",
    "备份": "Backup",
    "把仓库打包成一份 .tar.gz 归档，留在本机": "Packs the repository into a local .tar.gz archive",
    "备份已创建": "Backup created",
    "HTTP 端点": "HTTP endpoint",
    "本地 Streamable HTTP MCP 服务地址": "Local Streamable HTTP MCP endpoint",
    "给 MCP 客户端连接本地记忆库用的令牌": "Bearer token for MCP clients connecting to this repository",
    "从 ~/.zcode/cli/memories 导入": "Import from ~/.zcode/cli/memories",
    "从 ~/.hermes 导入": "Import from ~/.hermes",
    "导入中…": "Importing…",
    "没有可导入的记忆": "Nothing to import",
    "给项目起一个更好记的显示名，保存后侧栏和列表都会用它": "Assign friendly display names to projects across the sidebar and cards",
    "显示名": "Display name",

    // Command Palette (#cmdk-modal)
    "搜索记忆、项目、适配器或执行命令…": "Search memories, projects, adapters, or run a command…",
    "搜索记忆或输入命令": "Search memories or type a command",
    "搜索与命令结果": "Search and command results",
    "导航": "Navigate",
    "打开 / 执行": "Open / Run",
    "在列表中筛选": "Filter wall",
    "快捷命令": "Commands",
    "匹配记忆": "Matching memories",
    "最近记忆": "Recent memories",
    "匹配项目": "Projects",
    "匹配适配器": "Adapters",
    "没有找到匹配的记忆或命令": "No matching memories or commands",
    "前往全部记忆": "Go to All memories",
    "前往收件箱": "Go to Inbox",
    "前往适配器中心": "Go to Adapters",
    "前往 Git 历史": "Go to History",
    "切换到浅色外观": "Switch to Light appearance",
    "切换到深色外观": "Switch to Dark appearance",
    "外观跟随系统": "Use System appearance",
    "切换界面语言为 English": "Switch language to English",
    "切换界面语言为 中文": "Switch language to Chinese",
    "立即创建仓库备份": "Create repository backup now",
    "重建全文搜索索引": "Rebuild full-text search index",

    // Time
    "未知时间": "Unknown time",
    "刚刚": "just now",

    // All 40 Built-in Adapter Notes
    "别名 agy/antigravity；与 gemini-cli 共享 ~/.gemini 根但配置文件名不同（detect 有重叠，安装器按 id 区分）。技能根：~/.gemini/config/skills（App/IDE）与 ~/.gemini/antigravity-cli/skills（CLI），不读 ~/.agents/skills。": "Alias agy/antigravity; shares ~/.gemini with gemini-cli using distinct config filenames (installer disambiguates by id). Skill roots: ~/.gemini/config/skills (App/IDE) and ~/.gemini/antigravity-cli/skills (CLI).",
    "指令投影 + MCP + hooks；hooks 也可作为会话摄取来源。": "Instruction projection + MCP + hooks; hooks can also feed session ingestion.",
    "与 Claude Desktop Code tab（claude-desktop-code）是两个 id；CLI 管理的是本地 JSON 配置，不是账号级 connector 或 .mcpb 扩展。": "Distinct from Claude Desktop Code tab (claude-desktop-code); manages local JSON config rather than account connectors or .mcpb extensions.",
    "与 claude-code 共用 hooks 目标文件，装一次即覆盖；与 Chat tab（claude-desktop-chat）是两个不同 id。": "Shares the hooks target file with claude-code; distinct from the Chat tab (claude-desktop-chat).",
    "每事件一个 JSON 文件，不是单文件 schema，故 staged；VC 扩展 hooks 目录不同。": "Uses one JSON file per event rather than a single-file schema (staged); VS Code extension uses a separate hooks folder.",
    "腾讯 CodeBuddy Code；别名 code-buddy；无 native transcript backfill。": "Tencent CodeBuddy Code (alias code-buddy); no native transcript backfill.",
    "指令投影 + MCP + hooks；SessionEnd 版本差异要在适配器里探测。": "Instruction projection + MCP + hooks; SessionEnd version differences are probed by the adapter.",
    "可执行名 command-code（Windows 上 cmdc）；--session <uuid> 续接。": "Executable is command-code (cmdc on Windows); resumes with --session <uuid>.",
    "注意区分 VS Code Copilot（vs-code-copilot，MCP-only）与 Copilot CLI；别名 copilot-cli。": "Distinct from VS Code Copilot (vs-code-copilot, MCP-only); alias copilot-cli.",
    "Managed-only（ai-memory run crush）；无 hooks/MCP 安装器，MarkToast 只读会话库。": "Managed-only; no hooks/MCP installer — MarkToast reads the session store read-only.",
    "Cursor 不能 post-compaction 注入，压缩后靠已装 instructions 重新落地。": "Cursor cannot inject post-compaction; relies on installed instructions after context compaction.",
    "SessionEnd 可用（真实会话结束），无需 finalize-session；PostCompaction 是 Devin 独有事件。": "Supports real SessionEnd without finalize-session; PostCompaction is Devin-specific.",
    "无固定 MCP 配置文件；静态兜底是受管 AGENTS.md 区块（约 65536 bytes），但不能替代 MCP。": "No fixed MCP config file; static fallback is a managed AGENTS.md block (~65,536 bytes), which does not replace MCP.",
    "厂商拒绝把 hooks 写进 settings.json，专用 hooks.json 是唯一稳定入口；别名 droid。": "Hooks live exclusively in dedicated hooks.json (not settings.json); alias droid.",
    "配置文件在 ~/.agents（与共享 skills 同根）；stdio bridge 非 HTTP 条目。": "Config lives under ~/.agents alongside shared skills; uses a stdio bridge entry rather than HTTP.",
    "ai-memory 把 Gemini 归入 AGENTS.md 阵营（install_instructions.rs: Codex/OpenCode/Cursor/Gemini/Kimi Code 收敛到 AGENTS.md）；Gemini 自身也读 GEMINI.md，MarkToast 目前只投影 AGENTS.md，尚未覆盖 GEMINI.md。": "Grouped under AGENTS.md projection alongside Codex/OpenCode/Cursor/Kimi Code; Gemini also reads GEMINI.md (MarkToast currently projects AGENTS.md only).",
    "YAML 配置、extensions 容器、uri 键；Goose 不文档化 headers 环境变量展开。": "YAML config using extensions map and uri key; Goose does not document env expansion in headers.",
    "无可写配置，只有应用内 bridge，故为 staged。": "In-app bridge with no writable disk config (staged).",
    "Grok 只能通过 use_tool 元工具访问 MCP 工具，首轮规则文件是 handoff 可达的关键；Grok 不接受 SessionStart/UserPromptSubmit 注入。": "Grok calls MCP tools via its use_tool meta-tool, making first-turn rules essential; does not support SessionStart/UserPromptSubmit injection.",
    "内置 MEMORY.md/USER.md 用精确 \"\\n§\\n\" 分隔，2200/1375 字符预算；完整 preload/turn sync 需实现 standalone MemoryProvider 并关闭内置写入以避免双写。": "Built-in MEMORY.md/USER.md split on exact \"\\n§\\n\" (2200/1375-char budget); full preload/turn sync uses standalone MemoryProvider with built-in writes disabled.",
    "别名 bob；IDE 与 Bob Shell 共享 ~/.bob/settings。": "Alias bob; IDE and Bob Shell share ~/.bob/settings.",
    "shape 用 mcp（Kilo 的容器键，不是 mcp.servers）；JSONC 需注释安全写入。": "Uses top-level mcp container key (not mcp.servers); JSONC requires comment-preserving writes.",
    "别名 kimi；$KIMI_CODE_HOME 同时移动 mcp.json 与 config.toml。": "Alias kimi; $KIMI_CODE_HOME relocates both mcp.json and config.toml.",
    "v2/v3 是两个不兼容的 hook 注册格式；MarkToast 目前以 v2 片段为准，v3 尚未覆盖。": "v2 and v3 hook schemas are incompatible; MarkToast currently generates v2 snippets.",
    "别名 muse/muse-code；读 ~/.agents/skills 与 AGENTS.md，技能无需另装。": "Alias muse/muse-code; reads ~/.agents/skills and AGENTS.md directly.",
    "别名 omp/oh-my-pi；extension 与 mcp.json 必须落在同一 agent dir。": "Alias omp/oh-my-pi; extension and mcp.json must reside in the same agent directory.",
    "插件包没有单一配置目标，须经 openclaw plugins install 安装，故为 staged。": "Installed as a plugin package via openclaw plugins install rather than a single config file (staged).",
    "V1/V2 由 executable 版本探测；插件是 TS 源码，故 staged。": "V1/V2 detected by executable version; plugin ships as TypeScript source (staged).",
    "UI 内配置、无磁盘文件 → detect/target 都留空，MarkToast 只能展示接入说明。": "Configured inside the app UI with no on-disk config file; MarkToast provides setup instructions only.",
    "Pi 与 OMP 是不同 harness（extension 文件名与目录不同），不要合并 id。": "Pi and OMP are distinct harnesses with separate extension filenames and directories.",
    "别名 poolside；hooks 配置是项目级文件，MarkToast 只生成 staged 片段。": "Alias poolside; hooks config is project-scoped, so MarkToast generates staged snippets.",
    "bearerTokenEnvVar 是 Prime 特有字段，不能写 headers。": "Uses Prime-specific bearerTokenEnvVar field instead of custom HTTP headers.",
    "Qoder CLI 与 IDE 共享 ~/.qoder；读 AGENTS.md 与共享 ~/.agents/skills，routing/skills 无需 Qoder 专属副本。": "Qoder CLI and IDE share ~/.qoder and read AGENTS.md + ~/.agents/skills directly.",
    "别名 qwen/qwencode；读 AGENTS.md 与 QWEN.md，routing 片段无需 Qwen 专属文件；setup 在 ~/.qwen 存在时自动接线。": "Alias qwen/qwencode; reads AGENTS.md and QWEN.md and auto-wires when ~/.qwen is present.",
    "MCP-only，且配置在项目 checkout 内，故 staged 而非 mcp_only 自动写。": "MCP-only with repo-scoped config inside project checkouts (staged).",
    "注意与 copilot-cli（GitHub Copilot CLI，有 hooks）区分；--client copilot 的裸别名在过去指 VS Code Copilot。": "Distinct from copilot-cli (which supports hooks); bare copilot alias historically meant VS Code Copilot.",
    "MCP-only documented（未端到端验证）；Warp 与 Warp Agent CLI 是两份配置。": "MCP-only documented; Warp desktop and Warp Agent CLI use separate configurations.",
    "原生记忆：MEMORY.md 索引（≤200 行 / 25,000 字符）+ 一条事实一个 Markdown（frontmatter）；记忆根由 workspace 路径 SHA-256[:16] 推导。": "Native memory layout: MEMORY.md index (≤200 lines / 25,000 chars) + one Markdown fact per file; project key uses workspace SHA-256[:16].",
    "context_servers 是 Zed 独有的 shape（不是 mcpServers）。": "Uses Zed's context_servers config key rather than mcpServers.",
    "exec_list bundle（hooks.json）与 mcp.servers（config.json）的合并幂等、可回滚。注意 Zero 的 hook 沙箱仍要求用户放行网络或跑 hook-drain，安装器只改配置、不改沙箱策略。": "Idempotent, reversible merge for hooks.json and config.json; Zero's hook sandbox still requires network permission or hook-drain.",
  };

  const patterns = [
    [/^(\d+) 分钟前$/, (m) => `${m[1]} min ago`],
    [/^(\d+) 小时前$/, (m) => `${m[1]} hr ago`],
    [/^(\d+) 天前$/, (m) => `${m[1]} d ago`],
    [/^(\d+) 周前$/, (m) => `${m[1]} wk ago`],
    [/^(\d+) 个月前$/, (m) => `${m[1]} mo ago`],
    [/^(\d+) 年前$/, (m) => `${m[1]} yr ago`],
    [/^(\d+) 条记忆$/, (m) => `${Number(m[1]) === 1 ? "1 memory" : `${m[1]} memories`}`],
    [/^共 (\d+) 条$/, (m) => `${m[1]} total`],
    [/^显示 (\d+) \/ (\d+) 条$/, (m) => `Showing ${m[1]} of ${m[2]}`],
    [/^匹配 (\d+) 条$/, (m) => `${m[1]} matches`],
    [/^(\d+) 条待审阅$/, (m) => `${m[1]} pending review`],
    [/^已接入 (\d+) · 已检测 (\d+) · 共 (\d+) 个$/, (m) => `${m[1]} connected · ${m[2]} detected · ${m[3]} total`],
    [/^显示 (\d+) \/ (\d+) 个$/, (m) => `Showing ${m[1]} of ${m[2]}`],
    [/^(\d+) 条 · (.+)$/, (m) => `${m[1]} items · ${m[2]}`],
    [/^(\d+) 条$/, (m) => `${m[1]} items`],
    [/^(\d+) 字符$/, (m) => `${m[1]} chars`],
    [/^已导入 (\d+) 条$/, (m) => `Imported ${m[1]}`],
    [/^没有新记忆（跳过 (\d+) 条）$/, (m) => `Nothing new (${m[1]} skipped)`],
    [/^索引已重建 · (\d+) 条$/, (m) => `Index rebuilt · ${m[1]} memories`],
    [/^在记忆列表中查看「(.*)」的全部结果$/, (m) => `View all results for "${m[1]}" in memory wall`],
    [/^删除记忆「(.*)」？删除后可通过 Git 历史找回。$/, (m) => `Delete memory "${m[1]}"? You can still recover it from Git history.`],
    [/^收起记忆：(.*)$/, (m) => `Collapse memory: ${m[1]}`],
    [/^打开记忆：(.*)$/, (m) => `Open memory: ${m[1]}`],
    [/^展开记忆：(.*)$/, (m) => `Expand memory: ${m[1]}`],
    [/^项目 (.+) 的显示名$/, (m) => `Display name for project ${m[1]}`],
    [/^正文较长，仅显示前 (.+) 个字符。$/, (m) => `Long body; showing the first ${m[1]} characters.`],
    [/^(.*) · 恢复 (.*)$/, (m) => `${m[1]} · restored ${m[2]}`],
    [/^(.*) · 保存 (.*)$/, (m) => `${m[1]} · saved ${m[2]}`],
    [/^(.*) · 新增 (.*)$/, (m) => `${m[1]} · added ${m[2]}`],
    [/^(.*) · 删除 (.*)$/, (m) => `${m[1]} · deleted ${m[2]}`],
    [/^(.*)已复制$/, (m) => `${m[1]} copied`],
    [/^(.*)，但 Git 提交失败：(.*)$/, (m) => `${m[1]}, but Git commit failed: ${m[2]}`],
  ];

  const textOriginals = new WeakMap();
  const translatedTexts = new WeakMap();
  const attrOriginals = new WeakMap();
  const translatedAttrs = new WeakMap();
  let language = "zh";
  let observer = null;
  let busy = false;

  try {
    language = localStorage.getItem("marktoast-lang") === "en" ? "en" : "zh";
  } catch (error) {
    language = "zh";
  }

  function translate(value) {
    const text = String(value ?? "").trim();
    if (!text) return text;
    if (Object.prototype.hasOwnProperty.call(en, text)) return en[text];
    for (const [pattern, render] of patterns) {
      const match = text.match(pattern);
      if (match) return render(match);
    }
    return text;
  }

  function skipText(element) {
    if (!element) return true;
    if (element.closest && element.closest("[data-no-i18n]")) return true;
    if (element.closest && element.closest("[data-i18n]")) return false;
    if (element.tagName === "CODE" || (element.closest && element.closest("code"))) return true;
    return Boolean(element.closest && element.closest(SKIP_TEXT));
  }

  function skipAttr(element) {
    return !element || Boolean(element.closest && element.closest(SKIP_ATTR));
  }

  const singularTypes = {
    "用户": "User",
    "反馈": "Feedback",
    "项目": "Project",
    "参考": "Reference",
    "决策": "Decision",
  };

  function translateTextNode(node) {
    const parent = node.parentElement;
    if (skipText(parent)) return;
    const raw = node.nodeValue;
    const trimmed = raw.trim();
    if (!trimmed) return;

    if (language === "zh") {
      const original = textOriginals.get(node);
      if (original !== undefined) {
        const lastTranslated = translatedTexts.get(node);
        node.nodeValue = lastTranslated === undefined || raw === lastTranslated ? original : raw;
      }
      textOriginals.delete(node);
      translatedTexts.delete(node);
      return;
    }

    let mapped = trimmed;
    if (parent && parent.closest(SINGULAR_PROJECT_CTX) && singularTypes[trimmed]) {
      mapped = singularTypes[trimmed];
    } else {
      mapped = translate(trimmed);
    }

    const translated = mapped !== trimmed ? raw.replace(trimmed, () => mapped) : raw;
    const lastTranslated = translatedTexts.get(node);
    const isOwnTranslation = lastTranslated !== undefined && raw === lastTranslated;

    if (translated === raw) {
      if (!isOwnTranslation) {
        textOriginals.delete(node);
        translatedTexts.delete(node);
      }
      return;
    }
    if (!isOwnTranslation) textOriginals.set(node, raw);
    node.nodeValue = translated;
    translatedTexts.set(node, translated);
  }

  function translateAttributes(element) {
    if (skipAttr(element)) return;
    TRANSLATED_ATTRS.forEach((name) => {
      const value = element.getAttribute(name);
      if (!value) return;
      let stored = attrOriginals.get(element);
      if (!stored) {
        stored = {};
        attrOriginals.set(element, stored);
      }
      let translated = translatedAttrs.get(element);
      if (!translated) {
        translated = {};
        translatedAttrs.set(element, translated);
      }
      if (language === "zh") {
        if (stored[name] !== undefined) {
          const lastTranslated = translated[name];
          if (lastTranslated === undefined || value === lastTranslated) {
            element.setAttribute(name, stored[name]);
          }
        }
        delete stored[name];
        delete translated[name];
        return;
      }
      const lastTranslated = translated[name];
      const isOwnTranslation = lastTranslated !== undefined && value === lastTranslated;
      const rendered = translate(value);
      if (rendered === value.trim()) {
        if (!isOwnTranslation) {
          delete stored[name];
          delete translated[name];
        }
        return;
      }
      if (!isOwnTranslation) stored[name] = value;
      element.setAttribute(name, rendered);
      translated[name] = rendered;
    });
  }

  function translateNode(node) {
    if (!node) return;
    if (node.nodeType === Node.TEXT_NODE) {
      translateTextNode(node);
      return;
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return;
    translateAttributes(node);
    const walker = document.createTreeWalker(node, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT);
    let child;
    while ((child = walker.nextNode())) {
      if (child.nodeType === Node.TEXT_NODE) translateTextNode(child);
      else translateAttributes(child);
    }
  }

  function stopObserver() {
    if (observer) {
      observer.disconnect();
      observer = null;
    }
  }

  function startObserver() {
    if (observer || !document.body) return;
    observer = new MutationObserver((mutations) => {
      if (busy) return;
      busy = true;
      try {
        mutations.forEach((mutation) => {
          if (mutation.type === "childList") {
            mutation.addedNodes.forEach((node) => translateNode(node));
          } else if (mutation.type === "characterData") {
            translateTextNode(mutation.target);
          } else if (mutation.type === "attributes") {
            translateAttributes(mutation.target);
          }
        });
      } finally {
        busy = false;
      }
    });
    observer.observe(document.body, {
      childList: true,
      subtree: true,
      characterData: true,
      attributes: true,
      attributeFilter: TRANSLATED_ATTRS,
    });
  }

  function syncButtons() {
    document.querySelectorAll("[data-lang]").forEach((button) => {
      const active = button.dataset.lang === language;
      button.classList.toggle("on", active);
      button.setAttribute("aria-pressed", String(active));
    });
  }

  function setLanguage(next) {
    language = next === "en" ? "en" : "zh";
    try {
      localStorage.setItem("marktoast-lang", language);
    } catch (error) {
      // Private mode: keep session preference only.
    }
    document.documentElement.lang = language === "en" ? "en" : "zh-CN";
    stopObserver();
    if (document.body) translateNode(document.body);
    syncButtons();
    if (language === "en") startObserver();
    window.dispatchEvent(new CustomEvent("marktoast:langchange", { detail: { language } }));
  }

  window.MarkToastI18n = {
    t: translate,
    setLanguage,
    get language() {
      return language;
    },
  };

  const boot = () => {
    document.querySelectorAll("[data-lang]").forEach((button) => {
      button.addEventListener("click", () => setLanguage(button.dataset.lang));
    });
    setLanguage(language);
  };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot, { once: true });
  } else {
    boot();
  }
})();
