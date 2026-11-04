// MarkToast web UI — dependency-free and served directly by the local hub.

"use strict";

const $ = (id) => document.getElementById(id);
const tr = (text) => (window.MarkToastI18n ? window.MarkToastI18n.t(text) : text);

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
  inbox: [],
  history: [],
  aliases: {},
  settings: null,
  memoryHistory: {
    path: "",
    open: false,
    loading: false,
    commits: [],
    error: null,
    selectedHash: "",
    patch: "",
    patchError: null,
    patchLoading: false,
    restoring: false,
  },
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
  search: $("global-search"),
  searchShell: $("search-shell"),
  searchToggle: $("search-toggle"),
  searchClear: $("search-clear"),
  projectList: $("project-list"),
  projectFilter: $("project-filter"),
  typeFilter: $("type-filter"),
  resultRegion: $("memory-results"),
  resultTotal: $("result-total"),
  adapterRegion: $("adapter-results"),
  adapterCount: $("adapter-count"),
  historyRegion: $("history-results"),
  modal: $("memory-modal"),
  form: $("memory-form"),
  formError: $("form-error"),
  submit: $("submit-memory"),
};

if (/Mac|iPhone|iPad/.test(navigator.platform)) document.body.classList.add("mac");
const desktopShell = Boolean(
  window._wails ||
  /^wails:/.test(location.protocol) ||
  location.hostname.includes("wails") ||
  /Wails/i.test(navigator.userAgent),
);
if (desktopShell) document.body.classList.add("desktop");
try {
  if (localStorage.getItem("marktoast-sidebar") === "collapsed") document.body.classList.add("side-collapsed");
} catch (error) {}

const THEME_KEY = "marktoast-theme";
const themeMedia = matchMedia("(prefers-color-scheme: dark)");

function themePreference() {
  try {
    return localStorage.getItem(THEME_KEY) || "auto";
  } catch {
    return "auto";
  }
}

function applyTheme(preference = themePreference()) {
  const dark = preference === "dark" || (preference === "auto" && themeMedia.matches);
  document.documentElement.dataset.theme = dark ? "dark" : "light";
  document.documentElement.style.colorScheme = dark ? "dark" : "light";
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute("content", dark ? "#1a1a1e" : "#f4f4f6");
  document.querySelectorAll("[data-theme-choice]").forEach((button) => {
    button.classList.toggle("on", button.dataset.themeChoice === preference);
  });
}

themeMedia.addEventListener("change", () => {
  if (themePreference() === "auto") applyTheme("auto");
});

function setTheme(preference) {
  try {
    localStorage.setItem(THEME_KEY, preference);
  } catch {
    // Private mode or a locked-down shell: apply for this session only.
  }
  document.documentElement.classList.add("theming");
  applyTheme(preference);
  window.setTimeout(() => document.documentElement.classList.remove("theming"), 380);
}

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
  const locale = window.MarkToastI18n?.language === "en" ? "en-US" : "zh-CN";
  return new Intl.DateTimeFormat(locale, options).format(date).replaceAll("/", "-");
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
  els.hubStateText.textContent = kind === "online" ? "在线" : kind === "offline" ? "离线" : "连接中";
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
  button.classList.toggle("is-loading", Boolean(loading));
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
    setHubState("online", "在线");
    await loadAliases();
    $("all-count").textContent = String(data.facts ?? 0);
    $("inbox-count").textContent = Number(data.inbox) ? String(data.inbox) : "";
  requestAnimationFrame(positionSideHighlight);
    $("repo-count").textContent = `${Number(data.facts) || 0} 条`;
    $("repo-count").title = `${Number(data.facts) || 0} 条记忆`;
    $("repo-status").title = data.root || "";
    renderProjects(data.projects || []);
    renderProjectOptions(data.projects || []);
    if (announce) toast("已连接到本地 hub");
    return data;
  } catch (error) {
    state.status = null;
    setHubState("offline", "离线");
    showHubAlert(formatError(error));
    $("repo-count").textContent = "";
    renderProjects([]);
    renderProjectOptions([]);
    if (state.view === "memories" && !state.results.length) renderMemoryError(error);
    if (announce) toast(formatError(error), "error");
    return null;
  }
}

const TYPE_LABELS = { user: "用户", feedback: "反馈", project: "项目", reference: "参考", decision: "决策" };

function typeLabel(type) {
  return TYPE_LABELS[type] || type || "记忆";
}

function commitLabel(subject) {
  const value = String(subject || "");
  const mem = value.match(/^mem\(([^)]+)\):\s*(.*)$/);
  if (mem) {
    const project = projectLabel(mem[1]);
    const action = mem[2]
      .replace(/^add\s+/, "新增 ")
      .replace(/^save\s+/, "保存 ")
      .replace(/^reindex$/, "重建索引");
    return `${project} · ${action}`;
  }
  const restore = value.match(/^restore\(([^)]+)\):\s*(.*)$/);
  if (restore) return `${projectLabel(restore[1])} · 恢复 ${restore[2]}`;
  return value;
}

function projectLabel(key) {
  const value = String(key || "");
  if (state.aliases && state.aliases[value]) return state.aliases[value];
  const match = value.match(/^(.*)-[0-9a-f]{16}$/);
  return match ? match[1] : value;
}

async function loadAliases() {
  try {
    const data = await api("/api/aliases");
    state.aliases = data.aliases || {};
  } catch {
    state.aliases = {};
  }
  return state.aliases;
}

function renderProjects(projects) {
  clear(els.projectList);
  const total = $("project-total");
  if (total) total.textContent = projects.length ? String(projects.length) : "";
  if (!projects.length) {
    els.projectList.appendChild(element("div", "side-label", state.status ? "暂无项目" : "等待本地 hub"));
    return;
  }
  projects
    .slice()
    .sort((a, b) => (Number(b.facts) || 0) - (Number(a.facts) || 0) || projectLabel(a.key).localeCompare(projectLabel(b.key), "zh-CN"))
    .forEach((project) => {
      const button = element("button", `side-row project-row${state.project === project.key ? " is-active" : ""}`);
      button.type = "button";
      button.dataset.project = project.key;
      button.title = project.key;
      button.setAttribute("aria-pressed", String(state.project === project.key));
      button.append(
        icon("folder"),
        element("span", "side-name", projectLabel(project.key)),
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
    const option = element("option", "", projectLabel(project.key));
    option.value = project.key;
    els.projectFilter.appendChild(option);
  });
  els.projectFilter.value = projects.some((project) => project.key === current) ? current : "";
}

function selectProject(project) {
  state.project = project;
  state.query = "";
  els.search.value = "";
  els.searchClear.hidden = true;
  els.projectFilter.value = project;
  renderProjects(state.status?.projects || []);
  $("all-memories").classList.toggle("is-active", !project);
  switchView("memories");
  loadMemories({ preserveSelection: false });
}

// positionSideHighlight slides one white row shape to the active sidebar row,
// the way Magpie's navigation does, instead of toggling a background per row.
function positionSideHighlight() {
  const highlight = document.getElementById("side-hl");
  const scroll = document.querySelector(".side-scroll");
  if (!highlight || !scroll) return;
  const active = scroll.querySelector(".side-row.is-active, .side-row.on");
  if (!active || !active.getClientRects().length) {
    highlight.style.opacity = "0";
    return;
  }
  const scrollRect = scroll.getBoundingClientRect();
  const rowRect = active.getBoundingClientRect();
  const first = !highlight.dataset.ready;
  if (first) highlight.classList.add("still");
  highlight.style.opacity = "1";
  highlight.style.transform = `translate(${rowRect.left - scrollRect.left + scroll.scrollLeft}px, ${rowRect.top - scrollRect.top + scroll.scrollTop}px)`;
  highlight.style.width = `${rowRect.width}px`;
  highlight.style.height = `${rowRect.height}px`;
  highlight.style.borderRadius = getComputedStyle(active).borderRadius;
  document.body.classList.add("has-side-hl");
  if (first) {
    requestAnimationFrame(() => {
      highlight.dataset.ready = "1";
      highlight.classList.remove("still");
    });
  }
}

function updateBreadcrumb() {
  const crumb = document.getElementById("breadcrumb-current");
  if (!crumb) return;
  if (state.view !== "memories") crumb.textContent = VIEW_LABELS[state.view] || "全部记忆";
  else if (state.query) crumb.textContent = "搜索结果";
  else if (state.project) crumb.textContent = projectLabel(state.project);
  else crumb.textContent = "全部记忆";
}

function updateHeading() {
  $("sort-label").textContent = state.sort === "updated" ? "最近更新" : state.sort === "title" ? "标题" : "相关度";
  updateBreadcrumb();
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
  els.resultTotal.textContent = String(state.results.length);
  if (!state.results.length) {
    const list = element("div", "list");
    list.appendChild(emptyState(
      state.query ? "没有匹配" : "还没有记忆",
      state.query ? "换个关键词试试。" : "先写一条。",
      state.query ? "清除筛选" : "新建",
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
    who.dataset.noI18n = "true";
    who.append(
      element("div", "name", result.name || fileName(result.path)),
      element("div", "sub", result.description || cleanSnippet(result.snippet) || shortPath(result.path)),
    );
    row.appendChild(who);

    const meta = element("div", "memory-meta");
    const type = result.type || "memory";
    const typePill = element("span", `pill type-pill type-${type}`, typeLabel(type));
    typePill.title = type;
    meta.appendChild(typePill);
    if (!state.project && result.project) {
      const project = element("span", "memory-project", projectLabel(result.project));
      project.title = result.project;
      meta.appendChild(project);
    }
    meta.appendChild(element("span", "memory-time", relativeDate(result.updated)));
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

  const meta = element("div", "expanded-meta");
  const type = detail.type || result.type || "memory";
  const typePill = element("span", `pill type-pill type-${type}`, typeLabel(type));
  typePill.title = type;
  meta.appendChild(typePill);
  if (detail.project || result.project) {
    const project = element("span", "expanded-project", projectLabel(detail.project || result.project));
    project.title = detail.project || result.project;
    meta.appendChild(project);
  }
  meta.appendChild(element("span", "expanded-time", relativeDate(detail.updated || result.updated)));
  const path = detail.path || result.path;
  const actions = element("div", "memory-actions");
  const history = element("button", "icon copy-path", "");
  history.type = "button";
  history.title = "版本历史";
  history.setAttribute("aria-label", "查看这条记忆的版本历史");
  history.append(icon("branch"));
  history.addEventListener("click", () => toggleMemoryHistory(path));
  const copy = element("button", "icon copy-path", "");
  copy.type = "button";
  copy.title = path;
  copy.setAttribute("aria-label", "复制记忆路径");
  copy.append(icon("copy"));
  copy.addEventListener("click", () => copyPath(path));
  actions.append(history, copy);
  meta.appendChild(actions);
  box.appendChild(meta);

  const body = element("div", "markdown-body");
  renderMarkdown(body, detail.body || "");
  if (!body.textContent.trim()) body.appendChild(element("p", "muted", "没有正文。"));
  box.appendChild(body);
  const currentPath = detail.path || result.path;
  if (state.memoryHistory.open && state.memoryHistory.path === currentPath) {
    box.appendChild(renderMemoryHistory(currentPath));
  }
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

async function runAdapterAction(adapter, button) {
  const verb = adapter.installed ? "重新写入" : "写入";
  if (!window.confirm(tr(`${verb} ZCode 配置？会先备份原文件。`))) return;
  setButtonLoading(button, true);
  try {
    const data = await api("/api/adapters/action", {
      method: "POST",
      body: JSON.stringify({ id: adapter.id, action: "install" }),
    });
    toast(data.installed ? "ZCode 已接入" : "配置已更新");
    await loadAdapters();
    await loadStatus();
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
    setButtonLoading(button, false);
  }
}

async function stageAdapter(adapter, button) {
  setButtonLoading(button, true);
  try {
    const data = await api("/api/adapters/action", {
      method: "POST",
      body: JSON.stringify({ id: adapter.id, action: "stage" }),
    });
    try {
      await navigator.clipboard.writeText(data.dir);
      toast("片段已准备，路径已复制");
    } catch {
      toast(`片段已准备：${data.dir}`);
    }
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
    setButtonLoading(button, false);
  }
}

async function loadInbox() {
  const region = $("inbox-results");
  clear(region);
  const list = element("div", "list");
  list.append(
    element("div", "row loading-row"),
    element("div", "row loading-row"),
  );
  region.appendChild(list);
  try {
    const data = await api("/api/inbox");
    state.inbox = data.candidates || [];
    renderInbox();
  } catch (error) {
    clear(region);
    const list = element("div", "list");
    list.appendChild(emptyState("收件箱加载失败", formatError(error), "重试", loadInbox));
    region.appendChild(list);
  }
}

function renderInbox() {
  const region = $("inbox-results");
  clear(region);
  $("inbox-total").textContent = String(state.inbox.length);
  if (!state.inbox.length) {
    const list = element("div", "list");
    list.appendChild(emptyState("收件箱是空的", "会话结束后，候选记忆会出现在这里。"));
    region.appendChild(list);
    return;
  }
  const list = element("div", "list");
  state.inbox.forEach((candidate) => {
    const row = element("div", "row inbox-row");
    row.append(icon("inbox"));
    const who = element("div", "who");
    who.dataset.noI18n = "true";
    who.append(
      element("div", "name", candidate.name || "候选记忆"),
      element("div", "sub", candidate.description || ""),
    );
    const meta = element("div", "memory-meta");
    meta.append(
      element("span", "pill", candidate.agent || "agent"),
      element("span", "memory-time", relativeDate(candidate.created)),
    );
    const actions = element("div", "inbox-actions");
    const keep = element("button", "button primary compact", "保留");
    keep.type = "button";
    keep.addEventListener("click", () => promoteCandidate(candidate));
    const drop = element("button", "button quiet compact", "丢弃");
    drop.type = "button";
    drop.addEventListener("click", () => dismissCandidate(candidate));
    actions.append(keep, drop);
    row.append(who, meta, actions);
    list.appendChild(row);
  });
  region.appendChild(list);
}

async function promoteCandidate(candidate) {
  try {
    await api("/api/inbox/promote", {
      method: "POST",
      body: JSON.stringify({ path: candidate.path }),
    });
    toast("已保留");
    await loadStatus();
    await loadInbox();
  } catch (error) {
    toast(formatError(error), "error");
  }
}

async function dismissCandidate(candidate) {
  if (!window.confirm(tr("丢弃这条候选记忆？"))) return;
  try {
    await api("/api/inbox/dismiss", {
      method: "POST",
      body: JSON.stringify({ path: candidate.path }),
    });
    toast("已丢弃");
    await loadStatus();
    await loadInbox();
  } catch (error) {
    toast(formatError(error), "error");
  }
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
  els.adapterCount.textContent = String(state.adapters.length);
  if (!state.adapters.length) {
    const list = element("div", "list");
    list.appendChild(emptyState("没有适配器", "本地 hub 没有报告可用的 Agent。"));
    els.adapterRegion.appendChild(list);
    return;
  }
  const list = element("div", "list");
  state.adapters
    .slice()
    .sort((a, b) => (b.detected ? 1 : 0) - (a.detected ? 1 : 0) || String(a.title || a.id).localeCompare(String(b.title || b.id), "zh-CN"))
    .forEach((adapter) => {
      const entry = element("article", "adapter-entry");
      const row = element("div", "row adapter-row");
      row.append(icon("plug", "ic adapter-icon"));
      const who = element("div", "who");
      who.append(
        element("div", "name", adapter.title || adapter.id || "Adapter"),
        element("div", "sub", adapter.notes || adapter.id || ""),
      );
      const installed = Boolean(adapter.installed);
      const installable = Boolean(adapter.installable) && Boolean(adapter.detected);
      const pending = installable && !installed;
      const state = element(
        "span",
        `adapter-state${installed ? " is-on" : pending ? " is-pending" : ""}`,
        installed ? "已接入" : pending ? "待接入" : adapter.detected ? "已检测" : "未检测",
      );
      if (adapter.detected_path) state.title = adapter.detected_path;
      const tierLabel = { native: "原生", projection: "投影", readonly: "只读" }[adapter.tier] || adapter.tier || "";
      const tier = element("span", "pill", tierLabel);
      tier.title = adapter.tier || "";
      row.append(who, state);
      if (adapter.installable && adapter.detected) {
        const action = element("button", "button quiet adapter-action", installed ? "修复" : "接入");
        action.type = "button";
        action.addEventListener("click", (event) => runAdapterAction(adapter, event.currentTarget));
        row.appendChild(action);
      } else if (adapter.detected && adapter.staged) {
        const stage = element("button", "button quiet adapter-action", "片段");
        stage.type = "button";
        stage.title = "生成合并片段";
        stage.addEventListener("click", (event) => stageAdapter(adapter, event.currentTarget));
        row.appendChild(stage);
      }
      row.appendChild(tier);
      entry.appendChild(row);
      list.appendChild(entry);
    });
  els.adapterRegion.appendChild(list);
}

async function loadHistory() {
  clear(els.historyRegion);
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
    const list = element("div", "list");
    list.appendChild(emptyState("Git 历史不可用", formatError(error), "重试", loadHistory));
    els.historyRegion.appendChild(list);
    return false;
  }
}

function renderHistory() {
  clear(els.historyRegion);
  const git = state.status?.git;
  if (git?.available === false) {
    const list = element("div", "list");
    list.appendChild(emptyState("Git 不可用", "当前仓库没有可读取的 Git 历史。"));
    els.historyRegion.appendChild(list);
    return;
  }
  if (!state.history.length) {
    const list = element("div", "list");
    list.appendChild(emptyState("还没有提交", "保存记忆后会在这里出现。"));
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
      element("span", "history-subject", commitLabel(commit.subject || "无提交说明")),
      element("span", "history-date", commit.date || ""),
    );
    entry.appendChild(row);
    list.appendChild(entry);
  });
  els.historyRegion.appendChild(list);
}

const VIEW_LABELS = { memories: "全部记忆", inbox: "收件箱", adapters: "适配器", history: "历史", settings: "设置" };

function switchView(view) {
  state.view = view;
  document.querySelectorAll("[data-view]").forEach((tab) => {
    const active = tab.dataset.view === view;
    tab.classList.toggle("on", active);
    if (active) tab.setAttribute("aria-current", "page");
    else tab.removeAttribute("aria-current");
  });
  document.querySelectorAll(".view-panel").forEach((panel) => {
    panel.hidden = panel.id !== `view-${view}`;
  });
  updateBreadcrumb();
  positionSegThumb();
  const viewport = document.querySelector(".view");
  if (viewport) viewport.scrollTop = 0;
  $("all-memories")?.classList.toggle("is-active", view === "memories" && !state.project);
  document.querySelectorAll(".project-row").forEach((row) => {
    row.classList.toggle("is-active", view === "memories" && Boolean(state.project) && row.dataset.project === state.project);
  });
  requestAnimationFrame(positionSideHighlight);
  window.setTimeout(positionSideHighlight, 60);
  const newButton = $("new-memory-top");
  if (newButton) newButton.hidden = view !== "memories";
  if (view === "inbox") loadInbox();
  if (view === "adapters" && !state.adapters.length) loadAdapters();
  if (view === "history") loadHistory();
  if (view === "settings") loadSettings();
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
  }, 260);
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
  } else if (state.view === "settings") {
    await loadStatus();
    await loadSettings();
    refreshed = true;
  } else {
    refreshed = Boolean(await loadStatus());
  }
  button.disabled = false;
  if (refreshed) toast("已刷新");
  else toast("刷新失败，请确认本地 hub 正在运行", "error");
}

function openSearch(focus = true) {
  els.searchShell.classList.add("is-open");
  els.searchToggle.setAttribute("aria-expanded", "true");
  els.search.tabIndex = 0;
  if (focus) {
    requestAnimationFrame(() => {
      els.search.focus();
      els.search.select();
    });
  }
}

function closeSearch() {
  if (els.search.value.trim()) return;
  els.searchShell.classList.remove("is-open");
  els.searchToggle.setAttribute("aria-expanded", "false");
  els.search.tabIndex = -1;
}

function toggleSearch() {
  if (els.searchShell.classList.contains("is-open")) {
    if (els.search.value.trim()) els.search.focus();
    else closeSearch();
    return;
  }
  openSearch();
}

function focusSearch() {
  openSearch();
}

function bindEvents() {
  els.search.addEventListener("input", () => {
    const query = els.search.value.trim();
    state.query = query;
    state.selectedPath = "";
    els.searchClear.hidden = !query;
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
    els.search.focus();
    loadMemories({ preserveSelection: false });
  });

  els.searchToggle.setAttribute("aria-expanded", "false");
  els.searchToggle.addEventListener("click", toggleSearch);
  els.search.addEventListener("blur", () => {
    window.setTimeout(() => {
      if (!els.search.value.trim()) closeSearch();
    }, 140);
  });
  els.search.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    if (els.search.value.trim()) {
      event.preventDefault();
      els.search.value = "";
      state.query = "";
      state.selectedPath = "";
      els.searchClear.hidden = true;
      loadMemories({ preserveSelection: false });
      return;
    }
    els.search.blur();
    closeSearch();
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
  $("refresh-view").addEventListener("click", refreshCurrent);
  document.querySelectorAll("[data-theme-choice]").forEach((button) => {
    button.addEventListener("click", () => setTheme(button.dataset.themeChoice));
  });
  $("copy-root").addEventListener("click", () => copyText($("settings-root").textContent, "根目录"));
  $("copy-token").addEventListener("click", () => copyText($("settings-token").textContent, "Token"));
  $("reindex").addEventListener("click", rebuildIndex);
  $("backup-now").addEventListener("click", backupNow);
  $("save-aliases").addEventListener("click", saveAliases);
  document.querySelectorAll("[data-import]").forEach((button) => {
    button.addEventListener("click", () => importSource(button.dataset.import, button));
  });
  $("retry-hub").addEventListener("click", retryAll);
  $("close-dialog").addEventListener("click", closeDialog);
  $("cancel-dialog").addEventListener("click", closeDialog);
  els.form.addEventListener("submit", submitMemory);
  els.modal.addEventListener("click", (event) => {
    if (event.target === els.modal) closeDialog();
  });
  els.modal.addEventListener("keydown", (event) => {
    if (event.key !== "Tab") return;
    const focusables = [...els.modal.querySelectorAll(
      'button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), a[href]',
    )].filter((node) => node.offsetParent !== null);
    if (!focusables.length) return;
    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  });
  document.querySelectorAll("#side-toggle, #side-toggle-top").forEach((button) => {
    button.addEventListener("click", () => {
      const collapsed = document.body.classList.toggle("side-collapsed");
      try {
        localStorage.setItem("marktoast-sidebar", collapsed ? "collapsed" : "open");
      } catch (error) {}
      const topToggle = document.getElementById("side-toggle-top");
      if (topToggle) topToggle.hidden = !collapsed;
      window.setTimeout(positionSideHighlight, 0);
      button.setAttribute("aria-label", collapsed ? "展开侧栏" : "收起侧栏");
      button.title = collapsed ? "展开侧栏" : "收起侧栏";
    });
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


function copyText(value, label) {
  if (!value || value === "—") return;
  navigator.clipboard.writeText(value).then(
    () => toast(`${label}已复制`),
    () => toast("复制失败", "error"),
  );
}

async function loadSettings() {
  try {
    const data = await api("/api/settings");
    state.settings = data;
    state.aliases = data.aliases || state.aliases;
    renderSettings();
  } catch (error) {
    toast(formatError(error), "error");
  }
}

function renderSettings() {
  const settings = state.settings;
  if (!settings) return;
  $("settings-root").textContent = settings.root || "—";
  $("settings-root").title = settings.root || "";
  $("settings-facts").textContent = `${settings.facts || 0} 条`;
  const idx = settings.index || {};
  $("settings-index").textContent = idx.builtAt
    ? `${idx.facts || 0} 条 · ${formatDate(idx.builtAt, true)}`
    : "未建立";
  $("settings-token").textContent = settings.token || "—";
  renderAliasList(settings);
  applyTheme();
}

function renderAliasList(settings) {
  const list = $("alias-list");
  clear(list);
  const projects = state.status?.projects || [];
  if (!projects.length) {
    list.appendChild(element("p", "muted", "还没有项目"));
    return;
  }
  const aliases = settings?.aliases || state.aliases || {};
  projects.forEach((project) => {
    const row = element("div", "alias-row");
    row.dataset.key = project.key;
    row.appendChild(element("code", "alias-key", project.key));
    const input = element("input", "");
    input.type = "text";
    input.maxLength = 80;
    input.value = aliases[project.key] || "";
    input.placeholder = projectLabel(project.key);
    input.setAttribute("aria-label", `项目 ${project.key} 的显示名`);
    row.appendChild(input);
    list.appendChild(row);
  });
}

async function saveAliases() {
  const aliases = {};
  document.querySelectorAll(".alias-row").forEach((row) => {
    const value = row.querySelector("input")?.value.trim();
    if (value) aliases[row.dataset.key] = value;
  });
  try {
    const data = await api("/api/aliases", {
      method: "PUT",
      body: JSON.stringify({ aliases }),
    });
    state.aliases = data.aliases || {};
    if (state.settings) state.settings.aliases = state.aliases;
    renderAliasList(state.settings);
    renderProjects(state.status?.projects || []);
    renderProjectOptions(state.status?.projects || []);
    updateHeading();
    if (state.view === "memories") renderMemories();
    toast("已保存");
  } catch (error) {
    toast(formatError(error), "error");
  }
}

async function importSource(from, button) {
  const label = button.textContent;
  button.disabled = true;
  button.textContent = "导入中…";
  try {
    const data = await api("/api/import", {
      method: "POST",
      body: JSON.stringify({ from }),
    });
    const fresh = data.facts || 0;
    const skipped = data.skipped || 0;
    toast(fresh ? `已导入 ${fresh} 条` : skipped ? `没有新记忆（跳过 ${skipped} 条）` : "没有可导入的记忆");
    await loadStatus();
    if (state.view === "memories") await loadMemories({ preserveSelection: true });
    await loadSettings();
  } catch (error) {
    toast(formatError(error), "error");
    if (button.isConnected) {
      button.disabled = false;
      button.textContent = label;
    }
  }
}

async function backupNow() {
  const button = $("backup-now");
  button.disabled = true;
  try {
    const data = await api("/api/backup", { method: "POST" });
    $("settings-backup").textContent = data.path || "—";
    $("settings-backup").title = data.path || "";
    toast("备份已创建");
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
    button.disabled = false;
  }
}

async function rebuildIndex() {
  const button = $("reindex");
  button.disabled = true;
  try {
    const data = await api("/api/reindex", { method: "POST" });
    toast(`索引已重建 · ${data.facts || 0} 条`);
    await loadSettings();
    await loadStatus();
    if (state.view === "memories") loadMemories();
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
    button.disabled = false;
  }
}

function renderMemoryHistory(path) {
  const history = state.memoryHistory;
  const box = element("div", "memory-history");
  box.appendChild(element("h3", "", "历史"));
  if (history.loading) {
    const loading = element("div", "detail-loading");
    loading.append(element("span", "skeleton"), element("span", "skeleton"));
    box.appendChild(loading);
    return box;
  }
  if (history.error) {
    const error = element("div", "expanded-error");
    error.append(
      icon("warning"),
      element("span", "", formatError(history.error)),
      (() => {
        const retry = element("button", "text-button", "重试");
        retry.type = "button";
        retry.addEventListener("click", () => toggleMemoryHistory(path, true));
        return retry;
      })(),
    );
    box.appendChild(error);
    return box;
  }
  if (!history.commits.length) {
    box.appendChild(element("p", "muted", "还没有提交记录"));
    return box;
  }
  const list = element("div", "memory-commits");
  history.commits.forEach((commit) => {
    const row = element("div", `memory-commit${commit.hash === history.selectedHash ? " on" : ""}`);
    row.append(
      element("code", "", String(commit.hash || "").slice(0, 8)),
      element("span", "", commitLabel(commit.subject || "")),
      element("time", "", commit.date || ""),
    );
    row.addEventListener("click", () => loadMemoryDiff(path, commit.hash));
    list.appendChild(row);
  });
  box.appendChild(list);
  if (history.patchLoading) {
    box.appendChild(element("div", "detail-loading"));
  } else if (history.patchError) {
    box.appendChild(element("p", "muted", formatError(history.patchError)));
  } else if (history.patch) {
    const actions = element("div", "diff-actions");
    const restore = element("button", "button quiet", history.restoring ? "恢复中…" : "恢复此版本");
    restore.type = "button";
    restore.disabled = history.restoring;
    restore.addEventListener("click", () => restoreMemoryVersion(path, history.selectedHash));
    actions.appendChild(restore);
    box.appendChild(actions);
    const diff = element("div", "diff-box");
    diff.appendChild(element("pre", "", history.patch));
    box.appendChild(diff);
  }
  return box;
}

async function toggleMemoryHistory(path, force = false) {
  const history = state.memoryHistory;
  if (!force && history.open && history.path === path && !history.loading) {
    history.open = false;
    renderMemories();
    return;
  }
  Object.assign(history, {
    path,
    open: true,
    loading: true,
    commits: [],
    error: null,
    selectedHash: "",
    patch: "",
    patchError: null,
    patchLoading: false,
    restoring: false,
  });
  renderMemories();
  try {
    const data = await api(`/api/memory/history?path=${encodeURIComponent(path)}`);
    if (history.path !== path) return;
    history.commits = data.commits || [];
    history.loading = false;
  } catch (error) {
    if (history.path !== path) return;
    history.loading = false;
    history.error = error;
  }
  renderMemories();
}

async function loadMemoryDiff(path, hash) {
  const history = state.memoryHistory;
  Object.assign(history, { selectedHash: hash, patch: "", patchError: null, patchLoading: true });
  renderMemories();
  try {
    const data = await api(`/api/memory/diff?path=${encodeURIComponent(path)}&hash=${encodeURIComponent(hash)}`);
    if (history.selectedHash !== hash) return;
    history.patch = data.patch || "";
  } catch (error) {
    if (history.selectedHash !== hash) return;
    history.patchError = error;
  }
  history.patchLoading = false;
  renderMemories();
}

async function restoreMemoryVersion(path, hash) {
  if (!hash) return;
  if (!window.confirm(tr("恢复这个版本？会生成一次新的提交。"))) return;
  const history = state.memoryHistory;
  history.restoring = true;
  renderMemories();
  try {
    await api("/api/memory/restore", {
      method: "POST",
      body: JSON.stringify({ path, hash }),
    });
    state.details.delete(path);
    toast("已恢复");
    await selectMemory(path, true);
    const data = await api(`/api/memory/history?path=${encodeURIComponent(path)}`);
    history.commits = data.commits || [];
    history.selectedHash = "";
    history.patch = "";
    history.patchError = null;
    await loadStatus();
    await loadMemories({ preserveSelection: true });
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
    history.restoring = false;
    renderMemories();
  }
}

async function init() {
  bindEvents();
  renderMemoryLoading();
  applyTheme();
  const status = await loadStatus();
  if (status) await loadMemories();
  else renderMemoryError(new Error("无法连接本地 hub。请运行 `marktoast serve` 后重试。"));
  window.appReady = true;
}

init();
