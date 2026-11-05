// MarkToast web UI — dependency-free and served directly by the local hub.

"use strict";

const $ = (id) => document.getElementById(id);
const tr = (text) => (window.MarkToastI18n ? window.MarkToastI18n.t(text) : text);
// UI strings that leave the DOM (window.confirm) or are mixed into larger
// dynamic strings need the current language directly; plain DOM text is
// translated by the i18n MutationObserver instead.
const localized = (text) => (window.MarkToastI18n?.language === "en" ? window.MarkToastI18n.t(text) : text);

const API_TIMEOUT_MS = 15_000;
const MAX_RENDERED_BODY_CHARS = 200_000;
const MEMORY_PAGE_SIZE = 50;
const SEARCH_PAGE_SIZE = 20;
const MAX_DETAIL_CACHE_ENTRIES = 80;

const state = {
  status: null,
  root: "",
  view: "memories",
  project: "",
  type: "",
  query: "",
  sort: "relevance",
  results: [],
  total: 0,
  totalKnown: false,
  hasMore: false,
  offset: 0,
  limit: MEMORY_PAGE_SIZE,
  loadingMore: false,
  selectedPath: "",
  details: new Map(),
  detailErrors: new Map(),
  detailLoading: false,
  adapters: [],
  adapterInfo: null,
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
  searchGeneration: 0,
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
  detailModal: $("memory-detail-modal"),
  adapterModal: $("adapter-modal"),
  adapterDialogTitle: $("adapter-dialog-title"),
  adapterDialogBody: $("adapter-dialog-body"),
  adapterDialogAction: $("adapter-dialog-action"),
  detailContent: $("detail-content"),
  detailTitle: $("detail-title"),
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
    const active = button.dataset.themeChoice === preference;
    button.classList.toggle("on", active);
    button.setAttribute("aria-pressed", String(active));
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
  requestAnimationFrame(positionSetThumbs);
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
  const {
    headers: callerHeaders,
    signal: callerSignal,
    timeoutMs: requestedTimeout,
    ...requestOptions
  } = options;
  const timeoutMs = Number.isFinite(requestedTimeout) && requestedTimeout > 0 ? requestedTimeout : API_TIMEOUT_MS;
  const headers = new Headers(callerHeaders || {});
  if (!headers.has("Accept")) headers.set("Accept", "application/json");
  if (requestOptions.body !== undefined && requestOptions.body !== null && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const controller = new AbortController();
  let timedOut = false;
  const abortFromCaller = () => {
    if (!controller.signal.aborted) controller.abort(callerSignal?.reason);
  };
  if (callerSignal) {
    if (callerSignal.aborted) abortFromCaller();
    else callerSignal.addEventListener("abort", abortFromCaller, { once: true });
  }
  const timeoutId = window.setTimeout(() => {
    timedOut = true;
    if (!controller.signal.aborted) controller.abort();
  }, timeoutMs);

  const makeError = (message, name, status) => {
    const error = new Error(message);
    error.name = name;
    if (status) error.status = status;
    return error;
  };

  try {
    let response;
    try {
      response = await fetch(path, {
        ...requestOptions,
        headers,
        signal: controller.signal,
      });
    } catch (error) {
      if (timedOut) throw makeError("请求超时，请稍后重试。", "TimeoutError");
      if (callerSignal?.aborted || error?.name === "AbortError") {
        throw makeError("请求已取消。", "AbortError");
      }
      throw error;
    }

    if (!response.ok) {
      let detail = "";
      try {
        if (typeof response.text === "function") {
          const text = await response.text();
          if (text.trim()) {
            try {
              const payload = JSON.parse(text);
              detail = String(payload?.error || payload?.message || "");
            } catch {
              detail = text.trim();
            }
          }
        } else if (typeof response.json === "function") {
          const payload = await response.json();
          detail = String(payload?.error || payload?.message || "");
        }
      } catch {
        // The hub sometimes returns an empty or unreadable error body.
      }
      let message = detail || `HTTP ${response.status} ${response.statusText}`.trim();
      if (response.status === 401) message = "未授权，请重新连接本地 hub。";
      else if (response.status === 403) message = "没有权限执行此操作。";
      else if (response.status >= 500 && response.status <= 599) message = "本地 hub 暂时不可用，请稍后重试。";
      const error = makeError(message, "ApiError", response.status);
      error.detail = detail;
      throw error;
    }

    if (response.status === 204 || response.status === 205) return null;
    if (typeof response.text !== "function") {
      if (typeof response.json === "function") {
        try {
          return await response.json();
        } catch {
          throw makeError("本地 hub 返回了无法解析的响应。", "ParseError", response.status);
        }
      }
      return null;
    }
    let text;
    try {
      text = await response.text();
    } catch (error) {
      if (timedOut) throw makeError("请求超时，请稍后重试。", "TimeoutError");
      if (callerSignal?.aborted || error?.name === "AbortError") {
        throw makeError("请求已取消。", "AbortError");
      }
      throw error;
    }
    if (!text.trim()) return null;
    const contentType = (response.headers?.get?.("content-type") || "").toLowerCase();
    const looksLikeJson = /^[\s]*[\[{]/.test(text);
    if (contentType.includes("json") || looksLikeJson) {
      try {
        return JSON.parse(text);
      } catch {
        throw makeError("本地 hub 返回了无法解析的响应。", "ParseError", response.status);
      }
    }
    return text;
  } finally {
    window.clearTimeout(timeoutId);
    if (callerSignal) callerSignal.removeEventListener("abort", abortFromCaller);
  }
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

function toastDataResult(result, successText) {
  const warning = String(result?.commitWarning || "").trim();
  if (warning) {
    toast(`${successText}，但 Git 提交失败：${warning}`, "error");
    return;
  }
  toast(successText);
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
      button.dataset.noI18n = "true";
      button.title = project.key;
      button.setAttribute("aria-pressed", String(state.project === project.key));
      const projectName = element("span", "side-name", projectLabel(project.key));
      projectName.dataset.noI18n = "true";
      button.append(
        icon("folder"),
        projectName,
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
  const projectOptions = $("memory-project-options");
  if (projectOptions) {
    clear(projectOptions);
    const globalOption = document.createElement("option");
    globalOption.value = "global";
    globalOption.textContent = "global";
    projectOptions.appendChild(globalOption);
  }
  projects.forEach((project) => {
    const option = element("option", "", projectLabel(project.key));
    option.value = project.key;
    option.dataset.noI18n = "true";
    els.projectFilter.appendChild(option);
    if (projectOptions) {
      const suggestion = document.createElement("option");
      suggestion.value = project.key;
      suggestion.textContent = projectLabel(project.key);
      projectOptions.appendChild(suggestion);
    }
  });
  els.projectFilter.value = projects.some((project) => project.key === current) ? current : "";
}

function selectProject(project) {
  cancelPendingSearch();
  state.project = project;
  state.query = "";
  els.search.value = "";
  els.searchClear.hidden = true;
  els.projectFilter.value = project;
  renderProjects(state.status?.projects || []);
  $("all-memories").classList.toggle("is-active", !project);
  switchView("memories", { autoLoadMemories: false });
  loadMemories({ preserveSelection: true });
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

function syncSidebarControls() {
  const collapsed = document.body.classList.contains("side-collapsed");
  const label = collapsed ? "展开侧栏" : "收起侧栏";
  const topToggle = document.getElementById("side-toggle-top");
  if (topToggle) topToggle.hidden = !collapsed;
  document.querySelectorAll("#side-toggle, #side-toggle-top").forEach((button) => {
    button.setAttribute("aria-label", label);
    button.setAttribute("title", label);
    button.setAttribute("aria-expanded", String(!collapsed));
  });
  document.querySelectorAll("#side-toggle use, #side-toggle-top use").forEach((use) => {
    use.setAttribute("href", collapsed ? "#icon-sidebar-open" : "#icon-sidebar");
  });
}

function updateBreadcrumb() {
  const crumb = document.getElementById("breadcrumb-current");
  if (!crumb) return;
  const userProject = state.view === "memories" && !state.query && Boolean(state.project);
  if (userProject) crumb.dataset.noI18n = "true";
  else delete crumb.dataset.noI18n;
  if (state.view !== "memories") crumb.textContent = VIEW_LABELS[state.view] || "全部记忆";
  else if (state.query) crumb.textContent = "搜索结果";
  else if (state.project) crumb.textContent = projectLabel(state.project);
  else crumb.textContent = "全部记忆";
}

function updateHeading() {
  const effectiveSort = !state.query && state.sort === "relevance" ? "updated" : state.sort;
  $("sort-label").textContent = effectiveSort === "updated" ? "最近更新" : effectiveSort === "title" ? "标题" : "相关度";
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

function currentPageLimit() {
  return state.query ? SEARCH_PAGE_SIZE : MEMORY_PAGE_SIZE;
}

function currentApiSort() {
  if (!state.query) return state.sort === "title" ? "title" : "updated";
  return state.sort === "title" || state.sort === "updated" ? state.sort : "relevance";
}

function mergePageResults(existing, incoming) {
  const seen = new Set(existing.map((item) => item.path).filter(Boolean));
  const merged = existing.slice();
  incoming.forEach((item) => {
    const key = item.path;
    if (key && seen.has(key)) return;
    if (key) seen.add(key);
    merged.push(item);
  });
  return merged;
}

async function loadMemories({ preserveSelection = true, silent = false, append = false } = {}) {
  const requestId = ++state.requestId;
  if (state.searchController) state.searchController.abort();
  state.searchController = new AbortController();
  updateHeading();
  const limit = currentPageLimit();
  const offset = append ? state.offset : 0;
  if (append) {
    state.loadingMore = true;
    renderMemories();
  } else {
    state.loadingMore = false;
    state.offset = 0;
    state.hasMore = false;
    if (!silent) renderMemoryLoading();
  }
  els.resultRegion.setAttribute("aria-busy", "true");

  const params = new URLSearchParams();
  params.set("limit", String(limit));
  params.set("offset", String(offset));
  params.set("sort", currentApiSort());
  let endpoint;
  if (state.query) {
    params.set("q", state.query);
    if (state.project) params.set("project", state.project);
    if (state.type) params.set("type", state.type);
    endpoint = `/api/search?${params.toString()}`;
  } else {
    if (state.project) params.set("project", state.project);
    if (state.type) params.set("type", state.type);
    endpoint = `/api/memories?${params.toString()}`;
  }

  try {
    const data = await api(endpoint, { signal: state.searchController.signal });
    if (requestId !== state.requestId) return;
    const hasPaginationMeta = Boolean(
      data && typeof data === "object" &&
      ["total", "offset", "limit"].some((key) => Object.prototype.hasOwnProperty.call(data, key)),
    );
    let pageResults = Array.isArray(data?.results)
      ? data.results
      : Array.isArray(data?.memories)
        ? data.memories
        : [];
    if (!hasPaginationMeta && state.type) {
      pageResults = pageResults.filter((item) => String(item.type || "").toLowerCase() === state.type);
    }
    if (!hasPaginationMeta) pageResults = sortResults(pageResults);

    const totalNumber = Number(data?.total);
    const totalKnown = Boolean(
      data && data.total !== undefined && data.total !== null && Number.isFinite(totalNumber) && totalNumber >= 0,
    );
    const responseOffsetRaw = Number(data?.offset);
    const responseOffset = Number.isFinite(responseOffsetRaw) ? responseOffsetRaw : offset;
    const receivedCount = pageResults.length;
    const previousCount = state.results.length;
    const merged = append ? mergePageResults(state.results, pageResults) : pageResults;
    state.results = merged;
    state.totalKnown = totalKnown;
    state.total = totalKnown ? totalNumber : merged.length;
    state.limit = limit;
    state.offset = responseOffset + receivedCount;
    state.hasMore = totalKnown ? state.offset < totalNumber : receivedCount >= limit;
    if (append && (receivedCount === 0 || merged.length === previousCount)) state.hasMore = false;

    const selectedCanStay = Boolean(
      state.selectedPath && state.results.some((item) => item.path === state.selectedPath),
    );
    if (!preserveSelection || !selectedCanStay) {
      state.selectedPath = "";
      state.detailLoading = false;
      hideMemoryDetail();
    }
    state.loadingMore = false;
    renderMemories();
  } catch (error) {
    if (error.name === "AbortError" || requestId !== state.requestId) return;
    state.loadingMore = false;
    if (append) {
      toast(formatError(error), "error");
      renderMemories();
    } else {
      state.results = [];
      state.total = 0;
      state.totalKnown = false;
      state.hasMore = false;
      state.offset = 0;
      renderMemoryError(error);
    }
  } finally {
    if (requestId === state.requestId) {
      state.loadingMore = false;
      els.resultRegion.setAttribute("aria-busy", "false");
    }
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

function appendLoadMoreButton() {
  if (!state.hasMore) return;
  const wrap = element("div", "load-more-wrap");
  const button = element("button", "button quiet load-more", state.loadingMore ? "加载中…" : "加载更多");
  button.type = "button";
  button.disabled = state.loadingMore;
  button.addEventListener("click", () => {
    if (!state.loadingMore) loadMemories({ preserveSelection: true, silent: true, append: true });
  });
  wrap.appendChild(button);
  els.resultRegion.appendChild(wrap);
}

function renderMemories() {
  clear(els.resultRegion);
  els.resultTotal.textContent = state.totalKnown ? String(state.total) : String(state.results.length);
  if (!state.selectedPath) hideMemoryDetail();
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

  const grid = element("div", "memory-grid");
  state.results.forEach((result) => {
    const open = result.path === state.selectedPath;
    const type = result.type || "memory";
    const card = element("button", `memory-card type-${type}${open ? " is-open" : ""}`);
    card.type = "button";
    card.dataset.path = result.path;
    card.setAttribute("aria-expanded", String(open));
    card.setAttribute("aria-label", `${open ? "收起" : "打开"}记忆：${result.name || fileName(result.path)}`);

    const head = element("div", "card-head");
    const typePill = element("span", `pill type-pill type-${type}`, typeLabel(type));
    typePill.title = type;
    head.append(typePill, element("span", "card-time", relativeDate(result.updated)));
    card.appendChild(head);

    const title = element("div", "card-title", result.name || fileName(result.path));
    title.dataset.noI18n = "true";
    card.appendChild(title);

    const snippet = element("div", "card-snippet", result.description || cleanSnippet(result.snippet) || shortPath(result.path));
    snippet.dataset.noI18n = "true";
    card.appendChild(snippet);

    const foot = element("div", "card-foot");
    const project = element("span", "card-project", result.project ? projectLabel(result.project) : "未归类");
    if (result.project) {
      project.dataset.noI18n = "true";
      project.title = result.project;
    }
    const openIcon = element("span", "card-open");
    openIcon.appendChild(icon("chevron-right"));
    foot.append(project, openIcon);
    card.appendChild(foot);

    card.addEventListener("click", () => selectMemory(result.path));
    grid.appendChild(card);
  });
  els.resultRegion.appendChild(grid);
  appendLoadMoreButton();
  if (memoryDetailOpen()) renderMemoryDetail();
}

function isAnyModalOpen() {
  return [els.modal, els.detailModal, els.adapterModal].some((modal) => Boolean(modal && !modal.hidden));
}

function syncPageInert() {
  const open = isAnyModalOpen();
  [document.querySelector("header.top"), document.querySelector("aside.sidebar"), document.querySelector("main.view")]
    .filter(Boolean)
    .forEach((node) => {
      node.inert = open;
      if (open) node.setAttribute("inert", "");
      else node.removeAttribute("inert");
    });
  document.body.classList.toggle("modal-open", open);
}

function showModal(modal) {
  if (!modal) return;
  modal.hidden = false;
  syncPageInert();
}

function hideModal(modal) {
  if (!modal) return;
  modal.hidden = true;
  syncPageInert();
}

function memoryDetailOpen() {
  return Boolean(els.detailModal && !els.detailModal.hidden);
}

function hideMemoryDetail() {
  if (els.detailModal && !els.detailModal.hidden) hideModal(els.detailModal);
  state.detailLoading = false;
}

function renderMemoryDetail() {
  if (!els.detailModal || els.detailModal.hidden) return;
  const path = state.selectedPath;
  const result = state.results.find((item) => item.path === path) || state.details.get(path);
  if (!result) {
    hideMemoryDetail();
    return;
  }
  const detail = state.details.get(path);
  const title = (detail && detail.name) || result.name || fileName(path);
  if (els.detailTitle) {
    els.detailTitle.dataset.noI18n = "true";
    els.detailTitle.textContent = title;
    els.detailTitle.title = path || title;
  }
  clear(els.detailContent);
  els.detailContent.appendChild(renderExpandedMemory(result));
}

function openMemoryDetail() {
  if (!els.detailModal) return;
  showModal(els.detailModal);
  renderMemoryDetail();
  requestAnimationFrame(() => {
    $("close-detail")?.focus({ preventScroll: true });
  });
}

function closeMemoryDetail() {
  const path = state.selectedPath;
  hideMemoryDetail();
  state.selectedPath = "";
  renderMemories();
  if (path) {
    requestAnimationFrame(() => {
      const card = [...document.querySelectorAll(".memory-card")].find((node) => node.dataset.path === path);
      card?.focus({ preventScroll: true });
    });
  }
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

  const fullBody = String(detail.body || "");
  const visibleBody = fullBody.slice(0, MAX_RENDERED_BODY_CHARS);
  if (fullBody.length > MAX_RENDERED_BODY_CHARS) {
    box.appendChild(element(
      "p",
      "body-truncation-note",
      `正文较长，仅显示前 ${MAX_RENDERED_BODY_CHARS.toLocaleString("en-US")} 个字符。`,
    ));
  }
  const body = element("div", "markdown-body");
  renderMarkdown(body, visibleBody);
  if (!fullBody.trim()) body.appendChild(element("p", "muted", "没有正文。"));
  box.appendChild(body);
  const currentPath = detail.path || result.path;
  if (state.memoryHistory.open && state.memoryHistory.path === currentPath) {
    box.appendChild(renderMemoryHistory(currentPath));
  }
  return box;
}

function cacheMemoryDetail(path, detail) {
  state.details.delete(path);
  state.details.set(path, detail);
  while (state.details.size > MAX_DETAIL_CACHE_ENTRIES) {
    const oldest = state.details.keys().next().value;
    if (oldest === undefined) break;
    state.details.delete(oldest);
  }
}

async function selectMemory(path, force = false) {
  if (!path) return;
  if (path === state.selectedPath && !force && memoryDetailOpen()) {
    closeMemoryDetail();
    return;
  }
  state.selectedPath = path;
  state.detailErrors.delete(path);
  state.detailLoading = !state.details.has(path);
  renderMemories();
  openMemoryDetail();
  if (state.details.has(path) && !force) return;

  try {
    const detail = await api(`/api/memory?path=${encodeURIComponent(path)}`);
    cacheMemoryDetail(path, detail);
    state.detailErrors.delete(path);
    if (state.selectedPath === path && state.view === "memories") {
      state.detailLoading = false;
      renderMemories();
      renderMemoryDetail();
    }
  } catch (error) {
    if (state.selectedPath === path && state.view === "memories") {
      state.detailLoading = false;
      state.detailErrors.set(path, error);
      renderMemories();
      renderMemoryDetail();
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

// ---- Adapters -----------------------------------------------------------
// The hub reports one row per built-in descriptor (~40 harnesses). Older hub
// builds have no `mode` field, so derive the install mode from hooks.install_kind
// the same way the Go side does, and keep the three groups stable.
const ADAPTER_MODES = ["auto", "staged", "mcp_only"];
const ADAPTER_MODE_LABELS = { auto: "自动", staged: "片段", mcp_only: "仅 MCP" };
const ADAPTER_MODE_GROUP_LABELS = { auto: "自动接入", staged: "片段安装", mcp_only: "仅 MCP" };
const ADAPTER_MODE_HINTS = {
  auto: "MarkToast 直接合并这家 harness 的配置；写前备份，保留第三方条目。",
  staged: "MarkToast 生成可直接粘贴的 hooks / MCP 片段，由你手动合并。",
  mcp_only: "这家只支持 MCP：生成 MCP 片段，不安装生命周期 hooks。",
};
const ADAPTER_TIER_LABELS = { native: "原生", projection: "投影", readonly: "只读" };
const ADAPTER_INJECTION_LABELS = {
  stdout_additional_context: "stdout additionalContext",
  pre_llm_call_context: "pre_llm_call context",
  mcp_handoff: "MCP handoff",
  none: "none",
};

function adapterMode(adapter) {
  const explicit = String(adapter?.mode || adapter?.install_mode || "").trim();
  if (ADAPTER_MODES.includes(explicit)) return explicit;
  const kind = String(adapter?.hooks?.install_kind || "").trim();
  if (kind === "json_merge" || kind === "toml_merge" || kind === "json_bundle") return "auto";
  if (kind === "mcp_only" || kind === "none") return "mcp_only";
  return "staged";
}

function adapterHasExplicitMode(adapter) {
  return ADAPTER_MODES.includes(String(adapter?.mode || "").trim());
}

function adapterStagedReady(adapter) {
  return adapter?.staged_ready === true;
}

function adapterState(adapter) {
  const mode = adapterMode(adapter);
  if (mode === "auto" && adapter?.installed) {
    return { key: "installed", label: "已接入", className: " is-on" };
  }
  if (mode !== "auto" && adapterStagedReady(adapter)) {
    return { key: "staged", label: "已生成片段", className: " is-on" };
  }
  if (adapter?.detected) return { key: "detected", label: "已检测", className: "" };
  return { key: "missing", label: "未检测", className: "" };
}

function adapterCanInstall(adapter) {
  if (adapterMode(adapter) !== "auto" || !adapter?.detected) return false;
  // New hub builds make every auto-mode adapter installable; legacy builds only
  // exposed `installable` for ZCode.
  if (adapterHasExplicitMode(adapter)) return true;
  return Boolean(adapter?.installable);
}

function adapterCanStage(adapter) {
  return adapterMode(adapter) !== "auto" && Boolean(adapter?.detected);
}

async function runAdapterAction(adapter, button) {
  const title = adapter.title || adapter.id || "Adapter";
  const verb = adapter.installed ? "重新写入" : "写入";
  if (!window.confirm(localized(`${verb}这家 harness 的配置？会先备份原文件。`))) return;
  setButtonLoading(button, true);
  try {
    const data = await api("/api/adapters/action", {
      method: "POST",
      body: JSON.stringify({ id: adapter.id, action: "install" }),
      timeoutMs: 60_000,
    });
    toast(`${title} · ${localized(data?.installed ? "已接入" : "配置已更新")}`);
    await loadAdapters();
    await loadStatus();
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
    setButtonLoading(button, false);
  }
}

async function stageAdapter(adapter, button) {
  const title = adapter.title || adapter.id || "Adapter";
  setButtonLoading(button, true);
  try {
    const data = await api("/api/adapters/action", {
      method: "POST",
      body: JSON.stringify({ id: adapter.id, action: "stage" }),
      timeoutMs: 60_000,
    });
    const dir = String(data?.dir || "").trim();
    if (dir) {
      try {
        await navigator.clipboard.writeText(dir);
        toast(`${title} · ${localized("片段已生成，路径已复制")}`);
      } catch {
        toast(`${title} · ${localized("片段已生成")}：${dir}`);
      }
    } else {
      toast(`${title} · ${localized("片段已生成")}`);
    }
    await loadAdapters();
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
    setButtonLoading(button, false);
  }
}

function adapterActionButton(adapter, extraClass = "") {
  if (adapterCanInstall(adapter)) {
    const install = element(
      "button",
      `button quiet adapter-action${extraClass}`,
      adapter.installed ? "修复" : "接入",
    );
    install.type = "button";
    install.addEventListener("click", (event) => runAdapterAction(adapter, event.currentTarget));
    return install;
  }
  if (adapterCanStage(adapter)) {
    const stage = element("button", `button quiet adapter-action${extraClass}`, "生成片段");
    stage.type = "button";
    stage.title = "生成合并片段";
    stage.addEventListener("click", (event) => stageAdapter(adapter, event.currentTarget));
    return stage;
  }
  return null;
}

function adapterInfoRows(adapter) {
  const mode = adapterMode(adapter);
  const hooks = adapter.hooks || {};
  const mcp = adapter.mcp || {};
  const rows = [
    ["接入方式", `${localized(ADAPTER_MODE_LABELS[mode])} · ${localized(ADAPTER_MODE_GROUP_LABELS[mode])}`],
    ["层级", localized(ADAPTER_TIER_LABELS[adapter.tier] || adapter.tier || "—")],
  ];
  const detectedPaths = adapter.detected_path || (Array.isArray(adapter.detect) ? adapter.detect.join(", ") : "");
  if (detectedPaths) rows.push(["检测路径", detectedPaths]);
  if (Array.isArray(hooks.events) && hooks.events.length) rows.push(["Hook 事件", hooks.events.join(" · ")]);
  if (hooks.target) rows.push(["Hook 目标文件", hooks.target]);
  if (hooks.injection) rows.push(["注入方式", localized(ADAPTER_INJECTION_LABELS[hooks.injection] || hooks.injection)]);
  if (adapter.install_kind || hooks.install_kind) {
    rows.push(["安装类型", adapter.install_kind || hooks.install_kind]);
  }
  if (mcp.kind) rows.push(["MCP 方式", mcp.kind]);
  if (mcp.target) rows.push(["MCP 目标文件", mcp.target]);
  return rows;
}

function renderAdapterInfo(adapter) {
  if (!els.adapterDialogBody) return;
  const body = els.adapterDialogBody;
  clear(body);
  const mode = adapterMode(adapter);
  els.adapterDialogTitle.textContent = adapter.title || adapter.id || "Adapter";

  const intro = element("div", "adapter-info-intro");
  intro.append(
    element("span", `pill adapter-mode is-${mode.replace("_", "-")}`, ADAPTER_MODE_LABELS[mode]),
    element("p", "adapter-info-hint", ADAPTER_MODE_HINTS[mode]),
  );
  body.appendChild(intro);

  const table = element("table", "frontmatter-table adapter-info-table");
  adapterInfoRows(adapter).forEach(([key, value]) => {
    const trNode = element("tr");
    trNode.append(element("th", "", key), element("td", "", value));
    table.appendChild(trNode);
  });
  body.appendChild(table);

  if (adapter.notes) {
    const notes = element("section", "adapter-info-notes");
    notes.appendChild(element("h3", "", "说明"));
    const text = element("p", "", adapter.notes);
    text.dataset.noI18n = "true";
    notes.appendChild(text);
    body.appendChild(notes);
  }

  const stagedDir = String(adapter.staged_dir || "").trim();
  if (stagedDir) {
    const dir = element("section", "adapter-info-notes");
    dir.appendChild(element("h3", "", "片段目录"));
    const code = element("code", "", stagedDir);
    code.dataset.noI18n = "true";
    dir.appendChild(code);
    body.appendChild(dir);
  }
}

function adapterDialogActionDef(adapter) {
  if (adapterCanInstall(adapter)) return { kind: "install", label: adapter.installed ? "修复" : "接入" };
  if (adapterCanStage(adapter)) return { kind: "stage", label: "生成片段" };
  return null;
}

function syncAdapterDialogAction(adapter) {
  const action = els.adapterDialogAction;
  if (!action) return;
  const def = adapterDialogActionDef(adapter);
  if (!def) {
    action.hidden = true;
    action.removeAttribute("data-adapter-action");
    return;
  }
  action.hidden = false;
  action.textContent = def.label;
  action.dataset.adapterAction = def.kind;
}

function openAdapterInfo(adapter) {
  if (!els.adapterModal) return;
  state.adapterInfo = adapter;
  state.lastFocus = document.activeElement;
  renderAdapterInfo(adapter);
  syncAdapterDialogAction(adapter);
  els.adapterModal.classList.remove("closing");
  showModal(els.adapterModal);
  requestAnimationFrame(() => $("close-adapter-dialog")?.focus({ preventScroll: true }));
}

function closeAdapterInfo() {
  if (!els.adapterModal || els.adapterModal.hidden || els.adapterModal.classList.contains("closing")) return;
  els.adapterModal.classList.add("closing");
  const restoreFocus = state.lastFocus;
  state.lastFocus = null;
  state.adapterInfo = null;
  window.setTimeout(() => {
    if (!els.adapterModal.classList.contains("closing")) return;
    hideModal(els.adapterModal);
    els.adapterModal.classList.remove("closing");
    if (!isAnyModalOpen() && restoreFocus && typeof restoreFocus.focus === "function") {
      restoreFocus.focus();
    }
  }, 260);
}

function runAdapterDialogAction() {
  const adapter = state.adapterInfo;
  if (!adapter) return;
  const install = adapterCanInstall(adapter);
  closeAdapterInfo();
  if (install) runAdapterAction(adapter, null);
  else if (adapterCanStage(adapter)) stageAdapter(adapter, null);
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
    const result = await api("/api/inbox/promote", {
      method: "POST",
      body: JSON.stringify({ path: candidate.path }),
    });
    toastDataResult(result, "已保留");
    await loadStatus();
    await loadInbox();
  } catch (error) {
    toast(formatError(error), "error");
  }
}

async function dismissCandidate(candidate) {
  if (!window.confirm(tr("丢弃这条候选记忆？"))) return;
  try {
    const result = await api("/api/inbox/dismiss", {
      method: "POST",
      body: JSON.stringify({ path: candidate.path }),
    });
    toastDataResult(result, "已丢弃");
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

function renderAdapterRow(adapter, list) {
  const mode = adapterMode(adapter);
  const entry = element("article", "adapter-entry");
  entry.dataset.adapter = adapter.id || "";
  const row = element("div", "row adapter-row");
  row.append(icon("plug", "ic adapter-icon"));

  const who = element("div", "who");
  who.dataset.noI18n = "true";
  const nameLine = element("div", "adapter-name-line");
  nameLine.append(
    element("span", "name", adapter.title || adapter.id || "Adapter"),
    element("code", "", adapter.id || ""),
  );
  // Config targets read the same in every language and are what people look
  // for when deciding whether to install; long notes stay in the details dialog.
  const targets = [...new Set([adapter.hooks?.target, adapter.mcp?.target].filter(Boolean))];
  who.append(nameLine, element("div", "sub", targets.join("  ·  ") || adapter.detected_path || ""));
  row.appendChild(who);

  const st = adapterState(adapter);
  const stateNode = element("span", `adapter-state${st.className}`, st.label);
  if (adapter.detected_path) stateNode.title = adapter.detected_path;
  row.appendChild(stateNode);

  const modeBadge = element(
    "span",
    `pill adapter-mode is-${mode.replace("_", "-")}`,
    ADAPTER_MODE_LABELS[mode],
  );
  modeBadge.title = ADAPTER_MODE_HINTS[mode];
  row.appendChild(modeBadge);

  const tierLabel = ADAPTER_TIER_LABELS[adapter.tier] || adapter.tier || "";
  if (tierLabel) {
    const tier = element("span", "pill adapter-tier", tierLabel);
    tier.title = adapter.tier || "";
    row.appendChild(tier);
  }

  const actions = element("div", "adapter-actions");
  const primary = adapterActionButton(adapter);
  if (primary) actions.appendChild(primary);
  const info = element("button", "button quiet adapter-action", "查看说明");
  info.type = "button";
  info.addEventListener("click", () => openAdapterInfo(adapter));
  actions.appendChild(info);
  row.appendChild(actions);

  entry.appendChild(row);
  list.appendChild(entry);
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

  const groups = new Map(ADAPTER_MODES.map((mode) => [mode, []]));
  state.adapters
    .slice()
    .sort((a, b) => (b.detected ? 1 : 0) - (a.detected ? 1 : 0) || String(a.title || a.id).localeCompare(String(b.title || b.id), "zh-CN"))
    .forEach((adapter) => groups.get(adapterMode(adapter)).push(adapter));

  ADAPTER_MODES.forEach((mode) => {
    const adapters = groups.get(mode);
    if (!adapters.length) return;
    const group = element("section", "adapter-group");
    group.dataset.mode = mode;
    const head = element("div", "adapter-group-head");
    head.append(
      element("span", "adapter-group-name", ADAPTER_MODE_GROUP_LABELS[mode]),
      element("span", "adapter-group-count", String(adapters.length)),
    );
    group.appendChild(head);
    group.appendChild(element("p", "adapter-group-hint", ADAPTER_MODE_HINTS[mode]));
    const list = element("div", "list");
    adapters.forEach((adapter) => renderAdapterRow(adapter, list));
    group.appendChild(list);
    els.adapterRegion.appendChild(group);
  });
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

// Settings copy Magpie's preference pages: tabs at the top, one card of
// .row.pref rows under them. The tab strip and the search stay put while the
// page scrolls, and typing in the search shows each page's matching rows.
function positionSetThumb(box) {
  if (!box) return;
  const thumb = box.querySelector(":scope > .thumb");
  const active = box.querySelector(":scope > .opt.on");
  if (!thumb) return;
  if (!active || !box.offsetParent) {
    thumb.style.opacity = "0";
    return;
  }
  thumb.style.opacity = "1";
  thumb.style.width = `${active.offsetWidth}px`;
  thumb.style.height = `${active.offsetHeight}px`;
  thumb.style.transform = `translate(${active.offsetLeft - 2}px, 0)`;
}

function positionSetThumbs() {
  document.querySelectorAll("#view-settings .segs").forEach(positionSetThumb);
}

function showSettingsPage(name) {
  const tabs = [...document.querySelectorAll("#set-tabs [data-set]")];
  const known = tabs.some((tab) => tab.dataset.set === name);
  const target = known ? name : "general";
  tabs.forEach((tab) => {
    const on = tab.dataset.set === target;
    tab.classList.toggle("on", on);
    tab.setAttribute("aria-selected", String(on));
    tab.tabIndex = on ? 0 : -1;
  });
  document.querySelectorAll("#view-settings .set-page").forEach((page) => {
    page.hidden = page.id !== `setPage-${target}`;
  });
  requestAnimationFrame(positionSetThumbs);
}

function applySettingsSearch(raw) {
  const view = $("view-settings");
  if (!view) return;
  const query = String(raw || "").trim().toLowerCase();
  view.classList.toggle("set-searching", Boolean(query));
  let total = 0;
  document.querySelectorAll("#view-settings .set-page").forEach((page) => {
    let hits = 0;
    page.querySelectorAll(".row.pref").forEach((row) => {
      const match = !query || row.textContent.toLowerCase().includes(query);
      row.hidden = Boolean(query) && !match;
      if (match) hits += 1;
    });
    if (query) page.hidden = hits === 0;
    total += hits;
  });
  const none = $("set-none");
  if (none) none.hidden = !query || total > 0;
  if (!query) {
    const active = document.querySelector("#set-tabs .on")?.dataset.set || "general";
    showSettingsPage(active);
  }
}

function resetSettingsSearch() {
  const input = $("set-q");
  if (!input || !input.value) return;
  input.value = "";
  applySettingsSearch("");
}

function switchView(view, { autoLoadMemories = true } = {}) {
  cancelPendingSearch();
  state.view = view;
  document.body.classList.toggle("view-settings", view === "settings");
  if (view !== "memories") {
    state.selectedPath = "";
    hideMemoryDetail();
  }
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
  if (view === "settings") {
    loadSettings();
    requestAnimationFrame(positionSetThumbs);
  } else {
    resetSettingsSearch();
  }
  if (view === "memories" && !state.results.length && autoLoadMemories) loadMemories();
}

// animateTopNavFromSidebar mirrors Magpie's navMorph: the sidebar menu lands
// in the title bar as horizontal tabs instead of disappearing with the panel.
function animateTopNavFromSidebar(fromRect) {
  const seg = document.getElementById("main-nav");
  if (!seg || !fromRect) return;
  const to = seg.getBoundingClientRect();
  if (!to.width) return;
  positionSegThumb();
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
  const dx = fromRect.left + fromRect.width / 2 - (to.left + to.width / 2);
  const dy = fromRect.top + fromRect.height / 2 - (to.top + to.height / 2);
  const baseTransform = "translate(-50%, -50%)";
  const startTransform = `${baseTransform} translate(${dx}px, ${dy}px) scale(.35)`;
  seg.getAnimations().forEach((animation) => animation.cancel());
  seg.style.transform = startTransform;
  seg.style.opacity = ".15";
  const animation = seg.animate(
    [
      { transform: startTransform, opacity: .15 },
      { transform: baseTransform, opacity: 1 },
    ],
    { duration: 300, easing: "cubic-bezier(.2, .8, .2, 1)", fill: "forwards" },
  );
  let cleaned = false;
  const cleanup = () => {
    if (cleaned) return;
    cleaned = true;
    seg.style.transform = "";
    seg.style.opacity = "";
    animation.cancel();
  };
  animation.finished.then(cleanup).catch(cleanup);
  window.setTimeout(cleanup, 380);
}

// animateTopNavToSidebar is the reverse: the top tabs fold back down into the
// sidebar's first navigation row instead of blinking away.
function animateTopNavToSidebar(fromRect) {
  const seg = document.getElementById("main-nav");
  if (!seg || !fromRect) return;
  seg.classList.add("nav-return");
  const base = seg.getBoundingClientRect();
  const row = document.querySelector(".side-scroll .side-row");
  const sidebar = document.querySelector(".sidebar");
  if (!row || !sidebar || !base.width) {
    seg.classList.remove("nav-return");
    return;
  }
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
    seg.classList.remove("nav-return");
    return;
  }
  const rowRect = row.getBoundingClientRect();
  const sidebarRect = sidebar.getBoundingClientRect();
  const targetX = rowRect.left - sidebarRect.left + rowRect.width / 2;
  const targetY = rowRect.top + rowRect.height / 2;
  const dx = targetX - (base.left + base.width / 2);
  const dy = targetY - (base.top + base.height / 2);
  const baseTransform = "translate(-50%, -50%)";
  const endTransform = `${baseTransform} translate(${dx}px, ${dy}px) scale(.35)`;
  seg.getAnimations().forEach((animation) => animation.cancel());
  const animation = seg.animate(
    [
      { transform: baseTransform, opacity: 1 },
      { transform: endTransform, opacity: 0 },
    ],
    { duration: 260, easing: "cubic-bezier(.2, .8, .2, 1)", fill: "forwards" },
  );
  let cleaned = false;
  const cleanup = () => {
    if (cleaned) return;
    cleaned = true;
    seg.classList.remove("nav-return");
    seg.getAnimations().forEach((item) => item.cancel());
    animation.cancel();
  };
  animation.finished.then(cleanup).catch(cleanup);
  window.setTimeout(cleanup, 340);
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
  cancelPendingSearch();
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
  loadMemories({ preserveSelection: true });
}

function openDialog() {
  state.lastFocus = document.activeElement;
  clear(els.formError);
  els.formError.hidden = true;
  $("memory-project").value = state.project || "global";
  $("memory-type").value = state.type || "project";
  $("memory-name").value = "";
  $("memory-description").value = "";
  $("memory-body").value = "";
  els.modal.classList.remove("closing");
  showModal(els.modal);
  window.setTimeout(() => $("memory-project").focus(), 30);
}

function closeDialog() {
  if (els.modal.hidden || els.modal.classList.contains("closing")) return;
  els.modal.classList.add("closing");
  const restoreFocus = state.lastFocus;
  state.lastFocus = null;
  window.setTimeout(() => {
    if (!els.modal.classList.contains("closing")) return;
    hideModal(els.modal);
    els.modal.classList.remove("closing");
    if (!isAnyModalOpen() && restoreFocus && typeof restoreFocus.focus === "function") {
      restoreFocus.focus();
    }
  }, 260);
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
    toastDataResult(result, "记忆已保存");
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
  cancelPendingSearch();
  const selectedPath = state.selectedPath;
  state.details.clear();
  state.detailErrors.clear();
  state.detailLoading = false;
  try {
    if (state.view === "memories") {
      const status = await loadStatus();
      if (status) {
        await loadMemories({ preserveSelection: true });
        if (selectedPath && state.selectedPath === selectedPath) {
          await selectMemory(selectedPath, true);
        }
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
  } finally {
    button.disabled = false;
  }
  if (refreshed) toast("已刷新");
  else toast("刷新失败，请确认本地 hub 正在运行", "error");
}

function cancelPendingSearch() {
  if (state.searchTimer) {
    window.clearTimeout(state.searchTimer);
    state.searchTimer = 0;
  }
  state.searchGeneration += 1;
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
    const generation = ++state.searchGeneration;
    if (state.searchTimer) window.clearTimeout(state.searchTimer);
    state.searchTimer = window.setTimeout(() => {
      state.searchTimer = 0;
      if (generation !== state.searchGeneration) return;
      if (state.view !== "memories" && document.activeElement !== els.search) return;
      if (state.view !== "memories") switchView("memories", { autoLoadMemories: false });
      loadMemories({ preserveSelection: false });
    }, 240);
  });

  els.searchClear.addEventListener("click", () => {
    cancelPendingSearch();
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
      cancelPendingSearch();
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
      if (isAnyModalOpen()) return;
      event.preventDefault();
      focusSearch();
    }
    if (event.key === "Escape") {
      if (!els.modal.hidden) {
        event.preventDefault();
        closeDialog();
        return;
      }
      if (els.adapterModal && !els.adapterModal.hidden) {
        event.preventDefault();
        closeAdapterInfo();
        return;
      }
      if (memoryDetailOpen()) {
        event.preventDefault();
        closeMemoryDetail();
      }
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
    cancelPendingSearch();
    state.project = els.projectFilter.value;
    renderProjects(state.status?.projects || []);
    $("all-memories").classList.toggle("is-active", !state.project);
    loadMemories({ preserveSelection: true });
  });
  els.typeFilter.addEventListener("change", () => {
    cancelPendingSearch();
    state.type = els.typeFilter.value;
    loadMemories({ preserveSelection: true });
  });

  $("sort-button").addEventListener("click", () => {
    cancelPendingSearch();
    if (state.query) {
      state.sort = state.sort === "relevance" ? "updated" : state.sort === "updated" ? "title" : "relevance";
    } else {
      state.sort = state.sort === "title" ? "updated" : "title";
    }
    updateHeading();
    loadMemories({ preserveSelection: true });
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
  const settingsTabs = [...document.querySelectorAll("#set-tabs [data-set]")];
  settingsTabs.forEach((tab) => {
    tab.addEventListener("click", () => showSettingsPage(tab.dataset.set));
  });
  $("set-tabs").addEventListener("keydown", (event) => {
    const currentIndex = settingsTabs.indexOf(document.activeElement);
    if (currentIndex < 0) return;
    let nextIndex = currentIndex;
    if (event.key === "ArrowRight") nextIndex = (currentIndex + 1) % settingsTabs.length;
    else if (event.key === "ArrowLeft") nextIndex = (currentIndex - 1 + settingsTabs.length) % settingsTabs.length;
    else if (event.key === "Home") nextIndex = 0;
    else if (event.key === "End") nextIndex = settingsTabs.length - 1;
    else return;
    event.preventDefault();
    const next = settingsTabs[nextIndex];
    showSettingsPage(next.dataset.set);
    next.focus();
  });
  $("set-q").addEventListener("input", () => applySettingsSearch($("set-q").value));
  $("set-q").addEventListener("search", () => applySettingsSearch($("set-q").value));
  document.querySelectorAll("[data-lang]").forEach((button) => {
    button.addEventListener("click", () => window.setTimeout(positionSetThumbs, 30));
  });
  window.addEventListener("resize", positionSetThumbs);
  const settingsViewport = document.querySelector(".view");
  if (settingsViewport) {
    settingsViewport.addEventListener("scroll", () => {
      const head = document.querySelector("#view-settings .set-head");
      if (head) head.classList.toggle("stuck", settingsViewport.scrollTop > 4);
    });
  }
  document.querySelectorAll("[data-import]").forEach((button) => {
    button.addEventListener("click", () => importSource(button.dataset.import, button));
  });
  $("retry-hub").addEventListener("click", retryAll);
  $("close-dialog").addEventListener("click", closeDialog);
  $("cancel-dialog").addEventListener("click", closeDialog);
  $("close-detail")?.addEventListener("click", closeMemoryDetail);
  els.detailModal?.addEventListener("click", (event) => {
    if (event.target === els.detailModal) closeMemoryDetail();
  });
  els.detailModal?.addEventListener("keydown", (event) => {
    if (event.key !== "Tab") return;
    const focusables = [...els.detailModal.querySelectorAll(
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
  $("close-adapter-dialog")?.addEventListener("click", closeAdapterInfo);
  $("adapter-dialog-close")?.addEventListener("click", closeAdapterInfo);
  els.adapterDialogAction?.addEventListener("click", runAdapterDialogAction);
  els.adapterModal?.addEventListener("click", (event) => {
    if (event.target === els.adapterModal) closeAdapterInfo();
  });
  els.adapterModal?.addEventListener("keydown", (event) => {
    if (event.key !== "Tab") return;
    const focusables = [...els.adapterModal.querySelectorAll(
      'button:not([disabled]), a[href]',
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
      const wasCollapsed = document.body.classList.contains("side-collapsed");
      const sideRect = wasCollapsed
        ? null
        : document.querySelector(".side-scroll")?.getBoundingClientRect();
      const segEl = document.getElementById("main-nav");
      const topRect = wasCollapsed ? segEl?.getBoundingClientRect() : null;
      if (!wasCollapsed && segEl) segEl.classList.remove("nav-return");
      if (wasCollapsed && segEl) segEl.classList.add("nav-return");
      const collapsed = document.body.classList.toggle("side-collapsed");
      try {
        localStorage.setItem("marktoast-sidebar", collapsed ? "collapsed" : "open");
      } catch (error) {}
      syncSidebarControls();
      if (collapsed) animateTopNavFromSidebar(sideRect);
      if (!collapsed && wasCollapsed) animateTopNavToSidebar(topRect);
      window.setTimeout(positionSideHighlight, 0);
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
  requestAnimationFrame(positionSetThumbs);
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
  requestAnimationFrame(positionSetThumbs);
}

function renderAliasList(settings) {
  const list = $("alias-list");
  clear(list);
  const projects = (state.status?.projects || []).slice().sort(
    (a, b) => (Number(b.facts) || 0) - (Number(a.facts) || 0) || projectLabel(a.key).localeCompare(projectLabel(b.key), "zh-CN"),
  );
  if (!projects.length) {
    list.hidden = true;
    return;
  }
  list.hidden = false;
  const aliases = settings?.aliases || state.aliases || {};
  projects.forEach((project) => {
    const row = element("div", "row pref alias-row");
    row.dataset.key = project.key;
    const who = element("div", "who");
    who.appendChild(element("div", "name alias-key", project.key));
    who.appendChild(element("div", "sub", `${Number(project.facts) || 0} 条记忆`));
    row.appendChild(who);
    const val = element("div", "val");
    const input = element("input", "set-field");
    input.type = "text";
    input.maxLength = 80;
    input.value = aliases[project.key] || "";
    input.placeholder = "显示名";
    input.autocomplete = "off";
    input.spellcheck = false;
    input.setAttribute("aria-label", `项目 ${project.key} 的显示名`);
    val.appendChild(input);
    row.appendChild(val);
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
      timeoutMs: 120_000,
    });
    const fresh = data.facts || 0;
    const skipped = data.skipped || 0;
    toast(fresh ? `已导入 ${fresh} 条` : skipped ? `没有新记忆（跳过 ${skipped} 条）` : "没有可导入的记忆");
    await loadStatus();
    if (state.view === "memories") await loadMemories({ preserveSelection: true });
    await loadSettings();
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
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
    const data = await api("/api/backup", { method: "POST", timeoutMs: 120_000 });
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
    const data = await api("/api/reindex", { method: "POST", timeoutMs: 120_000 });
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
    const row = element("button", `memory-commit${commit.hash === history.selectedHash ? " on" : ""}`);
    row.type = "button";
    row.setAttribute("aria-pressed", String(commit.hash === history.selectedHash));
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
    const result = await api("/api/memory/restore", {
      method: "POST",
      body: JSON.stringify({ path, hash }),
    });
    state.details.delete(path);
    toastDataResult(result, "已恢复");
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
  syncSidebarControls();
  syncPageInert();
  showSettingsPage(document.querySelector("#set-tabs .on")?.dataset.set || "general");
  bindEvents();
  renderMemoryLoading();
  applyTheme();
  const status = await loadStatus();
  if (status) await loadMemories();
  else renderMemoryError(new Error("无法连接本地 hub。请运行 `marktoast serve` 后重试。"));
  window.appReady = true;
}

init();
