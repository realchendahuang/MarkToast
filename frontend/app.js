// MarkToast desktop UI — dependency-free and served directly by the Go hub.

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
  detail: null,
  adapters: [],
  history: [],
  searchController: null,
  searchTimer: 0,
  requestId: 0,
};

const els = {
  hubState: $("hub-state"),
  hubStateText: $("hub-state-text"),
  search: $("global-search"),
  searchClear: $("search-clear"),
  projectList: $("project-list"),
  projectFilter: $("project-filter"),
  typeFilter: $("type-filter"),
  resultRegion: $("memory-results"),
  resultTotal: $("result-total"),
  memoryHeading: $("memory-heading"),
  memorySubtitle: $("memory-subtitle"),
  detail: $("detail-content"),
  sidebarToggle: $("sidebar-toggle"),
  sidebarScrim: $("sidebar-scrim"),
  dialog: $("memory-dialog"),
  form: $("memory-form"),
  formError: $("form-error"),
  submit: $("submit-memory"),
  adapterRegion: $("adapter-results"),
  adapterCount: $("adapter-count"),
  historyRegion: $("history-results"),
  gitSubtitle: $("git-subtitle"),
  gitBadge: $("git-badge"),
};

function icon(name, className = "icon") {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("class", className);
  svg.setAttribute("aria-hidden", "true");
  const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
  use.setAttribute("href", `#icon-${name}`);
  svg.appendChild(use);
  return svg;
}

function element(tag, className, text) {
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
  const root = state.root || "";
  if (root && path.startsWith(root + "/")) return path.slice(root.length + 1);
  return path;
}

function formatError(error, fallback = "请求失败，请稍后重试。") {
  if (error instanceof TypeError && /fetch|network|load/i.test(error.message)) {
    return "无法连接本地 hub。请确认 `marktoast serve` 正在运行。";
  }
  const message = error && error.message ? String(error.message) : "";
  return message || fallback;
}

function formatDate(value) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(date).replaceAll("/", "-");
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
      // Some hub errors have no JSON body.
    }
    throw new Error(detail || `HTTP ${response.status} ${response.statusText}`.trim());
  }
  return response.json();
}

function setHubState(kind, text) {
  els.hubState.classList.remove("is-online", "is-offline");
  if (kind) els.hubState.classList.add(`is-${kind}`);
  els.hubStateText.textContent = text;
  const metaState = $("repo-meta-state");
  metaState.textContent = kind === "online" ? "已连接" : kind === "offline" ? "离线" : "检查中";
  metaState.classList.toggle("is-offline", kind === "offline");
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
  }, 3600);
}

function setButtonLoading(button, loading, loadingText = "处理中…") {
  if (!button) return;
  if (loading) {
    const label = button.querySelector("span");
    button.dataset.original = label ? label.textContent : button.textContent.trim();
    button.disabled = true;
    if (label) label.textContent = loadingText;
    else button.textContent = loadingText;
  } else {
    button.disabled = false;
    const label = button.querySelector("span");
    if (button.dataset.original) {
      if (label) label.textContent = button.dataset.original;
      else button.textContent = button.dataset.original;
    }
  }
}

function makeStatePanel({ kind = "empty", title, message, actionLabel, onAction, command }) {
  const panel = element("div", `state-panel${kind === "error" ? " is-error" : ""}`);
  const inner = element("div", "state-inner");
  const visual = element("div", "state-visual");
  visual.setAttribute("aria-hidden", "true");
  visual.append(
    element("span", "state-ring one"),
    element("span", "state-ring two"),
    element("span", "state-ring three"),
    element("span", "state-axis"),
  );
  inner.appendChild(visual);
  inner.appendChild(element("h2", "", title));

  const paragraph = element("p");
  if (command) {
    const parts = String(message).split("`");
    parts.forEach((part, index) => {
      paragraph.appendChild(index % 2 ? element("code", "", part) : document.createTextNode(part));
    });
  } else {
    paragraph.textContent = message;
  }
  inner.appendChild(paragraph);

  if (actionLabel && onAction) {
    const actions = element("div", "state-actions");
    const button = element("button", "button button-quiet", actionLabel);
    button.type = "button";
    button.addEventListener("click", onAction);
    actions.appendChild(button);
    inner.appendChild(actions);
  }
  panel.appendChild(inner);
  return panel;
}

function renderSkeleton(count = 7) {
  clear(els.resultRegion);
  const list = element("div", "skeleton-list");
  for (let index = 0; index < count; index += 1) {
    const row = element("div", "skeleton-row");
    row.append(
      element("div", "skeleton-line title"),
      element("div", "skeleton-line path"),
      element("div", "skeleton-line body"),
      element("div", "skeleton-line short"),
    );
    list.appendChild(row);
  }
  els.resultRegion.appendChild(list);
  els.resultRegion.setAttribute("aria-busy", "true");
}

function renderDetailSkeleton() {
  clear(els.detail);
  const wrapper = element("div", "detail-skeleton");
  wrapper.append(
    element("div", "skeleton-line detail-loading-title"),
    element("div", "skeleton-line detail-loading-line"),
    element("div", "skeleton-line detail-loading-line"),
    element("div", "detail-loading-box"),
    element("div", "skeleton-line detail-loading-line"),
    element("div", "skeleton-line short"),
  );
  els.detail.className = "";
  els.detail.appendChild(wrapper);
}

async function loadStatus({ announce = false } = {}) {
  setHubState("", "正在连接本地 hub");
  try {
    const data = await api("/api/status");
    state.status = data;
    state.root = data.root || "";
    setHubState("online", `本地 hub 在线 · ${Number(data.facts) || 0} 条记忆`);
    $("all-count").textContent = String(data.facts ?? 0);
    $("project-total").textContent = String(data.projects?.length ?? 0);
    $("repo-root").textContent = data.root || "—";
    $("repo-root").title = data.root || "";
    $("repo-version").textContent = `${data.version || "MarkToast"} · index ${formatDate(data.index?.builtAt)}`;
    renderProjects(data.projects || []);
    renderProjectOptions(data.projects || []);
    renderAdapterMiniatures(data.adapters || []);
    if (announce) toast("已连接到本地 hub");
    return data;
  } catch (error) {
    state.status = null;
    setHubState("offline", "hub 未连接 · 运行 marktoast serve");
    $("repo-root").textContent = "尚未连接本地仓库";
    $("repo-version").textContent = "MarkToast · offline";
    renderProjectsOffline();
    renderAdapterMiniatures([]);
    if (state.view === "memories") renderMemoryError(error);
    if (announce) toast(formatError(error), "error");
    return null;
  }
}

function renderProjects(projects) {
  clear(els.projectList);
  if (!projects.length) {
    els.projectList.appendChild(element("p", "repo-root", "暂无项目"));
    return;
  }
  projects
    .slice()
    .sort((a, b) => String(a.key).localeCompare(String(b.key), "zh-CN"))
    .forEach((project) => {
      const button = element("button", `project-row${state.project === project.key ? " is-selected" : ""}`);
      button.type = "button";
      button.dataset.project = project.key;
      button.title = project.key;
      button.append(
        icon("folder"),
        element("span", "project-name", project.key),
        element("span", "project-facts", String(project.facts ?? 0)),
      );
      button.addEventListener("click", () => selectProject(project.key));
      els.projectList.appendChild(button);
    });
}

function renderProjectsOffline() {
  clear(els.projectList);
  const button = element("button", "project-row");
  button.type = "button";
  button.append(icon("folder"), element("span", "project-name", "等待本地 hub"));
  button.addEventListener("click", () => loadStatus({ announce: true }));
  els.projectList.appendChild(button);
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
    list.appendChild(element("p", "repo-root", "无可用适配器"));
    return;
  }
  adapters.slice(0, 5).forEach((adapter) => {
    const item = element("div", "adapter-mini");
    item.title = adapter.title || adapter.id;
    item.append(
      element("span", "adapter-dot"),
      element("span", "adapter-mini-name", adapter.title || adapter.id),
      element("span", "tier-label", adapter.tier || ""),
    );
    list.appendChild(item);
  });
}

function selectProject(project) {
  state.project = project;
  els.projectFilter.value = project;
  document.querySelectorAll(".project-row").forEach((row) => {
    row.classList.toggle("is-selected", row.dataset.project === project);
  });
  $("all-memories").classList.toggle("is-active", !project);
  $("recent-memories").classList.remove("is-active");
  closeSidebar();
  switchView("memories");
  loadMemories();
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
}

async function loadMemories({ preserveSelection = true, silent = false } = {}) {
  const requestId = ++state.requestId;
  if (state.searchController) state.searchController.abort();
  state.searchController = new AbortController();
  updateHeading();
  if (!silent) renderSkeleton();
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
    renderResults();
    els.resultRegion.setAttribute("aria-busy", "false");

    const stillExists = state.selectedPath && state.results.some((item) => item.path === state.selectedPath);
    if (!preserveSelection || !stillExists) {
      state.selectedPath = "";
      state.detail = null;
      renderDetailPlaceholder();
    }
  } catch (error) {
    if (error.name === "AbortError" || requestId !== state.requestId) return;
    state.results = [];
    renderMemoryError(error);
    els.resultRegion.setAttribute("aria-busy", "false");
  }
}

function sortResults(results) {
  const copy = results.slice();
  if (state.sort === "updated") {
    copy.sort((a, b) => new Date(b.updated || 0).getTime() - new Date(a.updated || 0).getTime());
  } else if (state.query) {
    copy.sort((a, b) => Number(b.score || 0) - Number(a.score || 0));
  } else {
    copy.sort((a, b) => new Date(b.updated || 0).getTime() - new Date(a.updated || 0).getTime());
  }
  return copy;
}

function renderResults() {
  clear(els.resultRegion);
  els.resultTotal.textContent = state.results.length ? `共 ${state.results.length} 条` : "0 条";
  if (!state.results.length) {
    const title = state.query ? "没有找到匹配的记忆" : state.project ? "这个项目还没有记忆" : "仓库还是空的";
    const message = state.query
      ? "换一个更短的关键词，或清除项目与类型筛选后再试。"
      : "写下第一条记忆，让不同的 Agent 从这里接上上下文。";
    els.resultRegion.appendChild(makeStatePanel({
      title,
      message,
      actionLabel: state.query ? "清除筛选" : "写一条记忆",
      onAction: state.query ? clearFilters : openDialog,
    }));
    return;
  }

  const list = element("div", "result-list");
  state.results.forEach((result) => {
    const item = element("button", `result-item${result.path === state.selectedPath ? " is-selected" : ""}`);
    item.type = "button";
    item.dataset.path = result.path;
    item.setAttribute("aria-label", `查看记忆：${result.name || result.path}`);

    const top = element("div", "result-topline");
    top.append(
      element("span", "result-name", result.name || fileName(result.path)),
      element("span", "result-date", relativeDate(result.updated)),
    );
    item.appendChild(top);
    item.appendChild(element("p", "result-path mono", shortPath(result.path)));

    if (result.description || result.snippet) {
      item.appendChild(element("p", "result-description", result.description || cleanSnippet(result.snippet)));
    }

    const meta = element("div", "result-meta");
    meta.appendChild(element("span", "type-tag", result.type || "memory"));
    if (result.project) meta.appendChild(element("span", "", result.project));
    if (state.query && result.score !== undefined && result.score !== null) {
      meta.appendChild(element("span", "score", `相关度 ${Number(result.score).toFixed(2)}`));
    }
    item.appendChild(meta);
    item.addEventListener("click", () => selectMemory(result.path));
    list.appendChild(item);
  });
  els.resultRegion.appendChild(list);
}

function fileName(path) {
  return String(path || "未命名记忆").split("/").pop().replace(/\.md$/i, "");
}

function cleanSnippet(snippet) {
  return String(snippet || "").replace(/\s+/g, " ").trim();
}

function renderMemoryError(error) {
  clear(els.resultRegion);
  els.resultTotal.textContent = "不可用";
  els.resultRegion.appendChild(makeStatePanel({
    kind: "error",
    title: state.status ? "记忆加载失败" : "本地 hub 未连接",
    message: formatError(error),
    command: !state.status,
    actionLabel: "重新连接",
    onAction: async () => {
      const status = await loadStatus({ announce: true });
      if (status) loadMemories();
    },
  }));
}

async function selectMemory(path) {
  if (!path) return;
  state.selectedPath = path;
  document.querySelectorAll(".result-item").forEach((item) => {
    item.classList.toggle("is-selected", item.dataset.path === path);
  });
  renderDetailSkeleton();
  try {
    const detail = await api(`/api/memory?path=${encodeURIComponent(path)}`);
    if (state.selectedPath !== path || state.view !== "memories") return;
    state.detail = detail;
    renderDetail(detail);
    if (window.innerWidth <= 1040) {
      $("detail-panel").scrollIntoView({ behavior: "smooth", block: "start" });
    }
  } catch (error) {
    if (state.selectedPath !== path) return;
    renderDetailError(error, path);
  }
}

function renderDetailPlaceholder() {
  clear(els.detail);
  els.detail.className = "detail-placeholder";
  const mark = element("div", "detail-empty-mark");
  mark.setAttribute("aria-hidden", "true");
  mark.appendChild(element("span", "", "◍"));
  els.detail.append(
    mark,
    element("h2", "", "选择一条记忆"),
    element("p", "", "从结果列表中选择记忆，在这里阅读正文、frontmatter、路径与仓库历史。"),
  );
  const hint = element("div", "keyboard-hint");
  hint.append(element("kbd", "", "⌘"), element("kbd", "", "K"), element("span", "", "快速搜索"));
  els.detail.appendChild(hint);
}

function renderDetail(detail) {
  clear(els.detail);
  els.detail.className = "detail-view";

  const topbar = element("div", "detail-topbar");
  const breadcrumb = element("div", "detail-breadcrumb");
  breadcrumb.title = detail.path || "";
  breadcrumb.append(icon("folder"), element("span", "", shortPath(detail.path)));
  const copyButton = element("button", "icon-button");
  copyButton.type = "button";
  copyButton.title = "复制路径";
  copyButton.setAttribute("aria-label", "复制记忆路径");
  copyButton.appendChild(icon("copy"));
  copyButton.addEventListener("click", () => copyPath(detail.path, copyButton));
  topbar.append(breadcrumb, copyButton);

  const scroller = element("div", "detail-scroll");
  const titleRow = element("div", "detail-title-row");
  const titleIcon = element("span", "detail-title-icon");
  titleIcon.appendChild(icon("file"));
  titleRow.append(titleIcon, element("h1", "detail-title", detail.name || fileName(detail.path)));
  scroller.appendChild(titleRow);
  if (detail.description) scroller.appendChild(element("p", "detail-description", detail.description));

  const tags = element("div", "detail-tags");
  [detail.type, detail.project].filter(Boolean).forEach((value) => {
    tags.appendChild(element("span", "detail-tag", value));
  });
  if (detail.updated) tags.appendChild(element("span", "detail-tag", formatDate(detail.updated)));
  scroller.appendChild(tags);

  const parsed = parseFrontmatter(detail.raw || "");
  const fmSection = element("section", "detail-section");
  const fmTitle = element("h2", "detail-section-title");
  const fmChevron = icon("chevron-down");
  fmChevron.style.width = "12px";
  fmChevron.style.height = "12px";
  fmTitle.append(fmChevron, document.createTextNode("frontmatter"));
  fmSection.appendChild(fmTitle);
  fmSection.appendChild(renderFrontmatter(parsed.values));
  scroller.appendChild(fmSection);

  const bodySection = element("section", "detail-section");
  const bodyTitle = element("h2", "detail-section-title");
  bodyTitle.append(icon("file"), document.createTextNode("正文"));
  const markdown = element("div", "markdown-body");
  renderMarkdown(markdown, parsed.body || detail.body || "");
  bodySection.append(bodyTitle, markdown);
  scroller.appendChild(bodySection);

  const pathSection = element("section", "detail-section");
  const pathTitle = element("h2", "detail-section-title");
  pathTitle.append(icon("folder"), document.createTextNode("路径"));
  const pathBox = element("div", "path-box");
  pathBox.appendChild(element("code", "", shortPath(detail.path)));
  const inlineCopy = element("button", "icon-button");
  inlineCopy.type = "button";
  inlineCopy.title = "复制路径";
  inlineCopy.setAttribute("aria-label", "复制路径");
  inlineCopy.appendChild(icon("copy"));
  inlineCopy.addEventListener("click", () => copyPath(detail.path, inlineCopy));
  pathBox.appendChild(inlineCopy);
  pathSection.append(pathTitle, pathBox);
  scroller.appendChild(pathSection);

  const historySection = element("section", "detail-section");
  const historyTitle = element("h2", "detail-section-title");
  historyTitle.append(icon("branch"), document.createTextNode("Git 历史"));
  historySection.append(historyTitle, renderCompactHistory());
  scroller.appendChild(historySection);
  els.detail.append(topbar, scroller);
}

function parseFrontmatter(raw) {
  const text = String(raw || "");
  if (!text.startsWith("---")) return { values: [], body: text };
  const close = text.indexOf("\n---", 3);
  if (close < 0) return { values: [], body: text };
  const header = text.slice(3, close).trim();
  const body = text.slice(close + 4).replace(/^\r?\n/, "");
  const values = [];
  header.split(/\r?\n/).forEach((line) => {
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
  if (!values.length) return element("div", "frontmatter-empty", "这条记忆没有 frontmatter");
  const table = element("table", "frontmatter-table");
  const body = document.createElement("tbody");
  values.forEach(([key, value]) => {
    const row = document.createElement("tr");
    row.append(element("th", "", key), element("td", "", value.replace(/^\[|\]$/g, "")));
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

    const fence = line.match(/^```(.*)$/);
    if (fence) {
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
      const level = Math.min(heading[1].length, 6);
      const h = document.createElement(`h${level}`);
      appendInline(h, heading[2]);
      container.appendChild(h);
      index += 1;
      continue;
    }

    const unordered = line.match(/^\s*[-*+]\s+(.+)$/);
    const ordered = line.match(/^\s*\d+[.)]\s+(.+)$/);
    if (unordered || ordered) {
      const list = document.createElement(ordered ? "ol" : "ul");
      while (index < lines.length) {
        const current = lines[index];
        const match = ordered
          ? current.match(/^\s*\d+[.)]\s+(.+)$/)
          : current.match(/^\s*[-*+]\s+(.+)$/);
        if (!match) break;
        const li = document.createElement("li");
        appendInline(li, match[1]);
        list.appendChild(li);
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
      const strong = document.createElement("strong");
      strong.textContent = token.slice(2, -2);
      parent.appendChild(strong);
    } else if (token.startsWith("`")) {
      const code = document.createElement("code");
      code.textContent = token.slice(1, -1);
      parent.appendChild(code);
    } else {
      const linkMatch = token.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
      if (linkMatch) {
        const href = safeUrl(linkMatch[2]);
        if (href) {
          const link = document.createElement("a");
          link.href = href;
          link.textContent = linkMatch[1];
          link.target = "_blank";
          link.rel = "noreferrer noopener";
          parent.appendChild(link);
        } else {
          parent.appendChild(document.createTextNode(linkMatch[1]));
        }
      }
    }
    lastIndex = pattern.lastIndex;
  }
  if (lastIndex < source.length) {
    parent.appendChild(document.createTextNode(source.slice(lastIndex)));
  }
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

function renderCompactHistory() {
  if (!state.history.length) {
    return element("div", "frontmatter-empty", "Git 历史尚未加载，打开“历史”标签查看。");
  }
  const wrapper = element("div", "commit-list");
  state.history.slice(0, 3).forEach((commit) => {
    const item = element("article", "commit-item");
    item.appendChild(element("p", "commit-subject", commit.subject || "无提交说明"));
    const meta = element("div", "commit-meta");
    meta.append(
      element("span", "", String(commit.hash || "").slice(0, 7)),
      element("span", "", formatDate(commit.date)),
    );
    item.appendChild(meta);
    wrapper.appendChild(item);
  });
  return wrapper;
}

async function copyPath(path, button) {
  if (!path) return;
  try {
    await navigator.clipboard.writeText(path);
    button.title = "已复制";
    toast("路径已复制到剪贴板");
    window.setTimeout(() => {
      button.title = "复制路径";
    }, 1400);
  } catch {
    toast("复制失败，请手动选择路径", "error");
  }
}

function renderDetailError(error, path) {
  clear(els.detail);
  els.detail.className = "";
  const wrapper = element("div", "detail-error");
  const visual = element("div", "state-visual");
  visual.setAttribute("aria-hidden", "true");
  visual.append(
    element("span", "state-ring one"),
    element("span", "state-ring two"),
    element("span", "state-ring three"),
    element("span", "state-axis"),
  );
  const title = element("h2", "", "无法读取这条记忆");
  const message = element("p", "", formatError(error));
  const retry = element("button", "button button-quiet", "重试");
  retry.type = "button";
  retry.addEventListener("click", () => selectMemory(path));
  wrapper.append(visual, title, message, retry);
  els.detail.appendChild(wrapper);
}

async function loadAdapters() {
  clear(els.adapterRegion);
  els.adapterRegion.appendChild(makeStatePanel({
    title: "正在读取适配器",
    message: "检查本机可用的 Agent 连接方式…",
  }));
  try {
    const data = await api("/api/adapters");
    state.adapters = Array.isArray(data.adapters) ? data.adapters : [];
    renderAdapters();
    return true;
  } catch (error) {
    clear(els.adapterRegion);
    els.adapterCount.textContent = "不可用";
    els.adapterRegion.appendChild(makeStatePanel({
      kind: "error",
      title: "适配器加载失败",
      message: formatError(error),
      actionLabel: "重试",
      onAction: loadAdapters,
    }));
    return false;
  }
}

function renderAdapters() {
  clear(els.adapterRegion);
  els.adapterCount.textContent = `${state.adapters.length} 个`;
  if (!state.adapters.length) {
    els.adapterRegion.appendChild(makeStatePanel({
      title: "还没有适配器",
      message: "当前 hub 没有报告可用的 Agent 适配器。",
    }));
    return;
  }

  state.adapters.forEach((adapter) => {
    const card = element("article", "adapter-card");
    const head = element("div", "adapter-card-head");
    const identity = element("div", "adapter-title-wrap");
    const iconBox = element("span", "adapter-icon");
    iconBox.appendChild(icon("plug"));
    const copy = element("div");
    copy.append(
      element("h2", "adapter-title", adapter.title || adapter.id),
      element("p", "adapter-id", adapter.id || "adapter"),
    );
    identity.append(iconBox, copy);
    head.append(identity, element("span", "tier-badge", adapter.tier || "tier"));
    card.appendChild(head);
    if (adapter.notes) card.appendChild(element("p", "adapter-notes", adapter.notes));

    const capabilities = Array.isArray(adapter.capabilities) ? adapter.capabilities : [];
    if (capabilities.length) {
      const capabilityList = element("div", "capability-list");
      capabilities.forEach((capability) => capabilityList.appendChild(element("span", "capability", capability)));
      card.appendChild(capabilityList);
    }

    const metadata = [
      ["hooks", adapter.hooks?.events?.join?.(", ") || adapter.hooks?.events || adapter.hooks?.install_kind],
      ["injection", adapter.hooks?.injection],
      ["mcp", adapter.mcp?.kind],
      ["target", adapter.mcp?.target],
      ["detect", Array.isArray(adapter.detect) ? adapter.detect.join(", ") : adapter.detect],
    ].filter(([, value]) => value);
    if (metadata.length) {
      const grid = element("div", "adapter-meta-grid");
      metadata.slice(0, 4).forEach(([label, value]) => {
        const meta = element("div", "adapter-meta");
        meta.append(element("span", "", label), element("code", "", value));
        grid.appendChild(meta);
      });
      card.appendChild(grid);
    }
    els.adapterRegion.appendChild(card);
  });
}

async function loadHistory() {
  clear(els.historyRegion);
  els.gitBadge.textContent = "检查中";
  els.gitBadge.className = "git-badge";
  els.gitSubtitle.textContent = "正在读取仓库提交…";
  els.historyRegion.appendChild(makeStatePanel({
    title: "正在读取 Git 历史",
    message: "检查本地仓库的提交记录…",
  }));

  const status = state.status || await loadStatus();
  try {
    const data = await api("/api/git/log?n=20");
    state.history = Array.isArray(data.commits) ? data.commits : [];
    renderHistory(status);
    return true;
  } catch (error) {
    clear(els.historyRegion);
    els.gitBadge.textContent = "不可用";
    els.gitBadge.className = "git-badge is-offline";
    els.gitSubtitle.textContent = "Git 历史读取失败。";
    els.historyRegion.appendChild(makeStatePanel({
      kind: "error",
      title: "Git 历史加载失败",
      message: formatError(error),
      actionLabel: "重试",
      onAction: loadHistory,
    }));
    return false;
  }
}

function renderHistory(status) {
  clear(els.historyRegion);
  const git = status?.git;
  if (git?.available === false) {
    els.gitBadge.textContent = "Git 不可用";
    els.gitBadge.className = "git-badge is-offline";
    els.gitSubtitle.textContent = "当前目录没有可读取的 Git 仓库。";
  } else if (git?.clean === false) {
    els.gitBadge.textContent = `${git.changes ?? 0} 项变更`;
    els.gitBadge.className = "git-badge has-changes";
    els.gitSubtitle.textContent = "仓库有未提交的变更，记忆可能在下次提交前暂时不同步。";
  } else {
    els.gitBadge.textContent = "工作区干净";
    els.gitBadge.className = "git-badge";
    els.gitSubtitle.textContent = "所有记忆变更都可追溯、可回退。";
  }

  if (!state.history.length) {
    els.historyRegion.appendChild(makeStatePanel({
      title: "还没有提交记录",
      message: "第一次保存并提交记忆后，Git 历史会出现在这里。",
    }));
    return;
  }
  const list = element("div", "commit-list");
  state.history.forEach((commit) => {
    const item = element("article", "commit-item");
    item.appendChild(element("p", "commit-subject", commit.subject || "无提交说明"));
    const meta = element("div", "commit-meta");
    meta.append(
      element("span", "", String(commit.hash || "").slice(0, 8)),
      element("span", "", formatDate(commit.date)),
    );
    item.appendChild(meta);
    list.appendChild(item);
  });
  els.historyRegion.appendChild(list);
}

function switchView(view) {
  state.view = view;
  document.querySelectorAll("[data-view]").forEach((tab) => {
    const active = tab.dataset.view === view;
    tab.classList.toggle("is-active", active);
    tab.setAttribute("aria-selected", String(active));
  });
  document.querySelectorAll(".view").forEach((panel) => {
    const active = panel.id === `view-${view}`;
    panel.classList.toggle("is-active", active);
    panel.hidden = !active;
  });
  if (view === "adapters" && !state.adapters.length) loadAdapters();
  if (view === "history") loadHistory();
  if (view === "memories" && !state.results.length) loadMemories();
}

function clearFilters() {
  state.query = "";
  state.project = "";
  state.type = "";
  state.selectedPath = "";
  state.detail = null;
  els.search.value = "";
  els.searchClear.hidden = true;
  els.projectFilter.value = "";
  els.typeFilter.value = "";
  document.querySelectorAll(".project-row").forEach((row) => row.classList.remove("is-selected"));
  $("all-memories").classList.add("is-active");
  renderDetailPlaceholder();
  loadMemories({ preserveSelection: false });
}

function openDialog() {
  clear(els.formError);
  els.formError.hidden = true;
  $("memory-project").value = state.project || "";
  $("memory-type").value = state.type || "project";
  $("memory-name").value = "";
  $("memory-description").value = "";
  $("memory-body").value = "";
  els.dialog.showModal();
  window.setTimeout(() => $("memory-project").focus(), 40);
}

function closeDialog() {
  if (els.dialog.open) els.dialog.close();
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
    els.projectFilter.value = payload.project;
    state.query = "";
    els.search.value = "";
    switchView("memories");
    state.selectedPath = result.path || "";
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

function openSidebar() {
  if (window.innerWidth > 760) return;
  document.body.classList.add("sidebar-open");
  els.sidebarToggle.setAttribute("aria-expanded", "true");
  els.sidebarScrim.hidden = false;
  window.setTimeout(() => $("all-memories").focus(), 220);
}

function closeSidebar() {
  document.body.classList.remove("sidebar-open");
  els.sidebarToggle.setAttribute("aria-expanded", "false");
  els.sidebarScrim.hidden = true;
}

function focusSearch() {
  els.search.focus();
  els.search.select();
}

function bindEvents() {
  els.search.addEventListener("input", () => {
    const query = els.search.value.trim();
    state.query = query;
    els.searchClear.hidden = !query;
    window.clearTimeout(state.searchTimer);
    state.searchTimer = window.setTimeout(() => {
      loadMemories({ preserveSelection: false });
    }, 250);
  });

  els.searchClear.addEventListener("click", () => {
    els.search.value = "";
    state.query = "";
    els.searchClear.hidden = true;
    els.search.focus();
    loadMemories({ preserveSelection: false });
  });

  document.addEventListener("keydown", (event) => {
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
      event.preventDefault();
      focusSearch();
    }
    if (event.key === "Escape") {
      closeSidebar();
      if (document.activeElement === els.search) els.search.blur();
    }
  });

  $("all-memories").addEventListener("click", () => {
    state.query = "";
    state.type = "";
    els.search.value = "";
    els.searchClear.hidden = true;
    els.typeFilter.value = "";
    selectProject("");
  });
  $("recent-memories").addEventListener("click", () => {
    state.project = "";
    state.query = "";
    state.sort = "updated";
    $("sort-label").textContent = "最近更新";
    els.search.value = "";
    els.searchClear.hidden = true;
    els.projectFilter.value = "";
    document.querySelectorAll(".project-row").forEach((row) => row.classList.remove("is-selected"));
    $("all-memories").classList.remove("is-active");
    $("recent-memories").classList.add("is-active");
    closeSidebar();
    switchView("memories");
    loadMemories({ preserveSelection: false });
  });

  els.projectFilter.addEventListener("change", () => {
    state.project = els.projectFilter.value;
    renderProjects(state.status?.projects || []);
    $("all-memories").classList.toggle("is-active", !state.project);
    $("recent-memories").classList.remove("is-active");
    loadMemories({ preserveSelection: false });
  });

  els.typeFilter.addEventListener("change", () => {
    state.type = els.typeFilter.value;
    loadMemories({ preserveSelection: false });
  });

  $("sort-button").addEventListener("click", () => {
    state.sort = state.sort === "relevance" ? "updated" : "relevance";
    $("sort-label").textContent = state.sort === "relevance" ? "相关度" : "最近更新";
    state.results = sortResults(state.results);
    renderResults();
  });

  document.querySelectorAll("[data-view]").forEach((tab) => {
    tab.addEventListener("click", () => switchView(tab.dataset.view));
  });

  $("refresh-view").addEventListener("click", async () => {
    const button = $("refresh-view");
    button.disabled = true;
    let refreshed = false;
    if (state.view === "memories") {
      const status = await loadStatus();
      if (status) {
        await loadMemories();
        refreshed = true;
      }
    } else if (state.view === "adapters") {
      state.adapters = [];
      refreshed = await loadAdapters();
    } else {
      await loadStatus();
      refreshed = await loadHistory();
    }
    button.disabled = false;
    if (refreshed) toast("已刷新");
    else toast("刷新失败，请确认本地 hub 正在运行", "error");
  });

  $("view-adapters").addEventListener("click", () => switchView("adapters"));
  $("new-memory-top").addEventListener("click", openDialog);
  $("close-dialog").addEventListener("click", closeDialog);
  $("cancel-dialog").addEventListener("click", closeDialog);
  els.form.addEventListener("submit", submitMemory);
  els.dialog.addEventListener("click", (event) => {
    if (event.target === els.dialog) closeDialog();
  });

  els.sidebarToggle.addEventListener("click", () => {
    if (document.body.classList.contains("sidebar-open")) closeSidebar();
    else openSidebar();
  });
  els.sidebarScrim.addEventListener("click", closeSidebar);
  window.addEventListener("resize", () => {
    if (window.innerWidth > 760) closeSidebar();
  });
  window.addEventListener("online", () => loadStatus({ announce: true }));
  window.addEventListener("offline", () => setHubState("offline", "浏览器已离线"));
}

async function init() {
  bindEvents();
  renderDetailPlaceholder();
  const status = await loadStatus();
  if (status) {
    await loadMemories();
    loadHistory();
  } else {
    renderMemoryError(new Error("无法连接本地 hub。请运行 `marktoast serve` 后重试。"));
  }
}

init();
