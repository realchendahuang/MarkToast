// MarkToast i18n — a deliberately small DOM translation layer.
//
// Chinese is the source language: every UI string is authored in Chinese and
// this file maps it to English. User content (memory names, bodies, paths) is
// never translated because only exact UI phrases and a few numeric patterns
// are matched.
(function () {
  "use strict";

  const SKIP = [
    "script", "style", "pre", "code", "textarea",
    ".markdown-body",
    ".expanded-project", ".alias-key", ".diff-box",
    ".memory-project", "[data-no-i18n]",
  ].join(",");

  const en = {
    "记忆": "Memory",
    "全部记忆": "All memories",
    "收件箱": "Inbox",
    "适配器": "Adapters",
    "历史": "History",
    "项目": "Projects",
    "设置": "Settings",
    "连接中": "Connecting",
    "记忆库": "Library",
    "无法连接本地 hub。": "Can't reach the local hub.",
    "重试": "Retry",
    "新建": "New",
    "全部项目": "All projects",
    "全部类型": "All types",
    "用户": "User",
    "反馈": "Feedback",
    "参考": "Reference",
    "决策": "Decision",
    "相关度": "Relevance",
    "外观": "Appearance",
    "语言": "Language",
    "跟随系统": "System",
    "浅色": "Light",
    "深色": "Dark",
    "仓库": "Repository",
    "根目录": "Root",
    "根目录已复制": "Root path copied",
    "Token已复制": "Token copied",
    "索引": "Index",
    "重建索引": "Rebuild index",
    "备份": "Backup",
    "导入": "Import",
    "项目别名": "Project aliases",
    "保存": "Save",
    "新建记忆": "New memory",
    "类型": "Type",
    "名称": "Name",
    "描述": "Description",
    "正文": "Body",
    "取消": "Cancel",
    "记忆仓库导航": "Memory repository navigation",
    "MarkToast 首页": "MarkToast home",
    "当前位置": "You are here",
    "主导航": "Main navigation",
    "搜索": "Search",
    "搜索记忆": "Search memories",
    "清空搜索": "Clear search",
    "刷新当前视图": "Refresh view",
    "刷新": "Refresh",
    "记忆筛选": "Memory filters",
    "项目筛选": "Project filter",
    "类型筛选": "Type filter",
    "切换排序": "Change sort",
    "主题": "Theme",
    "复制根目录": "Copy root path",
    "复制": "Copy",
    "复制 MCP token": "Copy MCP token",
    "关闭新建记忆弹层": "Close new memory dialog",
    "为搜索补充一点上下文": "Extra context for search",
    "写下这条记忆…": "Write this memory…",
    "未命名记忆": "Untitled memory",
    "请求失败，请稍后重试。": "Request failed, try again.",
    "无法连接本地 hub。请确认 `marktoast serve` 正在运行。": "Can't reach the local hub. Is `marktoast serve` running?",
    "未知时间": "Unknown time",
    "刚刚": "just now",
    "在线": "Online",
    "离线": "Offline",
    "处理中…": "Working…",
    "正在连接本地 hub": "Connecting to local hub",
    "已连接到本地 hub": "Connected to local hub",
    "暂无项目": "No projects yet",
    "等待本地 hub": "Waiting for local hub",
    "搜索结果": "Search results",
    "最近更新": "Recently updated",
    "标题": "Title",
    "没有匹配": "No matches",
    "还没有记忆": "No memories yet",
    "换个关键词试试。": "Try another keyword.",
    "先写一条。": "Write the first one.",
    "清除筛选": "Clear filters",
    "收起": "Collapse",
    "展开": "Expand",
    "版本历史": "Version history",
    "查看这条记忆的版本历史": "View version history",
    "复制记忆路径": "Copy memory path",
    "没有正文。": "No body.",
    "路径已复制到剪贴板": "Path copied",
    "复制失败，请手动选择路径": "Copy failed; copy the path manually",
    "不可用": "Unavailable",
    "记忆加载失败": "Failed to load memories",
    "本地 hub 未连接": "Local hub is offline",
    "重新连接": "Reconnect",
    "重新写入": "Rewrite",
    "写入": "Write",
    "写入 ZCode 配置？会先备份原文件。": "Write the ZCode config? The current file will be backed up.",
    "重新写入 ZCode 配置？会先备份原文件。": "Rewrite the ZCode config? The current file will be backed up.",
    "ZCode 已接入": "ZCode connected",
    "配置已更新": "Configuration updated",
    "片段已准备，路径已复制": "Snippets ready; path copied",
    "收件箱加载失败": "Failed to load inbox",
    "收件箱是空的": "Inbox is empty",
    "会话结束后，候选记忆会出现在这里。": "Candidates show up here after a session ends.",
    "候选记忆": "Candidate",
    "保留": "Keep",
    "丢弃": "Discard",
    "已保留": "Kept",
    "丢弃这条候选记忆？": "Discard this candidate?",
    "已丢弃": "Discarded",
    "适配器加载失败": "Failed to load adapters",
    "没有适配器": "No adapters",
    "本地 hub 没有报告可用的 Agent。": "The local hub reported no agents.",
    "已接入": "Connected",
    "待接入": "Needs setup",
    "已检测": "Detected",
    "未检测": "Not detected",
    "原生": "Native",
    "投影": "Projected",
    "只读": "Read-only",
    "修复": "Repair",
    "接入": "Connect",
    "片段": "Snippets",
    "生成合并片段": "Generate merge snippets",
    "Git 历史不可用": "Git history unavailable",
    "Git 不可用": "Git unavailable",
    "当前仓库没有可读取的 Git 历史。": "This repository has no readable Git history.",
    "还没有提交": "No commits yet",
    "保存记忆后会在这里出现。": "Commits appear here after you save a memory.",
    "无提交说明": "No commit message",
    "正在保存…": "Saving…",
    "记忆已保存": "Memory saved",
    "保存失败，请检查表单和本地 hub。": "Save failed; check the form and the local hub.",
    "已刷新": "Refreshed",
    "刷新失败，请确认本地 hub 正在运行": "Refresh failed; is the local hub running?",
    "浏览器已离线": "Browser is offline",
    "浏览器已离线。重新联网后可继续刷新本地记忆。": "Browser is offline. Reconnect to refresh local memories.",
    "复制失败": "Copy failed",
    "未建立": "Not built",
    "还没有项目": "No projects yet",
    "已保存": "Saved",
    "导入中…": "Importing…",
    "没有可导入的记忆": "Nothing to import",
    "备份已创建": "Backup created",
    "还没有提交记录": "No commits yet",
    "恢复中…": "Restoring…",
    "恢复此版本": "Restore this version",
    "恢复这个版本？会生成一次新的提交。": "Restore this version? A new commit will be created.",
    "已恢复": "Restored",
    "无法连接本地 hub。请运行 `marktoast serve` 后重试。": "Can't reach the local hub. Run `marktoast serve` and retry.",
    "记忆已保存": "Memory saved",
    "收起记忆": "Collapse memory",
    "展开记忆": "Expand memory",
    // adapter notes
    "指令投影 + MCP；hooks 可作为 T2 会话摄取来源。": "Instruction projection + MCP; hooks can feed T2 session ingestion.",
    "指令投影 + MCP；SessionEnd 版本差异要在适配器里探测。": "Instruction projection + MCP; SessionEnd version differences must be probed by the adapter.",
    "无固定 MCP 配置文件；静态兜底是受管 AGENTS.md 区块（约 65536 bytes），但不能替代 MCP。": "No fixed MCP config file; the static fallback is a managed AGENTS.md block (~65,536 bytes), but it cannot replace MCP.",
    "项目记忆：MEMORY.md 索引（≤200 行 / 25,000 字符）+ 一条事实一个 Markdown（frontmatter）。": "Project memory: a MEMORY.md index (≤200 lines / 25,000 chars) plus one Markdown file per fact (frontmatter).",
    "内置 MEMORY.md/USER.md 用精确 \"\\n§\\n\" 分隔，2200/1375 字符预算（本机 MEMORY.md 已 2528/2200 超限）；完整 preload/turn sync 需实现 standalone MemoryProvider 并关闭内置写入以避免双写。": "Native MEMORY.md/USER.md split on exact \"\\n§\\n\"; 2200/1375-char budget (this machine is 2528/2200 over); full preload/turn sync needs a standalone MemoryProvider with native writes off to avoid double writes.",
  };

  const patterns = [
    [/^(\d+) 分钟前$/, (m) => `${m[1]} min ago`],
    [/^(\d+) 小时前$/, (m) => `${m[1]} hr ago`],
    [/^(\d+) 天前$/, (m) => `${m[1]} d ago`],
    [/^(\d+) 周前$/, (m) => `${m[1]} wk ago`],
    [/^(\d+) 个月前$/, (m) => `${m[1]} mo ago`],
    [/^(\d+) 年前$/, (m) => `${m[1]} yr ago`],
    [/^(\d+) 条记忆$/, (m) => `${m[1]} memories`],
    [/^(\d+) 条 · (.+)$/, (m) => `${m[1]} items · ${m[2]}`],
    [/^(\d+) 条$/, (m) => `${m[1]} items`],
    [/^已导入 (\d+) 条$/, (m) => `Imported ${m[1]}`],
    [/^没有新记忆（跳过 (\d+) 条）$/, (m) => `Nothing new (${m[1]} skipped)`],
    [/^索引已重建 · (\d+) 条$/, (m) => `Index rebuilt · ${m[1]} memories`],
    [/^收起记忆：(.*)$/, (m) => `Collapse memory: ${m[1]}`],
    [/^展开记忆：(.*)$/, (m) => `Expand memory: ${m[1]}`],
    [/^(.*) · 恢复 (.*)$/, (m) => `${m[1]} · restored ${m[2]}`],
    [/^(.*) · 保存 (.*)$/, (m) => `${m[1]} · saved ${m[2]}`],
    [/^(.*) · 新增 (.*)$/, (m) => `${m[1]} · added ${m[2]}`],
    [/^(.*)已复制$/, (m) => `${m[1]} copied`],
  ];

  const textOriginals = new WeakMap();
  const attrOriginals = new WeakMap();
  let language = "zh";
  let observer = null;
  let busy = false;

  try {
    language = localStorage.getItem("marktoast-lang") === "en" ? "en" : "zh";
  } catch (error) {
    language = "zh";
  }

  function translate(value) {
    const text = String(value || "").trim();
    if (!text) return text;
    if (Object.prototype.hasOwnProperty.call(en, text)) return en[text];
    for (const [pattern, render] of patterns) {
      const match = text.match(pattern);
      if (match) return render(match);
    }
    return text;
  }

  function skip(element) {
    return !element || (element.closest && element.closest(SKIP));
  }

  const typePills = {
    "用户": "User",
    "反馈": "Feedback",
    "项目": "Project",
    "参考": "Reference",
    "决策": "Decision",
  };

  function translateTextNode(node) {
    const parent = node.parentElement;
    if (skip(parent)) return;
    const raw = node.nodeValue;
    const trimmed = raw.trim();
    if (!trimmed) return;
    if (language === "en" && parent && parent.closest(".type-pill") && typePills[trimmed]) {
      if (!textOriginals.has(node)) textOriginals.set(node, raw);
      node.nodeValue = raw.replace(trimmed, typePills[trimmed]);
      return;
    }
    if (language === "zh") {
      if (textOriginals.has(node)) {
        const original = textOriginals.get(node);
        if (raw !== original) node.nodeValue = original;
      }
      return;
    }
    const translated = translate(trimmed);
    if (translated === trimmed) return;
    if (!textOriginals.has(node)) textOriginals.set(node, raw);
    const start = raw.indexOf(trimmed);
    const next = raw.slice(0, start) + translated + raw.slice(start + trimmed.length);
    if (next !== raw) node.nodeValue = next;
  }

  function translateAttributes(element) {
    if (skip(element)) return;
    ["placeholder", "aria-label", "title"].forEach((name) => {
      const value = element.getAttribute(name);
      if (!value) return;
      let stored = attrOriginals.get(element);
      if (!stored) {
        stored = {};
        attrOriginals.set(element, stored);
      }
      if (language === "zh") {
        if (stored[name] !== undefined) element.setAttribute(name, stored[name]);
        return;
      }
      const translated = translate(value);
      if (translated === value.trim()) return;
      if (stored[name] === undefined) stored[name] = value;
      element.setAttribute(name, translated);
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
    if (observer) return;
    observer = new MutationObserver((mutations) => {
      if (busy) return;
      busy = true;
      mutations.forEach((mutation) => {
        if (mutation.type === "childList") {
          mutation.addedNodes.forEach((node) => translateNode(node));
        } else if (mutation.type === "characterData") {
          translateTextNode(mutation.target);
        } else if (mutation.type === "attributes") {
          translateAttributes(mutation.target);
        }
      });
      busy = false;
    });
    observer.observe(document.body, {
      childList: true,
      subtree: true,
      characterData: true,
      attributes: true,
      attributeFilter: ["placeholder", "aria-label", "title"],
    });
  }

  function syncButtons() {
    document.querySelectorAll("[data-lang]").forEach((button) => {
      button.classList.toggle("on", button.dataset.lang === language);
    });
  }

  function setLanguage(next) {
    language = next === "en" ? "en" : "zh";
    try {
      localStorage.setItem("marktoast-lang", language);
    } catch (error) {
      // Private mode: keep the session preference only.
    }
    document.documentElement.lang = language === "en" ? "en" : "zh-CN";
    stopObserver();
    translateNode(document.body);
    syncButtons();
    if (language === "en") startObserver();
  }

  window.MarkToastI18n = {
    t: translate,
    setLanguage,
    get language() {
      return language;
    },
  };

  document.addEventListener("DOMContentLoaded", () => {
    document.querySelectorAll("[data-lang]").forEach((button) => {
      button.addEventListener("click", () => setLanguage(button.dataset.lang));
    });
    setLanguage(language);
  });
})();
