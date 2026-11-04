// MarkToast web UI — dependency-free and served directly by the local hub.

"use strict";

const $ = (id) => document.getElementById(id);

const state = {
  status: null,
  root: "",
  view: "memories",
  project: "",
  type: "",
  query: "",
  sort: "relevance",
  results: [],
  selectedPath: "",
  details: new Map(),
  detailErrors: new Map(),
  detailLoading: false,
  adapters: [],
  history: [],
  searchController: null,
  searchTimer: 0,
  requestId: 0,
  lastFocus: null,
};

const els = {
  hubAlert: $("hub-alert"),
  hubAlertText: $("hub-alert-text"),
  hubStateText: $("hub-state-text"),
  statusDot: $("side-status-dot"),
  metaState: $("repo-meta-state"),
  search: $("global-search"),
  searchClear: $("search-clear"),
  projectList: $("project-list"),
  projectFilter: $("project-filter"),
  typeFilter: $("type-filter"),
  resultRegion: $("memory-results"),
  resultTotal: $("result-total"),
  memoryHeading: $("memory-heading"),
  memorySubtitle: $("memory-subtitle"),
  adapterRegion: $("adapter-results"),
  adapterCount: $("adapter-count"),
  historyRegion: $("history-results"),
  gitSubtitle: $("git-subtitle"),
  gitBadge: $("git-badge"),
  modal: $("memory-modal"),
  form: $("memory-form"),
  formError: $("form-error"),
  submit: $("submit-memory"),
};

if (/Mac|iPhone|iPad/.test(navigator.platform)) document.body.classList.add("mac");
if (!/Mac|iPhone|iPad/.test(navigator.platform)) $("search-shortcut").textContent = "Ctrl K";

function icon(name, className = "ic") {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("class", className);
  svg.setAttribute("aria-hidden", "true");
  const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
  use.setAttribute("href", `#icon-${name}`);
  svg.appendChild(use);
  return svg;
}

function element(tag, className = "", text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined && text !== null) node.textContent = String(text);
  return node;
}

function clear(node) {
  while (node.firstChild) node.removeChild(node.firstChild);
}

function shortPath(path) {
  if (!path) return "—";
  const root = state.root.replace(/\/+$/, "");
  if (root && path === root) return ".";
  if (root && path.startsWith(`${root}/`)) return path.slice(root.length + 1);
  return String(path).replace(/^\/+/, "");
}

function fileName(path) {
  return String(path || "未命名记忆").split("/").pop().replace(/\.md$/i, "");
}

function formatError(error, fallback = "请求失败，请稍后重试。") {
  if (error instanceof TypeError && /fetch|network|load/i.test(error.message)) {
    return "无法连接本地 hub。请确认 `marktoast serve` 正在运行。";
  }
  return error?.message ? String(error.message) : fallback;
}

function formatDate(value, withTime = false) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  const options = withTime
    ? { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }
    : { year: "numeric", month: "2-digit", day: "2-digit" };
  return new Intl.DateTimeFormat("zh-CN", options).format(date).replaceAll("/", "-");
}

function relativeDate(value) {
  if (!value) return "未知时间";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  const diff = Date.now() - date.getTime();
  if (diff < 60_000) return "刚刚";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`;
  if (diff < 604_800_000) return `${Math.floor(diff / 86_400_000)} 天前`;
  return formatDate(value);
}

async function api(path, options = {}) {
  const response = await fetch(path, {
    headers: {
      Accept: "application/json",
      ...(options.body ? { "Content-Type": "application/json" } : {}),
      ...options.headers,
    },
    ...options,
  });
  if (!response.ok) {
    let detail = "";
    try {
      const payload = await response.json();
      detail = payload.error || payload.message || "";
    } catch {
      // The hub sometimes returns a plain HTTP error.
    }
    throw new Error(detail || `HTTP ${response.status} ${response.statusText}`.trim());
  }
  return response.json();
}

function setHubState(kind, text) {
  els.statusDot.classList.remove("is-online", "is-offline");
  if (kind) els.statusDot.classList.add(`is-${kind}`);
  els.hubStateText.textContent = text;
  els.metaState.classList.toggle("is-offline", kind === "offline");
  els.metaState.textContent = kind === "online" ? "已连接" : kind === "offline" ? "离线" : "连接中";
}

function showHubAlert(message) {
  els.hubAlertText.textContent = message;
  els.hubAlert.hidden = false;
}

function hideHubAlert() {
  els.hubAlert.hidden = true;
}

function toast(message, kind = "success") {
  const node = element("div", `toast${kind === "error" ? " is-error" : ""}`);
  node.appendChild(icon(kind === "error" ? "warning" : "check"));
  node.appendChild(element("span", "", message));
  $("toast-region").appendChild(node);
  window.setTimeout(() => {
    node.style.opacity = "0";
    node.style.transform = "translateY(5px)";
    window.setTimeout(() => node.remove(), 180);
  }, 3400);
}

function setButtonLoading(button, loading, loadingText = "处理中…") {
  if (!button) return;
  const label = button.querySelector("span");
  if (loading) {
    button.dataset.original = label ? label.textContent : button.textContent.trim();
    button.disabled = true;
    if (label) label.textContent = loadingText;
    else button.textContent = loadingText;
    return;
  }
  button.disabled = false;
  if (button.dataset.original) {
    if (label) label.textContent = button.dataset.original;
    else button.textContent = button.dataset.original;
  }
}

function emptyState(title, message, actionLabel, onAction) {
  const empty = element("div", "empty-state");
  empty.append(element("b", "", title), element("span", "", message));
  if (actionLabel && onAction) {
    const button = element("button", "button quiet", actionLabel);
    button.type = "button";
    button.addEventListener("click", onAction);
    empty.appendChild(button);
  }
  return empty;
}

async function loadStatus({ announce = false } = {}) {
  setHubState("", "正在连接本地 hub");
  try {
    const data = await api("/api/status");
    state.status = data;
    state.root = data.root || "";
    hideHubAlert();
    setHubState("online", `在线 · ${Number(data.facts) || 0} 条记忆`);
    $("all-count").textContent = String(data.facts ?? 0);
    $("repo-root").textContent = data.root || "—";
    $("repo-root").title = data.root || "";
    $("repo-version").textContent = `MarkToast · v${data.version || "dev"}`;
    $("settings-root").textContent = shortPath(data.root || "");
    $("settings-root").title = data.root || "";
    $("settings-index").textContent = `${Number(data.index?.facts) || 0} 条 · ${formatDate(data.index?.builtAt, true)}`;
    renderProjects(data.projects || []);
    renderProjectOptions(data.projects || []);
    renderAdapterMiniatures(data.adapters || []);
    if (announce) toast("已连接到本地 hub");
    return data;
  } catch (error) {
    state.status = null;
    setHubState("offline", "本地 hub 离线");
    showHubAlert(formatError(error));
    $("repo-root").textContent = "尚未连接本地仓库";
    $("repo-version").textContent = "MarkToast · offline";
    $("settings-root").textContent = "—";
    $("settings-index").textContent = "等待本地 hub";
    renderProjects([]);
    renderProjectOptions([]);
    renderAdapterMiniatures([]);
    if (state.view === "memories" && !state.results.length) renderMemoryError(error);
    if (announce) toast(formatError(error), "error");
    return null;
  }
}

function renderProjects(projects) {
  clear(els.projectList);
  if (!projects.length) {
    els.projectList.appendChild(element("div", "side-label", state.status ? "暂无项目" : "等待本地 hub"));
    return;
  }
  projects
    .slice()
    .sort((a, b) => String(a.key).localeCompare(String(b.key), "zh-CN"))
    .forEach((project) => {
      const button = element("button", `side-row project-row${state.project === project.key ? " is-active" : ""}`);
      button.type = "button";
      button.dataset.project = project.key;
      button.title = project.key;
      button.setAttribute("aria-pressed", String(state.project === project.key));
      button.append(
        icon("folder"),
        element("span", "side-name", project.key),
        element("span", "side-count", project.facts ?? 0),
      );
      button.addEventListener("click", () => selectProject(project.key));
      els.projectList.appendChild(button);
    });
}

function renderProjectOptions(projects) {
  const current = state.project || els.projectFilter.value;
  clear(els.projectFilter);
  const all = element("option", "", "全部项目");
  all.value = "";
  els.projectFilter.appendChild(all);
  projects.forEach((project) => {
    const option = element("option", "", project.key);
    option.value = project.key;
    els.projectFilter.appendChild(option);
  });
  els.projectFilter.value = projects.some((project) => project.key === current) ? current : "";
}

function renderAdapterMiniatures(adapters) {
  const list = $("adapter-mini-list");
  clear(list);
  if (!adapters.length) {
    list.appendChild(element("div", "side-label", state.status ? "暂无适配器" : "等待本地 hub"));
    return;
  }
  adapters.slice(0, 5).forEach((adapter) => {
    const item = element("div", "adapter-mini");
    item.title = `${adapter.title || adapter.id} · ${adapter.tier || ""}`;
    item.append(
      element("span", "adapter-dot"),
      element("span", "adapter-mini-name", adapter.id || adapter.title),
      element("span", "tier-label", adapter.tier || ""),
    );
    list.appendChild(item);
  });
}

function selectProject(project) {
  state.project = project;
  state.query = "";
  els.search.value = "";
  els.searchClear.hidden = true;
  els.projectFilter.value = project;
  renderProjects(state.status?.projects || []);
  $("all-memories").classList.toggle("is-active", !project);
  $("recent-memories").classList.remove("is-active");
  switchView("memories");
  loadMemories({ preserveSelection: false });
}

function updateHeading() {
  const project = state.project || "全部项目";
  if (state.query) {
    els.memoryHeading.textContent = "搜索结果";
    els.memorySubtitle.textContent = `“${state.query}” · ${project}${state.type ? ` · ${state.type}` : ""}`;
  } else if (state.project) {
    els.memoryHeading.textContent = state.project;
    els.memorySubtitle.textContent = `${state.type ? `${state.type} · ` : ""}本地记忆中与这个项目有关的内容。`;
  } else {
    els.memoryHeading.textContent = "全部记忆";
    els.memorySubtitle.textContent = "来自本地仓库，随时可读、可搜索。";
  }
  $("sort-label").textContent = state.sort === "updated" ? "最近更新" : state.sort === "title" ? "标题" : "相关度";
}

function renderMemoryLoading(count = 5) {
  clear(els.resultRegion);
  const list = element("div", "list");
  for (let index = 0; index < count; index += 1) {
    const row = element("div", "row loading-row");
    row.append(
      icon("file"),
      (() => {
        const who = element("div", "who");
        who.append(element("span", "skeleton title"), element("span", "skeleton line"));
        return who;
      })(),
    );
    list.appendChild(row);
  }
  els.resultRegion.setAttribute("aria-busy", "true");
  els.resultRegion.appendChild(list);
}

async function loadMemories({ preserveSelection = true, silent = false } = {}) {
  const requestId = ++state.requestId;
  if (state.searchController) state.searchController.abort();
  state.searchController = new AbortController();
  updateHeading();
  if (!silent) renderMemoryLoading();
  els.resultRegion.setAttribute("aria-busy", "true");

  const params = new URLSearchParams();
  let endpoint;
  if (state.query) {
    params.set("q", state.query);
    params.set("limit", "20");
    if (state.project) params.set("project", state.project);
    if (state.type) params.set("type", state.type);
    endpoint = `/api/search?${params.toString()}`;
  } else {
    if (state.project) params.set("project", state.project);
    endpoint = `/api/memories${params.size ? `?${params.toString()}` : ""}`;
  }

  try {
    const data = await api(endpoint, { signal: state.searchController.signal });
    if (requestId !== state.requestId) return;
    let results = Array.isArray(data.results) ? data.results : Array.isArray(data.memories) ? data.memories : [];
    if (state.type) results = results.filter((item) => String(item.type || "").toLowerCase() === state.type);
    state.results = sortResults(results);
    if (!preserveSelection || !state.results.some((item) => item.path === state.selectedPath)) {
      state.selectedPath = "";
      state.detailLoading = false;
    }
    renderMemories();
    els.resultRegion.setAttribute("aria-busy", "false");
  } catch (error) {
    if (error.name === "AbortError" || requestId !== state.requestId) return;
    state.results = [];
    renderMemoryError(error);
    els.resultRegion.setAttribute("aria-busy", "false");
  }
}

function sortResults(results) {
  const copy = results.slice();
  if (state.sort === "title") {
    copy.sort((a, b) => String(a.name || fileName(a.path)).localeCompare(String(b.name || fileName(b.path)), "zh-CN"));
  } else if (state.sort === "updated" || !state.query) {
    copy.sort((a, b) => new Date(b.updated || 0).getTime() - new Date(a.updated || 0).getTime());
  } else {
    copy.sort((a, b) => Number(b.score || 0) - Number(a.score || 0));
  }
  return copy;
}

function renderMemories() {
  clear(els.resultRegion);
  els.resultTotal.textContent = state.results.length ? `共 ${state.results.length} 条` : "0 条";
  if (!state.results.length) {
    const list = element("div", "list");
    list.appendChild(emptyState(
      state.query ? "没有找到匹配的记忆" : "仓库还是空的",
      state.query ? "换一个更短的关键词，或清除项目与类型筛选后再试。" : "写下第一条记忆，让不同的 Agent 从这里接上上下文。",
      state.query ? "清除筛选" : "新建记忆",
      state.query ? clearFilters : openDialog,
    ));
    els.resultRegion.appendChild(list);
    return;
  }

  const list = element("div", "list");
  state.results.forEach((result) => {
    const entry = element("article", "memory-entry");
    const open = result.path === state.selectedPath;
    const row = element("button", `row memory-row${open ? " is-open" : ""}`);
    row.type = "button";
    row.dataset.path = result.path;
    row.setAttribute("aria-expanded", String(open));
    row.setAttribute("aria-label", `${open ? "收起" : "展开"}记忆：${result.name || fileName(result.path)}`);
    row.append(icon("file", "ic memory-icon"));

    const who = element("div", "who");
    who.append(
      element("div", "name", result.name || fileName(result.path)),
      element("div", "sub", result.description || cleanSnippet(result.snippet) || shortPath(result.path)),
    );
    row.appendChild(who);

    const meta = element("div", "memory-meta");
    meta.append(
      element("span", "pill type-pill", result.type || "memory"),
      element("span", "memory-project", result.project || "—"),
      element("span", "memory-time", relativeDate(result.updated)),
    );
    const chevron = element("span", "chev");
    chevron.appendChild(icon("chevron-right"));
    meta.appendChild(chevron);
    row.appendChild(meta);
    row.addEventListener("click", () => selectMemory(result.path));
    entry.appendChild(row);
    if (open) entry.appendChild(renderExpandedMemory(result));
    list.appendChild(entry);
  });
  els.resultRegion.appendChild(list);
}

function cleanSnippet(snippet) {
  return String(snippet || "").replace(/\s+/g, " ").trim();
}

function renderExpandedMemory(result) {
  const box = element("div", "memory-expanded");
  if (state.detailLoading) {
    box.classList.add("loading");
    const loading = element("div", "detail-loading");
    loading.append(
      element("span", "skeleton"),
      element("span", "skeleton"),
      element("span", "skeleton"),
    );
    box.appendChild(loading);
    return box;
  }

  if (state.detailErrors.has(result.path)) {
    const error = element("div", "expanded-error");
    error.append(
      icon("warning"),
      element("span", "", formatError(state.detailErrors.get(result.path))),
      (() => {
        const retry = element("button", "text-button", "重试");
        retry.type = "button";
        retry.addEventListener("click", () => selectMemory(result.path, true));
        return retry;
      })(),
    );
    box.appendChild(error);
    return box;
  }

  const detail = state.details.get(result.path);
  if (!detail) {
    box.classList.add("loading");
    box.appendChild(element("div", "detail-loading"));
    return box;
  }

  const head = element("div", "expanded-head");
  const path = element("span", "path", shortPath(detail.path || result.path));
  path.title = detail.path || result.path;
  const copy = element("button", "button quiet", "");
  copy.type = "button";
  copy.setAttribute("aria-label", "复制记忆路径");
  copy.append(icon("copy"), document.createTextNode("复制路径"));
  copy.addEventListener("click", () => copyPath(detail.path || result.path));
  head.append(icon("folder"), path, copy);
  box.appendChild(head);

  const parsed = parseFrontmatter(detail.raw || "");
  let values = parsed.values;
  if (!values.length) {
    values = [
      ["name", detail.name || result.name],
      ["description", detail.description || result.description],
      ["project", detail.project || result.project],
      ["type", detail.type || result.type],
      ["updated", detail.updated || result.updated],
    ].filter(([, value]) => value);
  }

  const grid = element("div", "detail-grid");
  const frontmatter = element("section", "detail-section");
  frontmatter.append(element("h3", "", "frontmatter"), renderFrontmatter(values));
  const markdown = element("section", "detail-section");
  markdown.appendChild(element("h3", "", "正文"));
  const body = element("div", "markdown-body");
  renderMarkdown(body, detail.body || parsed.body || "");
  markdown.appendChild(body);
  grid.append(frontmatter, markdown);
  box.appendChild(grid);
  return box;
}

async function selectMemory(path, force = false) {
  if (!path) return;
  if (path === state.selectedPath && !force) {
    state.selectedPath = "";
    state.detailLoading = false;
    renderMemories();
    return;
  }
  state.selectedPath = path;
  state.detailErrors.delete(path);
  state.detailLoading = !state.details.has(path);
  renderMemories();
  if (state.details.has(path) && !force) return;

  try {
    const detail = await api(`/api/memory?path=${encodeURIComponent(path)}`);
    state.details.set(path, detail);
    state.detailErrors.delete(path);
    if (state.selectedPath === path && state.view === "memories") {
      state.detailLoading = false;
      renderMemories();
    }
  } catch (error) {
    if (state.selectedPath === path && state.view === "memories") {
      state.detailLoading = false;
      state.detailErrors.set(path, error);
      renderMemories();
    }
  }
}

function parseFrontmatter(raw) {
  const text = String(raw || "").replace(/\r\n?/g, "\n");
  if (!text.startsWith("---\n")) return { values: [], body: text };
  const close = text.indexOf("\n---", 4);
  if (close < 0) return { values: [], body: text };
  const header = text.slice(4, close).trim();
  const body = text.slice(close + 4).replace(/^\n/, "");
  const values = [];
  header.split("\n").forEach((line) => {
    const match = line.match(/^([A-Za-z0-9_-]+)\s*:\s*(.*)$/);
    if (match) values.push([match[1], stripYamlQuotes(match[2])]);
  });
  return { values, body };
}

function stripYamlQuotes(value) {
  const trimmed = String(value || "").trim();
  if (
    (trimmed.startsWith("\"") && trimmed.endsWith("\"")) ||
    (trimmed.startsWith("'") && trimmed.endsWith("'"))
  ) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}

function renderFrontmatter(values) {
  if (!values.length) return element("div", "frontmatter-empty", "没有 frontmatter");
  const table = element("table", "frontmatter-table");
  const body = document.createElement("tbody");
  values.forEach(([key, value]) => {
    const row = document.createElement("tr");
    row.append(element("th", "", key), element("td", "", String(value).replace(/^\[|\]$/g, "")));
    body.appendChild(row);
  });
  table.appendChild(body);
  return table;
}

function renderMarkdown(container, markdown) {
  const lines = String(markdown || "").replace(/\r\n?/g, "\n").split("\n");
  let index = 0;
  while (index < lines.length) {
    const line = lines[index];
    if (!line.trim()) {
      index += 1;
      continue;
    }

    if (/^```/.test(line)) {
      const codeLines = [];
      index += 1;
      while (index < lines.length && !/^```/.test(lines[index])) {
        codeLines.push(lines[index]);
        index += 1;
      }
      if (index < lines.length) index += 1;
      const pre = document.createElement("pre");
      const code = document.createElement("code");
      code.textContent = codeLines.join("\n");
      pre.appendChild(code);
      container.appendChild(pre);
      continue;
    }

    const heading = line.match(/^(#{1,6})\s+(.+)$/);
    if (heading) {
      const node = document.createElement(`h${heading[1].length}`);
      appendInline(node, heading[2]);
      container.appendChild(node);
      index += 1;
      continue;
    }

    const unordered = line.match(/^\s*[-*+]\s+(.+)$/);
    const ordered = line.match(/^\s*\d+[.)]\s+(.+)$/);
    if (unordered || ordered) {
      const list = document.createElement(ordered ? "ol" : "ul");
      while (index < lines.length) {
        const match = ordered
          ? lines[index].match(/^\s*\d+[.)]\s+(.+)$/)
          : lines[index].match(/^\s*[-*+]\s+(.+)$/);
        if (!match) break;
        const item = document.createElement("li");
        appendInline(item, match[1]);
        list.appendChild(item);
        index += 1;
      }
      container.appendChild(list);
      continue;
    }

    const paragraphLines = [line.trim()];
    index += 1;
    while (index < lines.length) {
      const current = lines[index];
      if (
        !current.trim() ||
        /^```/.test(current) ||
        /^(#{1,6})\s+/.test(current) ||
        /^\s*[-*+]\s+/.test(current) ||
        /^\s*\d+[.)]\s+/.test(current)
      ) {
        break;
      }
      paragraphLines.push(current.trim());
      index += 1;
    }
    const paragraph = document.createElement("p");
    appendInline(paragraph, paragraphLines.join(" "));
    container.appendChild(paragraph);
  }
}

function appendInline(parent, source) {
  const pattern = /(\*\*[^*]+\*\*|`[^`\n]+`|\[[^\]\n]+\]\([^) \n]+\))/g;
  let lastIndex = 0;
  let match;
  while ((match = pattern.exec(source)) !== null) {
    if (match.index > lastIndex) {
      parent.appendChild(document.createTextNode(source.slice(lastIndex, match.index)));
    }
    const token = match[0];
    if (token.startsWith("**")) {
      parent.appendChild(element("strong", "", token.slice(2, -2)));
    } else if (token.startsWith("`")) {
      parent.appendChild(element("code", "", token.slice(1, -1)));
    } else {
      const linkMatch = token.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
      const href = linkMatch ? safeUrl(linkMatch[2]) : "";
      if (href) {
        const anchor = document.createElement("a");
        anchor.href = href;
        anchor.textContent = linkMatch[1];
        anchor.target = "_blank";
        anchor.rel = "noreferrer noopener";
        parent.appendChild(anchor);
      } else if (linkMatch) {
        parent.appendChild(document.createTextNode(linkMatch[1]));
      }
    }
    lastIndex = pattern.lastIndex;
  }
  if (lastIndex < source.length) parent.appendChild(document.createTextNode(source.slice(lastIndex)));
}

function safeUrl(raw) {
  try {
    const url = new URL(raw, window.location.origin);
    if (url.protocol === "http:" || url.protocol === "https:") return url.href;
  } catch {
    return "";
  }
  return "";
}

async function copyPath(path) {
  if (!path) return;
  try {
    await navigator.clipboard.writeText(path);
    toast("路径已复制到剪贴板");
  } catch {
    toast("复制失败，请手动选择路径", "error");
  }
}

function renderMemoryError(error) {
  clear(els.resultRegion);
  els.resultTotal.textContent = "不可用";
  const list = element("div", "list");
  list.appendChild(emptyState(
    state.status ? "记忆加载失败" : "本地 hub 未连接",
    formatError(error),
    "重新连接",
    retryAll,
  ));
  els.resultRegion.appendChild(list);
}

async function loadAdapters() {
  clear(els.adapterRegion);
  const list = element("div", "list");
  list.append(
    element("div", "row loading-row"),
    element("div", "row loading-row"),
    element("div", "row loading-row"),
  );
  els.adapterRegion.appendChild(list);
  try {
    const data = await api("/api/adapters");
    state.adapters = Array.isArray(data.adapters) ? data.adapters : [];
    renderAdapters();
    return true;
  } catch (error) {
    clear(els.adapterRegion);
    els.adapterCount.textContent = "不可用";
    const list = element("div", "list");
    list.appendChild(emptyState("适配器加载失败", formatError(error), "重试", loadAdapters));
    els.adapterRegion.appendChild(list);
    return false;
  }
}

function renderAdapters() {
  clear(els.adapterRegion);
  els.adapterCount.textContent = `${state.adapters.length} 个`;
  if (!state.adapters.length) {
    const list = element("div", "list");
    list.appendChild(emptyState("还没有适配器", "当前 hub 没有报告可用的 Agent 适配器。"));
    els.adapterRegion.appendChild(list);
    return;
  }

  const list = element("div", "list");
  state.adapters.forEach((adapter) => {
    const entry = element("article", "adapter-entry");
    const row = element("div", "row adapter-row");
    row.appendChild(icon("plug", "ic adapter-icon"));
    const who = element("div", "who");
    const nameLine = element("div", "adapter-name-line");
    nameLine.append(
      element("span", "name", adapter.title || adapter.id || "Adapter"),
      element("code", "", adapter.id || ""),
    );
    who.appendChild(nameLine);
    if (adapter.notes) who.appendChild(element("div", "sub", adapter.notes));

    const capabilities = Array.isArray(adapter.capabilities) ? adapter.capabilities : [];
    if (capabilities.length) {
      const caps = element("div", "adapter-caps");
      capabilities.forEach((capability) => caps.appendChild(element("span", "pill", capability)));
      who.appendChild(caps);
    }

    const summary = [];
    if (adapter.hooks?.events) {
      const events = Array.isArray(adapter.hooks.events) ? adapter.hooks.events.join(", ") : adapter.hooks.events;
      summary.push(`hooks ${events}`);
    }
    if (adapter.hooks?.install_kind) summary.push(adapter.hooks.install_kind);
    if (adapter.hooks?.injection) summary.push(`inject ${adapter.hooks.injection}`);
    if (adapter.mcp?.kind || adapter.mcp?.target) {
      summary.push(`mcp ${[adapter.mcp.kind, adapter.mcp.target].filter(Boolean).join(" → ")}`);
    }
    if (summary.length) {
      const summaryNode = element("div", "adapter-summary");
      summary.forEach((item) => summaryNode.appendChild(element("span", "", item)));
      who.appendChild(summaryNode);
    }
    row.append(who, element("span", "pill", adapter.tier || "tier"));
    entry.appendChild(row);
    list.appendChild(entry);
  });
  els.adapterRegion.appendChild(list);
}

async function loadHistory() {
  clear(els.historyRegion);
  els.gitBadge.textContent = "检查中";
  els.gitBadge.className = "pill git-badge";
  els.gitSubtitle.textContent = "正在读取仓库提交…";
  const loading = element("div", "list");
  loading.append(element("div", "row loading-row"), element("div", "row loading-row"));
  els.historyRegion.appendChild(loading);

  if (!state.status) await loadStatus();
  try {
    const data = await api("/api/git/log?n=20");
    state.history = Array.isArray(data.commits) ? data.commits : [];
    renderHistory();
    return true;
  } catch (error) {
    clear(els.historyRegion);
    els.gitBadge.textContent = "不可用";
    els.gitBadge.className = "pill git-badge is-offline";
    els.gitSubtitle.textContent = "Git 历史读取失败。";
    const list = element("div", "list");
    list.appendChild(emptyState("Git 历史加载失败", formatError(error), "重试", loadHistory));
    els.historyRegion.appendChild(list);
    return false;
  }
}

function renderHistory() {
  clear(els.historyRegion);
  const git = state.status?.git;
  if (git?.available === false) {
    els.gitBadge.textContent = "Git 不可用";
    els.gitBadge.className = "pill git-badge is-offline";
    els.gitSubtitle.textContent = "当前目录没有可读取的 Git 仓库。";
  } else if (git?.clean === false) {
    els.gitBadge.textContent = `${git.changes ?? 0} 项变更`;
    els.gitBadge.className = "pill git-badge has-changes";
    els.gitSubtitle.textContent = "仓库有未提交的变更，记忆可能在下次提交前暂时不同步。";
  } else {
    els.gitBadge.textContent = "工作区干净";
    els.gitBadge.className = "pill git-badge";
    els.gitSubtitle.textContent = "所有记忆变更都可追溯、可回退。";
  }

  if (!state.history.length) {
    const list = element("div", "list");
    list.appendChild(emptyState("还没有提交记录", "第一次保存并提交记忆后，Git 历史会出现在这里。"));
    els.historyRegion.appendChild(list);
    return;
  }

  const list = element("div", "list");
  state.history.forEach((commit) => {
    const entry = element("article", "history-entry");
    const row = element("div", "row history-row");
    row.append(
      icon("branch"),
      element("code", "history-hash", String(commit.hash || "").slice(0, 8)),
      element("span", "history-subject", commit.subject || "无提交说明"),
      element("span", "pill", "提交"),
    );
    entry.appendChild(row);
    list.appendChild(entry);
  });
  els.historyRegion.appendChild(list);
}

const VIEW_LABELS = { memories: "全部记忆", adapters: "适配器", history: "历史", settings: "设置" };

function switchView(view) {
  state.view = view;
  document.querySelectorAll("[data-view]").forEach((tab) => {
    const active = tab.dataset.view === view;
    tab.classList.toggle("on", active);
    if (active) tab.setAttribute("aria-current", "page");
    else tab.removeAttribute("aria-current");
  });
  const crumb = document.getElementById("breadcrumb-current");
  if (crumb) crumb.textContent = VIEW_LABELS[view] || "全部记忆";
  document.querySelectorAll(".view-panel").forEach((panel) => {
    panel.hidden = panel.id !== `view-${view}`;
  });
  positionSegThumb();
  if (view === "adapters" && !state.adapters.length) loadAdapters();
  if (view === "history") loadHistory();
  if (view === "memories" && !state.results.length) loadMemories();
}

// positionSegThumb slides the segmented control's thumb under the picked tab.
function positionSegThumb() {
  const seg = document.getElementById("main-nav");
  const thumb = document.getElementById("seg-thumb");
  if (!seg || !thumb) return;
  const active = seg.querySelector("button.on");
  if (!active || seg.offsetWidth === 0) {
    thumb.style.opacity = "0";
    return;
  }
  thumb.style.opacity = "";
  const x = active.offsetLeft;
  const w = active.offsetWidth;
  const first = !thumb.style.width;
  if (first) thumb.classList.add("still");
  thumb.style.transform = `translateX(${x}px)`;
  thumb.style.width = `${w}px`;
  if (first) {
    void thumb.offsetWidth;
    thumb.classList.remove("still");
  }
}

function clearFilters() {
  state.query = "";
  state.project = "";
  state.type = "";
  state.selectedPath = "";
  state.detailLoading = false;
  els.search.value = "";
  els.searchClear.hidden = true;
  els.projectFilter.value = "";
  els.typeFilter.value = "";
  renderProjects(state.status?.projects || []);
  $("all-memories").classList.add("is-active");
  $("recent-memories").classList.remove("is-active");
  loadMemories({ preserveSelection: false });
}

function openDialog() {
  state.lastFocus = document.activeElement;
  clear(els.formError);
  els.formError.hidden = true;
  $("memory-project").value = state.project || "";
  $("memory-type").value = state.type || "project";
  $("memory-name").value = "";
  $("memory-description").value = "";
  $("memory-body").value = "";
  els.modal.classList.remove("closing");
  els.modal.hidden = false;
  document.body.classList.add("modal-open");
  window.setTimeout(() => $("memory-project").focus(), 30);
}

function closeDialog() {
  if (els.modal.hidden || els.modal.classList.contains("closing")) return;
  els.modal.classList.add("closing");
  document.body.classList.remove("modal-open");
  window.setTimeout(() => {
    els.modal.hidden = true;
    els.modal.classList.remove("closing");
  }, 150);
  if (state.lastFocus && typeof state.lastFocus.focus === "function") state.lastFocus.focus();
}

async function submitMemory(event) {
  event.preventDefault();
  if (!els.form.reportValidity()) return;
  els.formError.hidden = true;
  setButtonLoading(els.submit, true, "正在保存…");
  const payload = {
    project: $("memory-project").value.trim(),
    name: $("memory-name").value.trim(),
    description: $("memory-description").value.trim(),
    type: $("memory-type").value,
    body: $("memory-body").value,
  };

  try {
    const result = await api("/api/memory", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    closeDialog();
    toast("记忆已保存");
    await loadStatus();
    state.project = payload.project;
    state.type = "";
    state.query = "";
    els.typeFilter.value = "";
    els.search.value = "";
    els.searchClear.hidden = true;
    els.projectFilter.value = payload.project;
    switchView("memories");
    await loadMemories({ preserveSelection: false });
    if (result.path) await selectMemory(result.path);
  } catch (error) {
    els.formError.textContent = formatError(error, "保存失败，请检查表单和本地 hub。");
    els.formError.hidden = false;
    els.formError.scrollIntoView({ block: "nearest" });
  } finally {
    setButtonLoading(els.submit, false);
  }
}

async function retryAll() {
  const status = await loadStatus({ announce: true });
  if (!status) return;
  if (state.view === "adapters") await loadAdapters();
  else if (state.view === "history") await loadHistory();
  else await loadMemories({ preserveSelection: false });
}

async function refreshCurrent() {
  const button = $("refresh-view");
  button.disabled = true;
  let refreshed = false;
  if (state.view === "memories") {
    const status = await loadStatus();
    if (status) {
      await loadMemories({ preserveSelection: false });
      refreshed = true;
    }
  } else if (state.view === "adapters") {
    await loadStatus();
    state.adapters = [];
    refreshed = await loadAdapters();
  } else if (state.view === "history") {
    await loadStatus();
    refreshed = await loadHistory();
  } else {
    refreshed = Boolean(await loadStatus());
  }
  button.disabled = false;
  if (refreshed) toast("已刷新");
  else toast("刷新失败，请确认本地 hub 正在运行", "error");
}

function focusSearch() {
  els.search.focus();
  els.search.select();
}

function bindEvents() {
  els.search.addEventListener("input", () => {
    const query = els.search.value.trim();
    state.query = query;
    state.selectedPath = "";
    els.searchClear.hidden = !query;
    $("search-shortcut").hidden = Boolean(query);
    window.clearTimeout(state.searchTimer);
    state.searchTimer = window.setTimeout(() => {
      if (state.view !== "memories") switchView("memories");
      loadMemories({ preserveSelection: false });
    }, 240);
  });

  els.searchClear.addEventListener("click", () => {
    els.search.value = "";
    state.query = "";
    state.selectedPath = "";
    els.searchClear.hidden = true;
    $("search-shortcut").hidden = false;
    els.search.focus();
    loadMemories({ preserveSelection: false });
  });

  document.addEventListener("keydown", (event) => {
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
      event.preventDefault();
      focusSearch();
    }
    if (event.key === "Escape" && !els.modal.hidden) {
      event.preventDefault();
      closeDialog();
    }
  });

  document.querySelectorAll("[data-view]").forEach((tab) => {
    tab.addEventListener("click", () => switchView(tab.dataset.view));
  });

  $("all-memories").addEventListener("click", () => {
    state.type = "";
    els.typeFilter.value = "";
    selectProject("");
  });
  $("recent-memories").addEventListener("click", () => {
    state.project = "";
    state.query = "";
    state.type = "";
    state.sort = "updated";
    els.search.value = "";
    els.searchClear.hidden = true;
    els.typeFilter.value = "";
    els.projectFilter.value = "";
    renderProjects(state.status?.projects || []);
    $("all-memories").classList.remove("is-active");
    $("recent-memories").classList.add("is-active");
    switchView("memories");
    loadMemories({ preserveSelection: false });
  });

  els.projectFilter.addEventListener("change", () => {
    state.project = els.projectFilter.value;
    renderProjects(state.status?.projects || []);
    $("all-memories").classList.toggle("is-active", !state.project);
    loadMemories({ preserveSelection: false });
  });
  els.typeFilter.addEventListener("change", () => {
    state.type = els.typeFilter.value;
    loadMemories({ preserveSelection: false });
  });

  $("sort-button").addEventListener("click", () => {
    state.sort = state.sort === "relevance" ? "updated" : state.sort === "updated" ? "title" : "relevance";
    updateHeading();
    state.results = sortResults(state.results);
    renderMemories();
  });

  $("new-memory-top").addEventListener("click", openDialog);
  $("open-adapters").addEventListener("click", () => switchView("adapters"));
  $("refresh-view").addEventListener("click", refreshCurrent);
  $("retry-hub").addEventListener("click", retryAll);
  $("close-dialog").addEventListener("click", closeDialog);
  $("cancel-dialog").addEventListener("click", closeDialog);
  els.form.addEventListener("submit", submitMemory);
  els.modal.addEventListener("click", (event) => {
    if (event.target === els.modal) closeDialog();
  });
  document.querySelectorAll(".brand").forEach((brand) => {
    brand.addEventListener("click", (event) => {
      event.preventDefault();
      switchView("memories");
      focusSearch();
    });
  });
  window.addEventListener("resize", positionSegThumb);
  positionSegThumb();
  window.addEventListener("online", () => loadStatus({ announce: true }));
  window.addEventListener("offline", () => {
    setHubState("offline", "浏览器已离线");
    showHubAlert("浏览器已离线。重新联网后可继续刷新本地记忆。");
  });
}

async function init() {
  bindEvents();
  renderMemoryLoading();
  const status = await loadStatus();
  if (status) await loadMemories();
  else renderMemoryError(new Error("无法连接本地 hub。请运行 `marktoast serve` 后重试。"));
  window.appReady = true;
}

init();
