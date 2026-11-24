const root = window.__NOVEL_DL__.root;
const defaultSources = window.__NOVEL_DL__.defaultSources || [];
const allSources = window.__NOVEL_DL__.allSources || [];
const configurableSiteKeys = ["novalpie", "esjzone", "wenku8", "qqreader"];
const defaultPageSize = window.__NOVEL_DL__.pageSize || 50;
const initialGeneralConfig = window.__NOVEL_DL__.generalConfig || {};
const initialPageSize = normalizeSearchPageSize((initialGeneralConfig && initialGeneralConfig.web_page_size) || defaultPageSize, defaultPageSize);
const versionInfo = window.__NOVEL_DL__.version || { current: "", repo: "", mirrors: [] };
const sourceLabelMap = new Map(
  allSources.map((source) => [source.key, source.display_name || source.key]),
);
const sourceCapabilitiesMap = new Map(
  allSources.map((source) => [source.key, source.capabilities || {}]),
);
const discoverySiteShortLabels = Object.freeze({
  qidian: "起点",
  qqreader: "QQ阅读",
  weread: "微信读书",
  ciweimao: "刺猬猫",
  faloo: "飞卢",
  fanqienovel: "番茄",
  shuhuangdian: "书荒殿",
  n17k: "17K",
  sfacg: "SF轻文",
  ciyuanji: "次元姬",
  kadokado: "角角者",
  alphapolis: "阿尔法",
});
let siteWarnings = window.__NOVEL_DL__.siteWarnings || [];
let siteStats = window.__NOVEL_DL__.siteStats || [];
const siteWarningPanel = document.getElementById("siteWarningPanel");
let siteWarningHideTimer = 0;
let temporaryWarningDialogTimer = 0;
const TEMPORARY_WARNING_AUTO_HIDE_MS = 8000;
const VERSION_AUTO_CHECK_KEY = "novel-dl:update_checked_at";
const VERSION_AUTO_CHECK_INTERVAL = 6 * 60 * 60 * 1000;

const warningLevelIcons = {
  danger: "⚠️",
  info: "ℹ️",
  config: "🛠️",
};

const DEFAULT_COVER_SRC = `data:image/svg+xml;charset=UTF-8,${encodeURIComponent(`
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 360 480">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%" stop-color="#f0fdf4"/>
      <stop offset="100%" stop-color="#d1fae5"/>
    </linearGradient>
    <linearGradient id="circleBg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%" stop-color="#a7f3d0"/>
      <stop offset="100%" stop-color="#34d399"/>
    </linearGradient>
  </defs>
  <rect width="360" height="480" rx="20" fill="url(#bg)"/>
  <rect x="30" y="32" width="300" height="416" rx="16" fill="#ffffff" fill-opacity="0.9" stroke="#6ee7b7" stroke-width="2" stroke-opacity="0.5"/>
  <circle cx="180" cy="164" r="58" fill="url(#circleBg)" opacity="0.8"/>
  <rect x="104" y="248" width="152" height="16" rx="8" fill="#a7f3d0"/>
  <rect x="90" y="282" width="180" height="16" rx="8" fill="#d1fae5"/>
  <text x="180" y="356" text-anchor="middle" font-size="28" font-weight="bold" font-family="Arial, sans-serif" fill="#047857">Novel DL</text>
  <text x="180" y="388" text-anchor="middle" font-size="16" font-family="Arial, sans-serif" fill="#059669">No Cover</text>
</svg>
`)}`;

const SEARCH_SOURCE_STATE_KEY = "novel-dl-search-source-state";
const sourceKeySet = new Set(allSources.map((source) => source.key).filter(Boolean));
let sourceTagSet = new Set();
const SOURCE_TAG_SUGGESTIONS = [
  "简体中文", "繁体中文", "日文", "韩文", "英文",
  "原创", "转载站", "免费", "付费",
  "网络小说", "轻小说", "经典图书", "出版物", "正版", "二次元", "同人二创",
  "男性向", "女性向", "成人向", "NSFW",
];
refreshSourceTagSet();
const initialSearchSourceState = loadSearchSourceState();

const appState = {
  activeTab: "search",
  activeSubTab: "bookshelf",
  discover: {
    section: "trending",
    activeSite: "",
    catalog: [],
    catalogLoaded: false,
    catalogLoading: false,
    catalogNotice: "",
    warnings: [],
    trendingBooks: [],
    trendingLoaded: false,
    trendingLoading: false,
    activeList: null,
    activeBooks: [],
    activePage: 1,
    activePageSize: 0,
    activeTotal: 0,
    activeHasMore: false,
    listLoading: false,
    requestToken: 0,
  },
  page: 1,
  pageSize: initialPageSize,
  lastKeyword: "",
  lastExact: false,
  lastSearchSites: [],
  lastTagSearch: "",
  lastTagSearchUsed: false,
  results: [],
  total: 0,
  totalExact: true,
  hasPrev: false,
  hasNext: false,
  selectedSites: new Set(initialSearchSourceState.sites),
  selectedSourceTags: new Set(initialSearchSourceState.tags),
  tasks: new Map(),
  tasksLoaded: false,
  tasksPage: 1,
  pollers: new Map(),
  inlineTaskViews: new Map(),
  bookshelf: {
    parentId: null,
    items: [],
    breadcrumb: [],
    loading: false,
    loaded: false,
    page: 1,
    total: 0,
    bookTotal: 0,
    pages: 1,
    pageSize: 0,
    booksByKey: new Map(),
  },
  history: {
    items: [],
    loading: false,
    loaded: false,
    page: 1,
    total: 0,
    pages: 1,
    pageSize: 0,
    countPromise: null,
  },
  sourceSpeed: {
    running: false,
    keyword: "",
    lastRunAt: 0,
    byKey: new Map(),
  },
  reader: {
    site: "",
    bookID: "",
    title: "",
    author: "",
    coverURL: "",
    description: "",
    latestChapter: "",
    sourceURL: "",
    localOnly: false,
    progressTimer: 0,
    lastReportedProgressKey: "",
  },
  detailCache: new Map(),
  detailPending: new Map(),
  detailTimings: new Map(),
  readerCatalogCache: new Map(),
  readerCatalogPending: new Map(),
  detailResult: null,
  activeDetailKey: "",
  activeDetailVariant: null,
  activeDetailPage: 1,
  activeDetailPageSize: loadChapterPageSize(),
  detailWarmupToken: 0,
  chapterPageSize: loadChapterPageSize(),
  chapterColumns: loadChapterColumns(),
  siteConfigs: new Map(),
  paramSupports: [],
  generalConfig: initialGeneralConfig,
};

let appURLSyncTimer = 0;
let appURLRestoring = false;
const APP_URL_KEYS = [
  "view", "q", "mode", "page", "sites", "tags", "section", "list",
  "tag_search",
  "site", "book_id", "chapter_id", "chapter_url", "chapter_index",
  "chapter_page", "chapter_size", "local", "title", "folder", "panel",
];

function normalizeSearchPageSize(value, fallback = defaultPageSize) {
  const parsed = Number.parseInt(String(value || ""), 10);
  if (parsed > 0) return parsed;
  const fallbackParsed = Number.parseInt(String(fallback || defaultPageSize), 10);
  return fallbackParsed > 0 ? fallbackParsed : 50;
}

function setSearchPageSize(value, fallback = appState.pageSize) {
  appState.pageSize = normalizeSearchPageSize(value, fallback || defaultPageSize);
  return appState.pageSize;
}

function refreshSourceTagSet() {
  sourceTagSet = new Set(
    allSources.flatMap((source) => (
      Array.isArray(source.tags)
        ? source.tags.map((tag) => `${tag || ""}`.trim()).filter(Boolean)
        : []
    )),
  );
  return sourceTagSet;
}

function loadSearchSourceState() {
  const fallback = {
    sites: defaultSources.map((source) => source.key).filter((key) => sourceKeySet.has(key)),
    tags: [],
  };
  try {
    const raw = window.localStorage.getItem(SEARCH_SOURCE_STATE_KEY);
    if (!raw) return fallback;
    const parsed = JSON.parse(raw);
    const sites = Array.isArray(parsed.sites) ? parsed.sites.filter((key) => sourceKeySet.has(key)) : [];
    const tags = Array.isArray(parsed.tags) ? parsed.tags.filter((tag) => sourceTagSet.has(tag)) : [];
    return { sites: sites.length ? sites : fallback.sites, tags };
  } catch (_) {
    return fallback;
  }
}

function saveSearchSourceState() {
  try {
    window.localStorage.setItem(SEARCH_SOURCE_STATE_KEY, JSON.stringify({
      sites: Array.from(appState.selectedSites),
      tags: Array.from(appState.selectedSourceTags),
    }));
  } catch (_) {
  }
  scheduleAppURLSync();
}

function renderSiteWarnings() {
  if (!siteWarningPanel) return;
  if (siteWarningHideTimer) window.clearTimeout(siteWarningHideTimer);
  const visibleWarnings = [];
  const transientWarnings = [];
  siteWarnings.forEach((warning) => {
    if (!isTransientSiteWarning(warning)) {
      visibleWarnings.push(warning);
      return;
    }
    if (hasSeenTransientSiteWarning(warning)) return false;
    markTransientSiteWarningSeen(warning);
    transientWarnings.push(warning);
  });
  if (visibleWarnings.length === 0) {
    siteWarningPanel.hidden = true;
    siteWarningPanel.innerHTML = "";
    if (transientWarnings.length) showTemporaryWarningDialog(transientWarnings);
    return;
  }
  siteWarningPanel.hidden = false;
  siteWarningPanel.innerHTML = "";
  visibleWarnings.forEach((warning) => {
    siteWarningPanel.appendChild(createSiteWarningCard(warning));
  });
  if (transientWarnings.length) showTemporaryWarningDialog(transientWarnings);
}

function createSiteWarningCard(warning) {
  const card = document.createElement("article");
  card.className = `site-warning-card site-warning-${warning.level || "info"}`;
  if (warning.kind) card.classList.add(`site-warning-${warning.kind}`);

  const icon = document.createElement("span");
  icon.className = "site-warning-icon";
  icon.textContent = warning.kind === "timeout" ? "⏱️" : (warningLevelIcons[warning.level] || "ℹ️");

  const message = document.createElement("p");
  message.className = "site-warning-message";
  message.textContent = warning.message;

  const header = document.createElement("div");
  header.className = "site-warning-head";
  header.append(icon, message);
  card.appendChild(header);

  if (warning.kind !== "timeout" && warning.kind !== "blocked") {
    const stat = siteStats.find((stat) => stat.site_key === warning.site_key);
    if (stat && stat.enabled.length) {
      const detail = document.createElement("p");
      detail.className = "site-warning-detail";
      detail.textContent = `已自动配置字段：${stat.enabled.join("、")}`;
      card.appendChild(detail);
    }
  }

  if (warning.action_label && warning.action_link) {
    const action = document.createElement("a");
    action.className = "site-warning-action";
    action.href = warning.action_link;
    if (warning.action_link === "#site-config") {
      action.addEventListener("click", (event) => {
        event.preventDefault();
        hideTemporaryWarningDialog();
        void openSiteConfig({ panel: "site", siteKey: warning.site_key || "esjzone" });
      });
    } else {
      action.target = "_blank";
      action.rel = "noopener noreferrer";
    }
    action.textContent = warning.action_label;
    card.appendChild(action);
  }
  return card;
}

function showTemporaryWarningDialog(warnings) {
  const items = warnings.filter(Boolean);
  if (!items.length) return;
  const dialog = ensureTemporaryWarningDialog();
  const list = dialog.querySelector(".temporary-warning-list");
  const title = dialog.querySelector("#temporaryWarningHeading");
  const summary = dialog.querySelector("#temporaryWarningSummary");
  const normalized = items.map((warning) => {
    const item = typeof warning === "string" ? { message: warning, level: "info" } : warning;
    return { ...item, kind: item.kind || warningKind(item) };
  });
  const hasTimeout = normalized.some((warning) => warning.kind === "timeout");
  const allAuth = normalized.every((warning) => warning.kind === "auth");
  const allBlocked = normalized.every((warning) => warning.kind === "blocked");
  dialog.classList.toggle("has-timeout", hasTimeout);
  dialog.classList.toggle("is-auth", allAuth);
  dialog.classList.toggle("is-blocked", allBlocked);
  if (title) title.textContent = temporaryWarningTitle(normalized);
  if (summary) summary.textContent = `${normalized.length} 个渠道本次已跳过 · 不影响其他渠道结果`;
  list.innerHTML = "";
  normalized.forEach((warning) => {
    list.appendChild(createTemporaryWarningItem(warning));
  });
  dialog.hidden = false;
  scheduleTemporaryWarningAutoHide();
}

function temporaryWarningTitle(warnings) {
  if (warnings.every((warning) => warning.kind === "timeout")) return "渠道响应超时";
  if (warnings.every((warning) => warning.kind === "auth")) return "渠道需要登录";
  if (warnings.every((warning) => warning.kind === "blocked")) return "渠道访问受限";
  if (warnings.some((warning) => ["timeout", "blocked", "auth"].includes(warning.kind))) return "部分渠道响应异常";
  return "临时提示";
}

function scheduleTemporaryWarningAutoHide() {
  if (temporaryWarningDialogTimer) window.clearTimeout(temporaryWarningDialogTimer);
  temporaryWarningDialogTimer = window.setTimeout(hideTemporaryWarningDialog, TEMPORARY_WARNING_AUTO_HIDE_MS);
}

function pauseTemporaryWarningAutoHide() {
  if (!temporaryWarningDialogTimer) return;
  window.clearTimeout(temporaryWarningDialogTimer);
  temporaryWarningDialogTimer = 0;
}

function resumeTemporaryWarningAutoHide() {
  const dialog = document.getElementById("temporaryWarningDialog");
  if (!dialog || dialog.hidden || dialog.matches(":hover") || dialog.contains(document.activeElement)) return;
  scheduleTemporaryWarningAutoHide();
}

function createTemporaryWarningItem(warning) {
  const kind = warning.kind || "info";
  const card = document.createElement("article");
  card.className = `temporary-warning-item is-${kind}`;

  const head = document.createElement("div");
  head.className = "temporary-warning-item-head";
  const site = document.createElement("span");
  site.className = "temporary-warning-item-site";
  site.textContent = warning.site_key ? sourceLabel(warning.site_key) : "渠道提示";
  const badge = document.createElement("span");
  badge.className = "temporary-warning-item-badge";
  badge.textContent = kind === "timeout" ? "响应超时" : kind === "blocked" ? "访问受限" : kind === "auth" ? "需要登录" : "临时跳过";
  head.append(site, badge);

  const message = document.createElement("p");
  message.className = "temporary-warning-item-message";
  message.textContent = temporaryWarningMessage(warning);
  card.append(head, message);

  if (warning.action_label && warning.action_link) {
    const action = document.createElement("a");
    action.className = "temporary-warning-item-action";
    action.href = warning.action_link;
    if (warning.action_link === "#site-config") {
      action.addEventListener("click", (event) => {
        event.preventDefault();
        hideTemporaryWarningDialog();
        void openSiteConfig({ panel: "site", siteKey: warning.site_key || "esjzone" });
      });
    } else {
      action.target = "_blank";
      action.rel = "noopener noreferrer";
    }
    action.textContent = warning.action_label;
    card.appendChild(action);
  }
  return card;
}

function temporaryWarningMessage(warning) {
  const rawText = `${(warning && warning.message) || ""} ${(warning && warning.error) || ""}`.toLowerCase();
  if (warning && warning.kind === "auth") return "需要登录或配置 Cookie 后重试。";
  if (warning && warning.kind === "timeout") return "站点在限定时间内未响应，请稍后重试。";
  if (warning && warning.kind === "blocked") return "站点触发了访问限制，请稍后重试。";
  if (rawText.includes("getaddrinfow") || rawText.includes("no such host")) return "域名暂时无法解析。";
  let text = String((warning && warning.message) || "").trim();
  text = text.replace(/^临时提示[：:]\s*/, "");
  const site = warning && warning.site_key ? sourceLabel(warning.site_key) : "";
  if (site && text.startsWith(`${site}：`)) text = text.slice(site.length + 1).trim();
  else if (site && text.startsWith(`${site} `)) text = text.slice(site.length).trim();
  text = text.replace(/^临时提示[：:]\s*/, "");
  return text || "本次请求暂时未能完成。";
}

function hideTemporaryWarningDialog() {
  const dialog = document.getElementById("temporaryWarningDialog");
  if (!dialog) return;
  dialog.hidden = true;
  if (temporaryWarningDialogTimer) {
    window.clearTimeout(temporaryWarningDialogTimer);
    temporaryWarningDialogTimer = 0;
  }
}

function ensureTemporaryWarningDialog() {
  const existing = document.getElementById("temporaryWarningDialog");
  if (existing) return existing;
  const dialog = document.createElement("div");
  dialog.id = "temporaryWarningDialog";
  dialog.className = "temporary-warning-dialog temporary-warning-toast";
  dialog.hidden = true;
  dialog.setAttribute("role", "status");
  dialog.setAttribute("aria-live", "polite");

  const modal = document.createElement("section");
  modal.className = "temporary-warning-modal";
  modal.setAttribute("aria-labelledby", "temporaryWarningHeading");

  const head = document.createElement("div");
  head.className = "temporary-warning-head";
  const titleBlock = document.createElement("div");
  titleBlock.className = "temporary-warning-title-block";
  const title = document.createElement("h2");
  title.id = "temporaryWarningHeading";
  title.textContent = "临时提示";
  const summary = document.createElement("p");
  summary.id = "temporaryWarningSummary";
  summary.className = "temporary-warning-summary";
  titleBlock.append(title, summary);
  const close = document.createElement("button");
  close.type = "button";
  close.className = "temporary-warning-close";
  close.setAttribute("aria-label", "关闭临时提示");
  close.textContent = "×";
  close.addEventListener("click", hideTemporaryWarningDialog);
  head.append(titleBlock, close);

  const list = document.createElement("div");
  list.className = "temporary-warning-list";

  modal.append(head, list);
  dialog.appendChild(modal);
  dialog.addEventListener("pointerenter", pauseTemporaryWarningAutoHide);
  dialog.addEventListener("pointerleave", resumeTemporaryWarningAutoHide);
  dialog.addEventListener("focusin", pauseTemporaryWarningAutoHide);
  dialog.addEventListener("focusout", (event) => {
    if (!dialog.contains(event.relatedTarget)) resumeTemporaryWarningAutoHide();
  });
  document.body.appendChild(dialog);
  return dialog;
}

function showToast(title, message = "", type = "info", options = {}) {
  let container = document.getElementById("app-toast-container");
  if (!container) {
    container = document.createElement("div");
    container.id = "app-toast-container";
    container.className = "app-toast-container";
    document.body.appendChild(container);
  }

  const toastType = ["success", "warning", "error", "info"].includes(type) ? type : "info";
  const toast = document.createElement("div");
  toast.className = `app-toast app-toast-${toastType}`;
  toast.setAttribute("role", toastType === "error" ? "alert" : "status");
  toast.setAttribute("aria-live", toastType === "error" ? "assertive" : "polite");

  const content = document.createElement("div");
  content.className = "app-toast-content";
  const titleNode = document.createElement("div");
  titleNode.className = "app-toast-title";
  titleNode.textContent = title;
  content.appendChild(titleNode);

  if (message) {
    const messageNode = document.createElement("div");
    messageNode.className = "app-toast-message";
    messageNode.textContent = message;
    content.appendChild(messageNode);
  }

  if (options.actionLabel && options.actionURL) {
    const action = document.createElement("a");
    action.className = "app-toast-action";
    action.href = options.actionURL;
    action.target = "_blank";
    action.rel = "noopener noreferrer";
    action.textContent = options.actionLabel;
    content.appendChild(action);
  }

  const closeButton = document.createElement("button");
  closeButton.type = "button";
  closeButton.className = "app-toast-close";
  closeButton.setAttribute("aria-label", "关闭提示");
  closeButton.textContent = "\u00d7";

  let timer = 0;
  const close = () => {
    if (timer) {
      window.clearTimeout(timer);
      timer = 0;
    }
    toast.classList.add("app-toast-hide");
    window.setTimeout(() => toast.remove(), 180);
  };
  closeButton.addEventListener("click", close);

  toast.append(content, closeButton);
  container.appendChild(toast);
  const duration = Number.isFinite(options.duration) ? options.duration : 7000;
  if (duration > 0) {
    timer = window.setTimeout(close, duration);
  }
}

function isTransientSiteWarning(warning) {
  return Boolean(warning && (warning.transient || String(warning.message || "").startsWith("临时提示")));
}

function transientSiteWarningKey(warning) {
  return `novel-dl-site-warning:${warning.site_key || ""}:${warning.message || ""}`;
}

function hasSeenTransientSiteWarning(warning) {
  try {
    return window.sessionStorage.getItem(transientSiteWarningKey(warning)) === "1";
  } catch (_) {
    return false;
  }
}

function markTransientSiteWarningSeen(warning) {
  try {
    window.sessionStorage.setItem(transientSiteWarningKey(warning), "1");
  } catch (_) {
  }
}

const keywordInput = document.getElementById("keyword");
const searchModeSelect = document.getElementById("searchMode");
const searchForm = document.getElementById("searchForm");
const statusNode = document.getElementById("status");
const warningsNode = document.getElementById("warnings");
const resultsNode = document.getElementById("results");
const resultCountNode = document.getElementById("resultCount");
const resultMetaNode = document.getElementById("resultMeta");
const pageIndicatorNode = document.getElementById("pageIndicator");
const prevPageButton = document.getElementById("prevPage");
const nextPageButton = document.getElementById("nextPage");
const paginationBottomNode = document.getElementById("paginationBottom");
const pageIndicatorBottomNode = document.getElementById("pageIndicatorBottom");
const prevPageBottomButton = document.getElementById("prevPageBottom");
const nextPageBottomButton = document.getElementById("nextPageBottom");
const taskCountNode = document.getElementById("taskCount");
const taskTabCountNode = document.getElementById("taskTabCount");
const sourceSummaryNode = document.getElementById("sourceSummary");
const sourceSelectorNode = document.getElementById("sourceSelector");
const sourceTagFiltersNode = document.getElementById("sourceTagFilters");
const clearTagFiltersButton = document.getElementById("clearTagFilters");
const tasksNode = document.getElementById("tasks");
const tasksClearFinishedButton = document.getElementById("tasksClearFinished");
const searchTabButton = document.getElementById("searchTabButton");
const discoverTabButton = document.getElementById("discoverTabButton");
const favoritesTabButton = document.getElementById("favoritesTabButton");
const bookshelfSubTab = document.getElementById("bookshelfSubTab");
const historySubTab = document.getElementById("historySubTab");
const tasksSubTab = document.getElementById("tasksSubTab");
const bookshelfSubPanel = document.getElementById("bookshelfSubPanel");
const historySubPanel = document.getElementById("historySubPanel");
const tasksSubPanel = document.getElementById("tasksSubPanel");
const bookshelfTabCountNode = document.getElementById("bookshelfTabCount");
const historyTabCountNode = document.getElementById("historyTabCount");
const searchTabPanel = document.getElementById("searchTabPanel");
const discoverTabPanel = document.getElementById("discoverTabPanel");
const favoritesTabPanel = document.getElementById("favoritesTabPanel");
const discoverSectionTabs = Array.from(document.querySelectorAll("[data-discover-section]"));
const discoverWarningsNode = document.getElementById("discoverWarnings");
const discoverTrendingPanel = document.getElementById("discoverTrendingPanel");
const discoverTrendingStatusNode = document.getElementById("discoverTrendingStatus");
const discoverTrendingGridNode = document.getElementById("discoverTrendingGrid");
const discoverTrendingCountNode = document.getElementById("discoverTrendingCount");
const discoverCatalogPanel = document.getElementById("discoverCatalogPanel");
const discoverCatalogTitleNode = document.getElementById("discoverCatalogTitle");
const discoverCatalogHintNode = document.getElementById("discoverCatalogHint");
const discoverSiteTabsNode = document.getElementById("discoverSiteTabs");
const discoverStatusNode = document.getElementById("discoverStatus");
const discoverListGridNode = document.getElementById("discoverListGrid");
const discoverListPanel = document.getElementById("discoverListPanel");
const discoverBackButton = document.getElementById("discoverBackButton");
const discoverListHeaderNode = document.getElementById("discoverListHeader");
const discoverBookStatusNode = document.getElementById("discoverBookStatus");
const discoverBookGridNode = document.getElementById("discoverBookGrid");
const discoverBookPaginationNode = document.getElementById("discoverBookPagination");
const discoverBooksPrevPageButton = document.getElementById("discoverBooksPrevPage");
const discoverBooksNextPageButton = document.getElementById("discoverBooksNextPage");
const discoverBookPageIndicatorNode = document.getElementById("discoverBookPageIndicator");
const historyListNode = document.getElementById("historyList");
const historyStatusNode = document.getElementById("historyStatus");
const historyRefreshButton = document.getElementById("historyRefresh");
const bookshelfNode = document.getElementById("bookshelf");
const bookshelfBreadcrumbNode = document.getElementById("bookshelfBreadcrumb");
const bookshelfStatusNode = document.getElementById("bookshelfStatus");
const bookshelfNewFolderButton = document.getElementById("bookshelfNewFolder");
const bookshelfPaginationNode = document.getElementById("bookshelfPagination");
const bookshelfPrevPageButton = document.getElementById("bookshelfPrevPage");
const bookshelfNextPageButton = document.getElementById("bookshelfNextPage");
const bookshelfPageIndicatorNode = document.getElementById("bookshelfPageIndicator");
const historyPaginationNode = document.getElementById("historyPagination");
const historyPrevPageButton = document.getElementById("historyPrevPage");
const historyNextPageButton = document.getElementById("historyNextPage");
const historyPageIndicatorNode = document.getElementById("historyPageIndicator");
const tasksPaginationNode = document.getElementById("tasksPagination");
const tasksPrevPageButton = document.getElementById("tasksPrevPage");
const tasksNextPageButton = document.getElementById("tasksNextPage");
const tasksPageIndicatorNode = document.getElementById("tasksPageIndicator");
const selectAllSourcesButton = document.getElementById("selectAllSources");
const clearSourcesButton = document.getElementById("clearSources");
const speedTestSourcesButton = document.getElementById("speedTestSources");
const speedTestStatusNode = document.getElementById("speedTestStatus");
const sourceFiltersNode = document.getElementById("sourceFilters");
const sourceFiltersToggleButton = document.getElementById("sourceFiltersToggle");
const sourceFiltersToggleSummaryNode = document.getElementById("sourceFiltersToggleSummary");
const sourceTagEditorOverlay = document.getElementById("sourceTagEditorOverlay");
const sourceTagEditorBackdrop = document.getElementById("sourceTagEditorBackdrop");
const sourceTagEditorCloseButton = document.getElementById("sourceTagEditorClose");
const sourceTagEditorCancelButton = document.getElementById("sourceTagEditorCancel");
const sourceTagEditorSaveButton = document.getElementById("sourceTagEditorSave");
const sourceTagEditorTitleNode = document.getElementById("sourceTagEditorTitle");
const sourceTagEditorKeyNode = document.getElementById("sourceTagEditorKey");
const sourceTagEditorCountNode = document.getElementById("sourceTagEditorCount");
const sourceTagEditorSelectedNode = document.getElementById("sourceTagEditorSelected");
const sourceTagEditorSuggestionsNode = document.getElementById("sourceTagEditorSuggestions");
const sourceTagEditorInputNode = document.getElementById("sourceTagEditorInput");
const sourceTagEditorAddButton = document.getElementById("sourceTagEditorAdd");
const sourceTagEditorStatusNode = document.getElementById("sourceTagEditorStatus");
let sourceTagEditorSiteKey = "";
let sourceTagEditorTags = [];
let sourceTagEditorPreviousFocus = null;
let sourceTagEditorSaving = false;
const detailOverlay = document.getElementById("detailOverlay");
const detailBackdrop = document.getElementById("detailBackdrop");
const detailCloseButton = document.getElementById("detailCloseButton");
const detailContentNode = document.getElementById("detailContent");

const openGeneralConfigButton = document.getElementById("openGeneralConfig");
const siteConfigOverlay = document.getElementById("siteConfigOverlay");
const siteConfigBackdrop = document.getElementById("siteConfigBackdrop");
const closeSiteConfigButton = document.getElementById("closeSiteConfig");
const siteConfigForm = document.getElementById("siteConfigForm");
const siteConfigKeyNode = document.getElementById("siteConfigKey");
const siteConfigHintNode = document.getElementById("siteConfigHint");
const siteUsernameNode = document.getElementById("siteUsername");
const sitePasswordNode = document.getElementById("sitePassword");
const toggleSitePasswordButton = document.getElementById("toggleSitePassword");
const siteCookieNode = document.getElementById("siteCookie");
const siteCookieHelpNode = document.getElementById("siteCookieHelp");
const siteMirrorHostsNode = document.getElementById("siteMirrorHosts");
const generalConfigForm = document.getElementById("generalConfigForm");
const saveSettingsConfigButton = document.getElementById("saveSettingsConfig");
const settingsSaveStatusNode = document.getElementById("settingsSaveStatus");
const settingsTabs = Array.from(document.querySelectorAll("[data-settings-tab]"));
const settingsPanels = Array.from(document.querySelectorAll("[id^='settingsPanel']"));
const settingsContentNode = document.querySelector(".settings-content");
const siteConfigStatusNode = document.getElementById("siteConfigStatus");
let settingsActivePanel = "performance";
let settingsPreviousFocus = null;

const generalWorkersNode = document.getElementById("generalWorkers");
const generalTimeoutNode = document.getElementById("generalTimeout");
const generalRequestIntervalNode = document.getElementById("generalRequestInterval");
const generalLocaleStyleNode = document.getElementById("generalLocaleStyle");
const generalFormatsNode = document.getElementById("generalFormats");
const generalFilenameTemplateNode = document.getElementById("generalFilenameTemplate");
const generalAppendTimestampNode = document.getElementById("generalAppendTimestamp");
const generalIncludePictureNode = document.getElementById("generalIncludePicture");
const generalDisableCacheNode = document.getElementById("generalDisableCache");
const generalBlurWebImagesNode = document.getElementById("generalBlurWebImages");
const generalBlurWebImagesLabelNode = generalBlurWebImagesNode
  ? generalBlurWebImagesNode.closest(".settings-toggle")?.querySelector(".settings-toggle-copy strong")
  : null;
const generalWebPageSizeNode = document.getElementById("generalWebPageSize");
const generalCLIPageSizeNode = document.getElementById("generalCLIPageSize");
const generalRawDataDirNode = document.getElementById("generalRawDataDir");
const generalCacheDirNode = document.getElementById("generalCacheDir");
const generalOutputDirNode = document.getElementById("generalOutputDir");

const versionCurrentNode = document.getElementById("versionCurrent");
const versionRepoLinkNode = document.getElementById("versionRepoLink");
const versionMirrorNode = document.getElementById("versionMirror");
const versionCheckButton = document.getElementById("versionCheckButton");
const versionResultNode = document.getElementById("versionResult");

const backToTopButton = document.getElementById("backToTop");

function bindRangeValue(inputId) {
  const input = document.getElementById(inputId);
  const valDisplay = document.getElementById(inputId + "Val");
  if (input && valDisplay) {
    input.addEventListener("input", () => { valDisplay.textContent = input.value; });
  }
}

bootstrap();

function bootstrap() {
  if (generalBlurWebImagesLabelNode) generalBlurWebImagesLabelNode.textContent = "\u7f51\u9875\u56fe\u7247\u6a21\u7cca\u5316\uff08\u4ec5\u5f71\u54cd\u9875\u9762\u663e\u793a\uff09";
  renderSourceTagFilters();
  renderSourceSelector();
  renderWarnings([]);
  renderResults([]);
  renderTasks();
  renderPaging();
  renderResultMeta();
  renderDiscover();
  setStatus("选择渠道后输入关键词开始搜索。");

  // 绑定滑动条联动显示
  ['generalWorkers', 'generalTimeout', 'generalRequestInterval', 'generalWebPageSize', 'generalCLIPageSize'].forEach(bindRangeValue);

  renderSiteWarnings();
  renderGeneralConfigForm(appState.generalConfig);
  void loadSiteConfigs();
  void loadGeneralConfig().catch((error) => {
    setStatus(`全局配置加载失败：${error.message}`);
  });

  searchTabButton.addEventListener("click", () => activateTab("search"));
  if (discoverTabButton) discoverTabButton.addEventListener("click", () => activateTab("discover"));
  if (favoritesTabButton) favoritesTabButton.addEventListener("click", () => activateTab("favorites"));
  discoverSectionTabs.forEach((tab) => {
    tab.addEventListener("click", () => activateDiscoverSection(tab.dataset.discoverSection));
  });
  if (discoverBackButton) discoverBackButton.addEventListener("click", closeDiscoverList);
  if (bookshelfSubTab) bookshelfSubTab.addEventListener("click", () => activateSubTab("bookshelf"));
  if (historySubTab) historySubTab.addEventListener("click", () => activateSubTab("history"));
  if (tasksSubTab) tasksSubTab.addEventListener("click", () => activateSubTab("tasks"));

  if (historyRefreshButton) historyRefreshButton.addEventListener("click", () => void loadHistory());

  const goBookshelfPrev = () => {
    if (appState.bookshelf.page <= 1) return;
    void loadBookshelf(appState.bookshelf.parentId, appState.bookshelf.page - 1);
  };
  const goBookshelfNext = () => {
    if (appState.bookshelf.page >= appState.bookshelf.pages) return;
    void loadBookshelf(appState.bookshelf.parentId, appState.bookshelf.page + 1);
  };
  if (bookshelfPrevPageButton) bookshelfPrevPageButton.addEventListener("click", goBookshelfPrev);
  if (bookshelfNextPageButton) bookshelfNextPageButton.addEventListener("click", goBookshelfNext);

  const goHistoryPrev = () => {
    if (appState.history.page <= 1) return;
    void loadHistory(appState.history.page - 1);
  };
  const goHistoryNext = () => {
    if (appState.history.page >= appState.history.pages) return;
    void loadHistory(appState.history.page + 1);
  };
  if (historyPrevPageButton) historyPrevPageButton.addEventListener("click", goHistoryPrev);
  if (historyNextPageButton) historyNextPageButton.addEventListener("click", goHistoryNext);

  const goDiscoverBooksPrev = () => {
    const discover = appState.discover;
    if (!discover.activeList || discover.activePage <= 1 || discover.listLoading) return;
    void openDiscoverList(discover.activeList.site, discover.activeList, discover.activePage - 1);
  };
  const goDiscoverBooksNext = () => {
    const discover = appState.discover;
    if (!discover.activeList || !discover.activeHasMore || discover.listLoading) return;
    void openDiscoverList(discover.activeList.site, discover.activeList, discover.activePage + 1);
  };
  if (discoverBooksPrevPageButton) discoverBooksPrevPageButton.addEventListener("click", goDiscoverBooksPrev);
  if (discoverBooksNextPageButton) discoverBooksNextPageButton.addEventListener("click", goDiscoverBooksNext);

  const goTasksPrev = () => { if (appState.tasksPage > 1) { appState.tasksPage -= 1; renderTasks(); } };
  const goTasksNext = () => { appState.tasksPage += 1; renderTasks(); };
  if (tasksPrevPageButton) tasksPrevPageButton.addEventListener("click", goTasksPrev);
  if (tasksNextPageButton) tasksNextPageButton.addEventListener("click", goTasksNext);

  if (bookshelfNewFolderButton) bookshelfNewFolderButton.addEventListener("click", () => void createBookshelfFolderPrompt());
  if (bookshelfBreadcrumbNode) {
    bookshelfBreadcrumbNode.addEventListener("click", (event) => {
      const target = event.target.closest("[data-parent-id]");
      if (!target) return;
      const raw = target.dataset.parentId;
      const parentId = raw === "" ? null : Number.parseInt(raw, 10);
      void loadBookshelf(parentId);
    });
  }
  if (tasksClearFinishedButton) tasksClearFinishedButton.addEventListener("click", () => void clearFinishedTasks());

  void hydrateTasksFromServer();

  selectAllSourcesButton.addEventListener("click", () => {
    const visibleSources = filteredSources();
    if (!visibleSources.length) return setStatus("当前标签筛选下没有可选择的渠道。");
    visibleSources.forEach((source) => appState.selectedSites.add(source.key));
    saveSearchSourceState();
    renderSourceSelector();
    setStatus(appState.selectedSourceTags.size > 0 ? `已选中当前筛选范围内的 ${visibleSources.length} 个渠道。` : `已选中全部 ${appState.selectedSites.size} 个渠道。`);
  });

  clearSourcesButton.addEventListener("click", () => {
    const visibleSources = filteredSources();
    if (!visibleSources.length) return setStatus("当前标签筛选下没有可清空的渠道。");
    visibleSources.forEach((source) => appState.selectedSites.delete(source.key));
    saveSearchSourceState();
    renderSourceSelector();
    setStatus(appState.selectedSourceTags.size > 0 ? `已清空当前筛选范围内的 ${visibleSources.length} 个渠道选择。` : "已清空渠道选择。");
  });

  if (speedTestSourcesButton) {
    speedTestSourcesButton.addEventListener("click", () => void runSourceSpeedTest());
  }

  setupVersionPanel();
  setupSettingsNavigation();
  maybeAutoCheckVersion();

  clearTagFiltersButton.addEventListener("click", () => {
    if (appState.selectedSourceTags.size === 0) return;
    appState.selectedSourceTags = new Set();
    saveSearchSourceState();
    renderSourceTagFilters();
    renderSourceSelector();
    setStatus("已清空渠道标签筛选。");
  });

  setupSourceFiltersToggle();
  if (sourceTagEditorBackdrop) sourceTagEditorBackdrop.addEventListener("click", closeSourceTagEditor);
  if (sourceTagEditorCloseButton) sourceTagEditorCloseButton.addEventListener("click", closeSourceTagEditor);
  if (sourceTagEditorCancelButton) sourceTagEditorCancelButton.addEventListener("click", closeSourceTagEditor);
  if (sourceTagEditorAddButton) sourceTagEditorAddButton.addEventListener("click", () => addSourceTagEditorInput());
  if (sourceTagEditorSaveButton) sourceTagEditorSaveButton.addEventListener("click", () => void saveSourceTagEditor());
  if (sourceTagEditorInputNode) {
    sourceTagEditorInputNode.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === "," || event.key === "，") {
        event.preventDefault();
        addSourceTagEditorInput();
      } else if (event.key === "Backspace" && !sourceTagEditorInputNode.value && sourceTagEditorTags.length) {
        sourceTagEditorTags.pop();
        renderSourceTagEditor();
      }
    });
  }

  const goPrevPage = async () => {
    if (!appState.hasPrev || !appState.lastKeyword) return;
    appState.page -= 1;
    await performSearch();
    scrollToResultsTop();
  };

  const goNextPage = async () => {
    if (!appState.hasNext || !appState.lastKeyword) return;
    appState.page += 1;
    await performSearch();
    scrollToResultsTop();
  };

  prevPageButton.addEventListener("click", goPrevPage);
  nextPageButton.addEventListener("click", goNextPage);
  if (prevPageBottomButton) prevPageBottomButton.addEventListener("click", goPrevPage);
  if (nextPageBottomButton) nextPageBottomButton.addEventListener("click", goNextPage);

  searchForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    appState.page = 1;
    await performSearch(searchModeSelect ? searchModeSelect.value === "exact" : false, {
      sites: Array.from(appState.selectedSites),
      tag: "",
    });
  });

  detailCloseButton.addEventListener("click", closeDetail);
  detailBackdrop.addEventListener("click", closeDetail);
  openGeneralConfigButton.addEventListener("click", () => {
    void openSiteConfig();
  });
  closeSiteConfigButton.addEventListener("click", closeSiteConfig);
  siteConfigBackdrop.addEventListener("click", closeSiteConfig);
  
  siteConfigKeyNode.addEventListener("change", () => {
    populateSiteConfigForm(siteConfigKeyNode.value);
  });
  
  siteConfigForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    await saveSettingsConfig();
  });
  
  generalConfigForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    await saveSettingsConfig();
  });
  if (saveSettingsConfigButton) saveSettingsConfigButton.addEventListener("click", () => void saveSettingsConfig());
  
  // 切换密码显示小眼睛图标
  toggleSitePasswordButton.addEventListener("click", () => {
    const reveal = sitePasswordNode.type === "password";
    sitePasswordNode.type = reveal ? "text" : "password";
    toggleSitePasswordButton.innerHTML = reveal 
      ? `<svg viewBox="0 0 24 24" width="20" height="20" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"></path><line x1="1" y1="1" x2="23" y2="23"></line></svg>` 
      : `<svg viewBox="0 0 24 24" width="20" height="20" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path><circle cx="12" cy="12" r="3"></circle></svg>`;
  });
  
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && !sourceTagEditorOverlay.hidden) { closeSourceTagEditor(); return; }
    if (event.key === "Escape" && !readerOverlay.hidden) { closeReader(); return; }
    if (event.key === "Escape" && !detailOverlay.hidden) closeDetail();
    if (event.key === "Escape" && !siteConfigOverlay.hidden) closeSiteConfig();
  });

  window.addEventListener("scroll", () => {
    if (window.scrollY > 300) backToTopButton.classList.add("is-visible");
    else backToTopButton.classList.remove("is-visible");
  });

  backToTopButton.addEventListener("click", () => {
    window.scrollTo({ top: 0, behavior: "smooth" });
  });

  window.setTimeout(() => void restoreAppFromLocation(), 0);
}

function activateTab(tabName, options = {}) {
  if (!["search", "discover", "favorites"].includes(tabName)) tabName = "search";
  appState.activeTab = tabName;
  searchTabButton.classList.toggle("is-active", tabName === "search");
  if (discoverTabButton) discoverTabButton.classList.toggle("is-active", tabName === "discover");
  if (favoritesTabButton) favoritesTabButton.classList.toggle("is-active", tabName === "favorites");
  searchTabPanel.classList.toggle("is-active", tabName === "search");
  if (discoverTabPanel) discoverTabPanel.classList.toggle("is-active", tabName === "discover");
  if (favoritesTabPanel) favoritesTabPanel.classList.toggle("is-active", tabName === "favorites");
  if (tabName === "discover" && options.load !== false) {
    activateDiscoverSection(appState.discover.section || "trending", {
      syncURL: false,
      preserveList: true,
    });
  }
  if (tabName === "favorites") {
    // Keep the history badge accurate even before the user opens the history
    // sub-tab (the list itself is still loaded lazily).
    void refreshHistoryCount();
    activateSubTab(appState.activeSubTab || "bookshelf", options);
  }
  if (options.syncURL !== false) scheduleAppURLSync();
}

function activateSubTab(subTabName, options = {}) {
  if (subTabName !== "bookshelf" && subTabName !== "history" && subTabName !== "tasks") subTabName = "bookshelf";
  appState.activeSubTab = subTabName;
  if (bookshelfSubTab) bookshelfSubTab.classList.toggle("is-active", subTabName === "bookshelf");
  if (historySubTab) historySubTab.classList.toggle("is-active", subTabName === "history");
  if (tasksSubTab) tasksSubTab.classList.toggle("is-active", subTabName === "tasks");
  if (bookshelfSubPanel) bookshelfSubPanel.classList.toggle("is-active", subTabName === "bookshelf");
  if (historySubPanel) historySubPanel.classList.toggle("is-active", subTabName === "history");
  if (tasksSubPanel) tasksSubPanel.classList.toggle("is-active", subTabName === "tasks");
  if (subTabName === "bookshelf" && !appState.bookshelf.loading && options.load !== false) {
    // 每次切到书架都自动刷新，确保任务/外部修改后内容始终最新
    void loadBookshelf(appState.bookshelf.parentId);
  }
  if (subTabName === "history" && !appState.history.loaded && options.load !== false) {
    void loadHistory();
  }
  if (options.syncURL !== false) scheduleAppURLSync();
}

async function performSearch(exact = appState.lastExact, options = {}) {
  const keyword = keywordInput.value.trim();
  if (!keyword) return setStatus("请输入关键词。");
  options = options && typeof options === "object" ? options : {};
  const hasExplicitScope = Object.prototype.hasOwnProperty.call(options, "sites")
    || Object.prototype.hasOwnProperty.call(options, "tag");
  const requestedSites = (hasExplicitScope && Array.isArray(options.sites) ? options.sites : (
    !hasExplicitScope && Array.isArray(appState.lastSearchSites) && appState.lastSearchSites.length
      ? appState.lastSearchSites
      : Array.from(appState.selectedSites)
  )).filter((key) => sourceKeySet.has(key));
  const searchSites = Array.from(new Set(requestedSites));
  if (searchSites.length === 0) return setStatus("请至少选择一个渠道。");
  const requestedTag = hasExplicitScope
    ? String(options.tag || "").trim()
    : String(appState.lastTagSearch || "").trim();
  const tagSearch = searchSites.length === 1 ? requestedTag : "";
  const useExact = tagSearch ? false : Boolean(exact);
  if (searchModeSelect) searchModeSelect.value = useExact ? "exact" : "fuzzy";

  closeDetail();
  setSearchPageSize((appState.generalConfig && appState.generalConfig.web_page_size) || appState.pageSize);
  appState.lastKeyword = keyword;
  appState.lastExact = useExact;
  appState.lastSearchSites = searchSites.slice();
  appState.lastTagSearch = tagSearch;
  appState.lastTagSearchUsed = false;
  activateTab("search");
  scheduleAppURLSync();
  renderWarnings([]);
  setStatus(tagSearch
    ? `正在通过${sourceLabel(searchSites[0])}原站标签搜索“${tagSearch}”，第 ${appState.page} 页...`
    : `正在${useExact ? "精确搜索" : "模糊搜索"}“${keyword}”，第 ${appState.page} 页...`);

  try {
    const requestBody = {
      keyword, scope: "all", sites: searchSites,
      page: appState.page, page_size: appState.pageSize, exact: useExact,
    };
    if (tagSearch) requestBody.tag = tagSearch;
    const response = await fetch(`${root}/api/search`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(requestBody),
    });
    const payload = await response.json();
    if (!response.ok) {
      const err = new Error(payload.error || "search failed");
      err.payload = payload;
      throw err;
    }

    appState.results = payload.results || [];
    appState.total = payload.total || 0;
    appState.totalExact = payload.total_exact !== false;
    appState.hasPrev = Boolean(payload.has_prev);
    appState.hasNext = Boolean(payload.has_next);
    appState.page = payload.page || appState.page;
    appState.pageSize = Math.max(1, Number.parseInt(String(payload.page_size || appState.pageSize || defaultPageSize), 10) || defaultPageSize);
    appState.lastTagSearchUsed = Boolean(tagSearch && payload.tag_search);

    const warnings = payload.warnings || [];
    renderResults(appState.results);
    renderWarnings(warnings);
    renderPaging();
    renderResultMeta();

    if (!appState.results.length) {
      const suffix = warnings.length ? `；${temporaryWarningSummary(warnings)}` : "";
      const queryLabel = tagSearch ? `原站标签“${tagSearch}”` : `${useExact ? "精确搜索" : "模糊搜索"}“${keyword}”`;
      return setStatus(`没有搜索到${queryLabel}，可以换关键词、减少渠道标签筛选，或直接粘贴小说链接${suffix}。`, "empty");
    }
    const warningSuffix = warnings.length ? `，${warnings.length} 个渠道临时跳过` : "";
    const searchSuffix = tagSearch
      ? (appState.lastTagSearchUsed
        ? `，已使用 ${sourceLabel(searchSites[0])} 原站标签检索`
        : `，${sourceLabel(searchSites[0])} 已回退为关键词检索`)
      : "";
    setStatus(`当前显示第 ${appState.page} 页，共 ${totalLabel(appState.total, appState.totalExact)} 条结果${searchSuffix}${warningSuffix}。`);
    warmupDetailCache(appState.results);
  } catch (error) {
    appState.results = []; appState.total = 0; appState.totalExact = true;
    appState.hasPrev = false; appState.hasNext = false;
    renderResults([]); renderWarnings([]); renderPaging(); renderResultMeta();
    setStatus(`搜索失败：${error.message}`, "error");
  }
}

function renderSourceSelector() {
  sourceSelectorNode.innerHTML = "";
  const visibleSources = filteredSources();
  if (!visibleSources.length) {
    sourceSelectorNode.appendChild(createEmptyState("当前标签组合下没有匹配的渠道。", true));
    sourceSummaryNode.textContent = sourceSummaryText(0);
    refreshSourceFiltersToggleSummary();
    return;
  }

  visibleSources.forEach((source) => {
    const shell = document.createElement("div");
    shell.className = "source-option-shell";

    const button = document.createElement("button");
    button.type = "button";
    button.className = "source-option";
    button.setAttribute("aria-pressed", String(appState.selectedSites.has(source.key)));
    if (appState.selectedSites.has(source.key)) button.classList.add("is-selected");

    const title = document.createElement("span");
    title.className = "source-option-title";
    title.textContent = source.display_name || source.key;

    const key = document.createElement("span");
    key.className = "source-option-key";
    key.textContent = source.key;

    const tags = document.createElement("div");
    tags.className = "source-option-tags";
    (Array.isArray(source.tags) ? source.tags : []).filter(Boolean).forEach((tagText) => {
      const tag = document.createElement("span");
      tag.className = "source-option-tag";
      tag.textContent = tagText;
      tags.appendChild(tag);
    });
    if (!sourceSupportsDownload(source.key)) {
      const capability = document.createElement("span");
      capability.className = "source-option-tag is-capability";
      capability.textContent = "仅搜索";
      capability.title = "该渠道不提供正文下载，点击搜索结果会打开原站";
      tags.appendChild(capability);
    }

    const speedBadge = renderSourceSpeedBadge(source.key);
    if (speedBadge) tags.appendChild(speedBadge);

    button.appendChild(title);
    button.appendChild(key);
    button.appendChild(tags);
    button.addEventListener("click", () => toggleSource(source.key));

    const editButton = document.createElement("button");
    editButton.type = "button";
    editButton.className = "source-tag-edit-button";
    editButton.title = `编辑 ${source.display_name || source.key} 的标签`;
    editButton.setAttribute("aria-label", editButton.title);
    editButton.innerHTML = `<svg viewBox="0 0 24 24" width="15" height="15" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20.59 13.41 12 22l-9-9V4a2 2 0 0 1 2-2h9l6.59 6.59a2 2 0 0 1 0 2.82z"></path><circle cx="7.5" cy="7.5" r=".75" fill="currentColor" stroke="none"></circle></svg>`;
    editButton.addEventListener("click", () => openSourceTagEditor(source.key));

    shell.append(button, editButton);
    sourceSelectorNode.appendChild(shell);
  });
  sourceSummaryNode.textContent = sourceSummaryText(visibleSources.length);
  refreshSourceFiltersToggleSummary();
}

function openSourceTagEditor(siteKey) {
  const source = allSources.find((item) => item.key === siteKey);
  if (!source || !sourceTagEditorOverlay) return;
  sourceTagEditorSiteKey = siteKey;
  sourceTagEditorTags = sourceTags(source);
  sourceTagEditorPreviousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  sourceTagEditorTitleNode.textContent = source.display_name || source.key;
  sourceTagEditorKeyNode.textContent = source.key;
  sourceTagEditorInputNode.value = "";
  setSourceTagEditorStatus("");
  renderSourceTagEditor();
  hideTemporaryWarningDialog();
  sourceTagEditorOverlay.hidden = false;
  document.body.classList.add("has-overlay");
  window.requestAnimationFrame(() => sourceTagEditorInputNode.focus({ preventScroll: true }));
}

function closeSourceTagEditor() {
  if (!sourceTagEditorOverlay || sourceTagEditorOverlay.hidden || sourceTagEditorSaving) return;
  sourceTagEditorOverlay.hidden = true;
  sourceTagEditorSiteKey = "";
  sourceTagEditorTags = [];
  if (siteConfigOverlay.hidden && detailOverlay.hidden && readerOverlay.hidden) {
    document.body.classList.remove("has-overlay");
  }
  const focusTarget = sourceTagEditorPreviousFocus;
  sourceTagEditorPreviousFocus = null;
  if (focusTarget && focusTarget.isConnected) focusTarget.focus({ preventScroll: true });
}

function renderSourceTagEditor() {
  if (!sourceTagEditorSelectedNode || !sourceTagEditorSuggestionsNode) return;
  sourceTagEditorCountNode.textContent = `${sourceTagEditorTags.length} / 16`;
  sourceTagEditorSelectedNode.innerHTML = "";
  if (!sourceTagEditorTags.length) {
    const empty = document.createElement("div");
    empty.className = "source-tag-editor-chip-empty";
    empty.textContent = "暂未设置标签，添加后可用于渠道筛选。";
    sourceTagEditorSelectedNode.appendChild(empty);
  } else {
    sourceTagEditorTags.forEach((tagText) => {
      const chip = document.createElement("span");
      chip.className = "source-tag-editor-chip";
      const label = document.createElement("span");
      label.textContent = tagText;
      const remove = document.createElement("button");
      remove.type = "button";
      remove.setAttribute("aria-label", `移除标签 ${tagText}`);
      remove.textContent = "×";
      remove.addEventListener("click", () => {
        sourceTagEditorTags = sourceTagEditorTags.filter((item) => item !== tagText);
        setSourceTagEditorStatus("");
        renderSourceTagEditor();
      });
      chip.append(label, remove);
      sourceTagEditorSelectedNode.appendChild(chip);
    });
  }

  sourceTagEditorSuggestionsNode.innerHTML = "";
  const suggestions = [...SOURCE_TAG_SUGGESTIONS];
  sourceTagCatalog()
    .sort((left, right) => right.count - left.count)
    .forEach((item) => {
      if (!suggestions.includes(item.label)) suggestions.push(item.label);
    });
  suggestions.slice(0, 30).forEach((tagText) => {
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = tagText;
    button.disabled = sourceTagEditorTags.includes(tagText);
    button.addEventListener("click", () => addSourceTagEditorValue(tagText));
    sourceTagEditorSuggestionsNode.appendChild(button);
  });
}

function addSourceTagEditorInput() {
  if (!sourceTagEditorInputNode) return false;
  const added = addSourceTagEditorValue(sourceTagEditorInputNode.value);
  if (added) sourceTagEditorInputNode.value = "";
  return added;
}

function addSourceTagEditorValue(rawValue) {
  const tagText = `${rawValue || ""}`.trim();
  if (!tagText) return false;
  if (tagText.length > 24) {
    setSourceTagEditorStatus("单个标签不能超过 24 个字符。", "error");
    return false;
  }
  if (sourceTagEditorTags.includes(tagText)) {
    setSourceTagEditorStatus("这个标签已经在当前渠道中。", "error");
    return false;
  }
  if (sourceTagEditorTags.length >= 16) {
    setSourceTagEditorStatus("每个渠道最多设置 16 个标签。", "error");
    return false;
  }
  sourceTagEditorTags.push(tagText);
  setSourceTagEditorStatus("");
  renderSourceTagEditor();
  if (sourceTagEditorInputNode) sourceTagEditorInputNode.focus({ preventScroll: true });
  return true;
}

function setSourceTagEditorStatus(text, tone = "") {
  if (!sourceTagEditorStatusNode) return;
  sourceTagEditorStatusNode.textContent = text;
  if (tone) sourceTagEditorStatusNode.dataset.tone = tone;
  else delete sourceTagEditorStatusNode.dataset.tone;
}

async function saveSourceTagEditor() {
  if (!sourceTagEditorSiteKey || sourceTagEditorSaving) return;
  if (sourceTagEditorInputNode && sourceTagEditorInputNode.value.trim()) {
    if (!addSourceTagEditorInput()) return;
  }
  sourceTagEditorSaving = true;
  if (sourceTagEditorSaveButton) {
    sourceTagEditorSaveButton.disabled = true;
    sourceTagEditorSaveButton.textContent = "保存中...";
  }
  setSourceTagEditorStatus("正在写入本地标签数据库...", "info");
  const siteKey = sourceTagEditorSiteKey;
  try {
    const response = await fetch(`${root}/api/source-tags/${encodeURIComponent(siteKey)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ tags: sourceTagEditorTags }),
    });
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || "标签保存失败");
    const savedTags = Array.isArray(payload.item && payload.item.tags) ? payload.item.tags : sourceTagEditorTags;
    const source = allSources.find((item) => item.key === siteKey);
    if (source) source.tags = savedTags.slice();
    refreshSourceTagSet();
    appState.selectedSourceTags = new Set(
      Array.from(appState.selectedSourceTags).filter((tagText) => sourceTagSet.has(tagText)),
    );
    pruneSelectedSitesByVisibleSources();
    saveSearchSourceState();
    renderSourceTagFilters();
    renderSourceSelector();
    sourceTagEditorSaving = false;
    closeSourceTagEditor();
    showToast("渠道标签已保存", `${sourceLabel(siteKey)} 的标签已写入本地数据库。`, "success");
    setStatus(`已更新 ${sourceLabel(siteKey)} 的标签，可继续使用标签组合筛选渠道。`);
  } catch (error) {
    setSourceTagEditorStatus(`保存失败：${error.message}`, "error");
  } finally {
    sourceTagEditorSaving = false;
    if (sourceTagEditorSaveButton) {
      sourceTagEditorSaveButton.disabled = false;
      sourceTagEditorSaveButton.textContent = "保存标签";
    }
  }
}

function renderSourceSpeedBadge(siteKey) {
  const speed = appState.sourceSpeed;
  if (!speed) return null;
  const entry = speed.byKey.get(siteKey);
  if (!entry) {
    if (!speed.running) return null;
    const pending = document.createElement("span");
    pending.className = "source-option-speed is-pending";
    pending.textContent = "测速中…";
    return pending;
  }
  const badge = document.createElement("span");
  badge.className = "source-option-speed";
  if (entry.status === "pending") {
    badge.classList.add("is-pending");
    badge.textContent = "测速中…";
    return badge;
  }
  if (entry.status === "ok") {
    badge.classList.add("is-ok");
    const elapsed = formatSpeedMs(entry.elapsedMs);
    const countText = Number.isFinite(entry.count) ? `· ${entry.count} 条` : "";
    badge.textContent = `${elapsed} ${countText}`.trim();
    const sampleNote = entry.samples > 1 ? `，已取 ${entry.samples} 次最优` : "";
    badge.title = `响应 ${entry.elapsedMs} ms${sampleNote}，命中 ${entry.count} 条结果`;
    return badge;
  }
  badge.classList.add(entry.timedOut ? "is-timeout" : "is-error");
  badge.textContent = entry.timedOut ? `超时 · ${formatSpeedMs(entry.elapsedMs)}` : `失败 · ${formatSpeedMs(entry.elapsedMs)}`;
  badge.title = entry.error || (entry.timedOut ? "测速请求超时" : "测速失败");
  return badge;
}

function formatSpeedMs(ms) {
  const value = Math.max(0, Math.round(Number(ms) || 0));
  if (value < 1000) return `${value} ms`;
  return `${(value / 1000).toFixed(value < 10000 ? 2 : 1)} s`;
}

function setSpeedTestStatus(text) {
  if (!speedTestStatusNode) return;
  speedTestStatusNode.textContent = text || "";
}

async function runSourceSpeedTest() {
  if (!speedTestSourcesButton) return;
  const speed = appState.sourceSpeed;
  if (!speed || speed.running) return;

  const visibleSources = filteredSources();
  if (!visibleSources.length) {
    setStatus("当前标签筛选下没有可测速的渠道。");
    return;
  }

  const keyword = (keywordInput && keywordInput.value.trim()) || "测试";
  const sites = visibleSources.map((source) => source.key);

  speed.running = true;
  speed.keyword = keyword;
  speed.byKey = new Map(sites.map((key) => [key, { status: "pending" }]));
  speedTestSourcesButton.disabled = true;
  speedTestSourcesButton.classList.add("is-loading");
  setSpeedTestStatus(`正在测速：${sites.length} 个渠道，关键字「${keyword}」`);
  renderSourceSelector();

  try {
    const response = await fetch(`${root}/api/sources/speedtest`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ keyword, sites, per_site_timeout_ms: 8000 }),
    });
    if (!response.ok) {
      const payload = await response.json().catch(() => ({}));
      throw new Error(payload.error || `speedtest failed (${response.status})`);
    }
    const payload = await response.json();
    const results = Array.isArray(payload && payload.results) ? payload.results : [];
    const next = new Map();
    results.forEach((row) => {
      if (!row || !row.site) return;
      next.set(row.site, {
        status: row.ok ? "ok" : "error",
        elapsedMs: Number(row.elapsed_ms) || 0,
        count: Number(row.count) || 0,
        samples: Number(row.samples) || 0,
        error: row.error || "",
        timedOut: Boolean(row.timed_out),
      });
    });
    speed.byKey = next;
    speed.lastRunAt = Date.now();
    setSpeedTestStatus(summarizeSpeedResults(results));
  } catch (error) {
    setSpeedTestStatus(`测速失败：${error.message}`);
  } finally {
    speed.running = false;
    speedTestSourcesButton.disabled = false;
    speedTestSourcesButton.classList.remove("is-loading");
    renderSourceSelector();
  }
}

function summarizeSpeedResults(results) {
  if (!Array.isArray(results) || !results.length) return "测速完成：无结果。";
  const ok = results.filter((row) => row && row.ok);
  const fail = results.length - ok.length;
  if (!ok.length) return `测速完成：${results.length} 个渠道全部失败。`;
  const fastest = ok.reduce((best, row) => (best && best.elapsed_ms <= row.elapsed_ms ? best : row));
  const slowest = ok.reduce((worst, row) => (worst && worst.elapsed_ms >= row.elapsed_ms ? worst : row));
  const failNote = fail > 0 ? `，失败 ${fail}` : "";
  return `测速完成：成功 ${ok.length}${failNote}。最快 ${fastest.site} ${formatSpeedMs(fastest.elapsed_ms)}，最慢 ${slowest.site} ${formatSpeedMs(slowest.elapsed_ms)}。`;
}

// -----------------------------------------------------------------------------
// 版本检查
// -----------------------------------------------------------------------------

function setupVersionPanel() {
  if (versionCurrentNode) {
    versionCurrentNode.textContent = versionInfo.current ? `v${versionInfo.current}` : "未知";
  }
  if (versionRepoLinkNode && versionInfo.repo) {
    versionRepoLinkNode.href = `https://github.com/${versionInfo.repo}`;
  } else if (versionRepoLinkNode) {
    versionRepoLinkNode.hidden = true;
  }
  if (versionMirrorNode) {
    versionMirrorNode.innerHTML = "";
    const mirrors = Array.isArray(versionInfo.mirrors) ? versionInfo.mirrors : [];
    const auto = document.createElement("option");
    auto.value = "";
    auto.textContent = "自动（并发尝试所有镜像）";
    versionMirrorNode.appendChild(auto);
    mirrors.forEach((mirror) => {
      const opt = document.createElement("option");
      opt.value = mirror.key;
      opt.textContent = mirror.label || mirror.key;
      versionMirrorNode.appendChild(opt);
    });
  }
  if (versionCheckButton) {
    versionCheckButton.addEventListener("click", () => void runVersionCheck());
  }
}

async function runVersionCheck(options = {}) {
  const silent = Boolean(options.silent);
  const renderPanel = !silent || Boolean(options.renderPanel);
  if (!silent && !versionCheckButton) return null;
  const mirror = versionMirrorNode ? versionMirrorNode.value : "";
  if (!silent && versionCheckButton) {
    versionCheckButton.disabled = true;
    versionCheckButton.classList.add("is-loading");
  }
  if (renderPanel) {
    setVersionResult({ kind: "info", text: "正在检查最新版本..." });
  }
  try {
    const response = await fetch(`${root}/api/version/check`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ mirror }),
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      throw new Error(payload.error || `version check failed (${response.status})`);
    }
    if (payload.error) {
      if (renderPanel) setVersionResult({ kind: "error", text: `检查失败：${payload.error}` });
      return payload;
    }
    if (renderPanel) {
      renderVersionResult(payload, { notify: Boolean(options.notify) });
    } else if (options.notify) {
      notifyVersionUpdateIfNeeded(payload);
    }
    return payload;
  } catch (error) {
    if (renderPanel) setVersionResult({ kind: "error", text: `检查失败：${error.message}` });
    return null;
  } finally {
    if (!silent && versionCheckButton) {
      versionCheckButton.disabled = false;
      versionCheckButton.classList.remove("is-loading");
    }
  }
}

function maybeAutoCheckVersion() {
  if (!versionInfo || !versionInfo.current) return;
  const now = Date.now();
  try {
    const last = Number(window.localStorage.getItem(VERSION_AUTO_CHECK_KEY) || "0");
    if (Number.isFinite(last) && now - last < VERSION_AUTO_CHECK_INTERVAL) return;
    window.localStorage.setItem(VERSION_AUTO_CHECK_KEY, String(now));
  } catch (_) {
  }
  void runVersionCheck({ silent: true, notify: true });
}

function renderVersionResult(payload, options = {}) {
  const current = (payload.current || "").replace(/^v/, "");
  const latest = (payload.latest || "").replace(/^v/, "");
  if (!latest) {
    setVersionResult({ kind: "info", text: "未获取到最新版本号。" });
    return;
  }
  const cmp = compareSemver(latest, current);
  const mirrorTag = payload.mirror_used ? ` · 镜像 ${payload.mirror_used}` : "";
  const releaseLink = payload.html_url
    ? `<a href="${payload.html_url}" target="_blank" rel="noopener noreferrer">查看 Release</a>`
    : "";
  const publishedAt = payload.published_at ? `（发布于 ${formatDate(payload.published_at)}）` : "";

  if (cmp > 0) {
    setVersionResult({
      kind: "warning",
      html: `<strong>有新版本：v${latest}</strong>${publishedAt}<br>当前 v${current}${mirrorTag}<br>${releaseLink}`,
    });
    if (options.notify) showVersionUpdateReminder(payload, latest, current);
  } else if (cmp === 0) {
    setVersionResult({
      kind: "ok",
      html: `已是最新版本 <strong>v${current}</strong>${mirrorTag}<br>${releaseLink}`,
    });
  } else {
    setVersionResult({
      kind: "info",
      html: `当前 v${current} 高于 GitHub 最新发布 v${latest}${mirrorTag}<br>${releaseLink}`,
    });
  }
}

function notifyVersionUpdateIfNeeded(payload) {
  const current = (payload.current || "").replace(/^v/, "");
  const latest = (payload.latest || "").replace(/^v/, "");
  if (!latest || compareSemver(latest, current) <= 0) return false;
  showVersionUpdateReminder(payload, latest, current);
  return true;
}

function showVersionUpdateReminder(payload, latest, current) {
  const releaseURL = payload.html_url || (versionInfo.repo ? `https://github.com/${versionInfo.repo}/releases/latest` : "");
  const message = current ? `当前 v${current}，最新 v${latest}` : `最新 v${latest}`;
  showToast("发现新版本", message, "warning", {
    actionLabel: "查看 Release",
    actionURL: releaseURL,
    duration: 8000,
  });
}

function setVersionResult(opts) {
  if (!versionResultNode) return;
  versionResultNode.hidden = false;
  versionResultNode.dataset.kind = opts.kind || "info";
  if (opts.html) {
    versionResultNode.innerHTML = opts.html;
  } else {
    versionResultNode.textContent = opts.text || "";
  }
}

function compareSemver(a, b) {
  const pa = (a || "").split(/[.+-]/).map((part) => Number.parseInt(part, 10));
  const pb = (b || "").split(/[.+-]/).map((part) => Number.parseInt(part, 10));
  const len = Math.max(pa.length, pb.length);
  for (let i = 0; i < len; i += 1) {
    const ai = Number.isFinite(pa[i]) ? pa[i] : 0;
    const bi = Number.isFinite(pb[i]) ? pb[i] : 0;
    if (ai > bi) return 1;
    if (ai < bi) return -1;
  }
  return 0;
}

function formatDate(iso) {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return date.toLocaleString();
}

function toggleSource(siteKey) {
  if (appState.selectedSites.has(siteKey)) appState.selectedSites.delete(siteKey);
  else appState.selectedSites.add(siteKey);
  saveSearchSourceState();
  renderSourceSelector();
}

function renderSourceTagFilters() {
  if (!sourceTagFiltersNode) return;
  sourceTagFiltersNode.innerHTML = "";

  const tags = sourceTagCatalog();
  clearTagFiltersButton.hidden = appState.selectedSourceTags.size === 0;

  if (!tags.length) {
    sourceTagFiltersNode.appendChild(createEmptyInline("当前没有可用的渠道标签。"));
    return;
  }

  tags.forEach((item) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "source-tag-filter";
    button.setAttribute("aria-pressed", String(appState.selectedSourceTags.has(item.label)));
    if (appState.selectedSourceTags.has(item.label)) button.classList.add("is-active");

    const label = document.createElement("span");
    label.textContent = item.label;
    const count = document.createElement("span");
    count.className = "source-tag-filter-count";
    count.textContent = String(item.count);

    button.append(label, count);
    button.addEventListener("click", () => toggleSourceTag(item.label));
    sourceTagFiltersNode.appendChild(button);
  });
}

function toggleSourceTag(tagText) {
  if (appState.selectedSourceTags.has(tagText)) appState.selectedSourceTags.delete(tagText);
  else appState.selectedSourceTags.add(tagText);
  const removedCount = pruneSelectedSitesByVisibleSources();
  saveSearchSourceState();
  renderSourceTagFilters();
  renderSourceSelector();

  const visibleCount = filteredSources().length;
  if (appState.selectedSourceTags.size === 0) {
    setStatus("已清空渠道标签筛选。");
    return;
  }
  const removedLabel = removedCount > 0 ? `，并取消选择 ${removedCount} 个不匹配渠道` : "";
  setStatus(`已按标签 ${Array.from(appState.selectedSourceTags).join("、")} 筛选，当前显示 ${visibleCount} 个渠道${removedLabel}。`);
}

function sourceTagCatalog() {
  const counts = new Map();
  const ordered = [];
  allSources.forEach((source) => {
    sourceTags(source).forEach((tagText) => {
      if (!counts.has(tagText)) ordered.push(tagText);
      counts.set(tagText, (counts.get(tagText) || 0) + 1);
    });
  });
  return ordered.map((label) => ({ label, count: counts.get(label) || 0 }));
}

function sourceTags(source) {
  return (Array.isArray(source.tags) ? source.tags : []).map((tagText) => `${tagText || ""}`.trim()).filter(Boolean);
}

function sourceSupportsDownload(siteKey) {
  const capabilities = sourceCapabilitiesMap.get(siteKey) || {};
  return capabilities.download === true || capabilities.Download === true;
}

function openResultVariant(result, variant) {
  const activeVariant = variant || (result && result.primary) || {};
  if (sourceSupportsDownload(activeVariant.site)) {
    openDetail(result, activeVariant);
    return;
  }
  const targetURL = `${activeVariant.url || (result && result.url) || ""}`.trim();
  if (!targetURL) {
    setStatus("这个渠道当前只提供搜索，但没有返回可打开的原站链接。", "error");
    return;
  }
  window.open(targetURL, "_blank", "noopener,noreferrer");
}

function filteredSources() {
  if (appState.selectedSourceTags.size === 0) return allSources;
  return allSources.filter((source) => {
    const tags = new Set(sourceTags(source));
    return Array.from(appState.selectedSourceTags).every((tagText) => tags.has(tagText));
  });
}

function pruneSelectedSitesByVisibleSources() {
  const visibleKeys = new Set(filteredSources().map((source) => source.key));
  let removedCount = 0;
  Array.from(appState.selectedSites).forEach((siteKey) => {
    if (visibleKeys.has(siteKey)) return;
    appState.selectedSites.delete(siteKey);
    removedCount += 1;
  });
  return removedCount;
}

function sourceSummaryText(visibleCount) {
  const total = allSources.length;
  const selectedCount = appState.selectedSites.size;
  if (appState.selectedSourceTags.size === 0) {
    return `已选择 ${selectedCount} / ${total} 个渠道，高亮即已选。`;
  }
  return `标签筛选：${Array.from(appState.selectedSourceTags).join("、")}；当前显示 ${visibleCount} / ${total} 个渠道，已选择 ${selectedCount} 个。`;
}

function setupSourceFiltersToggle() {
  if (!sourceFiltersToggleButton || !sourceFiltersNode) return;
  const mql = window.matchMedia ? window.matchMedia("(max-width: 768px)") : null;
  const startCollapsed = mql ? mql.matches : false;
  applySourceFiltersCollapsed(startCollapsed);
  sourceFiltersToggleButton.addEventListener("click", () => {
    const collapsed = !sourceFiltersNode.classList.contains("is-collapsed");
    applySourceFiltersCollapsed(collapsed);
  });
  if (mql) {
    const mqlListener = (event) => {
      // Auto-collapse when entering mobile, auto-expand when leaving.
      applySourceFiltersCollapsed(event.matches);
    };
    if (typeof mql.addEventListener === "function") mql.addEventListener("change", mqlListener);
    else if (typeof mql.addListener === "function") mql.addListener(mqlListener);
  }
  refreshSourceFiltersToggleSummary();
}

function applySourceFiltersCollapsed(collapsed) {
  if (!sourceFiltersToggleButton || !sourceFiltersNode) return;
  sourceFiltersNode.classList.toggle("is-collapsed", collapsed);
  sourceFiltersToggleButton.classList.toggle("is-collapsed", collapsed);
  sourceFiltersToggleButton.setAttribute("aria-expanded", collapsed ? "false" : "true");
}

function refreshSourceFiltersToggleSummary() {
  if (!sourceFiltersToggleSummaryNode) return;
  const total = allSources.length;
  const selectedCount = appState.selectedSites.size;
  const tagCount = appState.selectedSourceTags.size;
  const parts = [`已选 ${selectedCount}/${total}`];
  if (tagCount > 0) parts.push(`${tagCount} 个标签`);
  sourceFiltersToggleSummaryNode.textContent = parts.join(" · ");
}

function renderWarnings(warnings) {
  warningsNode.innerHTML = "";
  const temporaryWarnings = [];
  const visibleWarnings = [];
  warnings.forEach((warning) => {
    const message = formatSearchWarning(warning);
    if (isTemporaryWarningText(message)) {
      const kind = warningKind(warning);
      temporaryWarnings.push({
        message,
        kind,
        level: kind === "timeout" || kind === "blocked" ? "warning" : "info",
        site_key: warning.site || "",
      });
      return;
    }
    visibleWarnings.push(warning);
  });
  if (visibleWarnings.length) warningsNode.appendChild(createWarningSummary(visibleWarnings));
  warningsNode.classList.toggle("is-empty", warningsNode.children.length === 0);
  if (temporaryWarnings.length) showTemporaryWarningDialog(temporaryWarnings);
}

function createWarningSummary(warnings) {
  const details = document.createElement("details");
  details.className = "warning-summary";

  const summary = document.createElement("summary");
  summary.className = "warning-summary-head";

  const icon = document.createElement("span");
  icon.className = "warning-summary-icon";
  icon.setAttribute("aria-hidden", "true");
  icon.textContent = "!";

  const copy = document.createElement("span");
  copy.className = "warning-summary-copy";
  const title = document.createElement("span");
  title.className = "warning-summary-title";
  const meta = document.createElement("span");
  meta.className = "warning-summary-meta";
  const labels = warnings.map((warning) => sourceLabel(warning.site));
  if (warnings.length === 1) {
    title.textContent = `${labels[0]}暂时不可用`;
    meta.textContent = `${warningReason(warnings[0])} · 其他渠道结果不受影响`;
  } else {
    title.textContent = `${warnings.length} 个渠道暂时不可用`;
    const shown = labels.slice(0, 3).join("、");
    meta.textContent = `${shown}${labels.length > 3 ? " 等" : ""} · 其他渠道结果不受影响`;
  }
  copy.append(title, meta);

  const toggle = document.createElement("span");
  toggle.className = "warning-summary-toggle";
  toggle.textContent = "详情";
  summary.append(icon, copy, toggle);

  const list = document.createElement("div");
  list.className = "warning-summary-list";
  warnings.forEach((warning) => {
    const row = document.createElement("div");
    row.className = "warning-summary-row";
    if (warning.error) row.title = String(warning.error);
    const site = document.createElement("span");
    site.className = "warning-summary-site";
    site.textContent = sourceLabel(warning.site);
    const reason = document.createElement("span");
    reason.className = "warning-summary-reason";
    reason.textContent = warningReason(warning);
    row.append(site, reason);
    list.appendChild(row);
  });

  details.append(summary, list);
  return details;
}

function warningReason(warning) {
  const error = String((warning && warning.error) || "").trim();
  const lower = error.toLowerCase();
  if (lower.includes("http 401") || error.includes("未登录") || lower.includes("unauthorized")) {
    return "需要登录或配置 Cookie";
  }
  if (lower.includes("getaddrinfow") || lower.includes("no such host") || (lower.includes("lookup ") && lower.includes("dial tcp"))) {
    return "域名暂时无法解析";
  }
  if (lower.includes("context deadline exceeded") || lower.includes("timeout")) {
    return "响应超时";
  }
  if (lower.includes("http 403")) {
    return "访问受限";
  }
  if (lower.includes("connection refused") || lower.includes("connectex") || lower.includes("dial tcp")) {
    return "网络连接失败";
  }
  const status = error.match(/\bHTTP\s+(\d{3})\b/i);
  if (status) return `服务返回 HTTP ${status[1]}`;
  return "暂时无法完成搜索";
}

function isTemporaryWarningText(text) {
  return String(text || "").includes("临时提示");
}

function formatSearchWarning(warning) {
  const site = sourceLabel(warning.site);
  const error = String(warning.error || "");
  const lower = error.toLowerCase();
  let reason = error;
  if (lower.includes("context deadline exceeded") || lower.includes("timeout")) {
    reason = "临时提示：站点响应超时。";
  } else if (lower.includes("http 403")) {
    reason = "临时提示：站点返回 403，可能触发访问限制或反爬验证。";
  }
  if (warning.site === "n8novel" && !reason.includes("无限轻小说")) {
    reason += " 无限轻小说近期较容易出现 403，可稍后重试。";
  }
  return `${site}：${reason}`;
}

function warningKind(warning) {
  const text = `${(warning && warning.error) || ""} ${(warning && warning.message) || ""}`.toLowerCase();
  if (text.includes("http 401") || text.includes(" 401") || text.includes("unauthorized") || text.includes("未登录")) return "auth";
  if (text.includes("context deadline exceeded") || text.includes("timeout") || text.includes("超时")) return "timeout";
  if (text.includes("http 403") || text.includes(" 403") || text.includes("访问限制") || text.includes("反爬")) return "blocked";
  return "info";
}

function temporaryWarningSummary(warnings) {
  const labels = warnings.slice(0, 3).map((warning) => sourceLabel(warning.site));
  const rest = warnings.length > labels.length ? `${labels.join("、")} 等 ${warnings.length} 个渠道` : labels.join("、");
  return `${rest}临时不可用，已显示其它可用渠道结果`;
}

function normalizeDiscoverSection(value) {
  return ["trending", "ranking", "booklist"].includes(value) ? value : "trending";
}

function activateDiscoverSection(section, options = {}) {
  section = normalizeDiscoverSection(section);
  const discover = appState.discover;
  discover.section = section;
  if (!options.preserveList) {
    discover.activeList = null;
    discover.activeBooks = [];
    discover.activePage = 1;
    discover.activeTotal = 0;
    discover.activeHasMore = false;
    discover.listError = "";
  }

  discoverSectionTabs.forEach((tab) => {
    const active = tab.dataset.discoverSection === section;
    tab.classList.toggle("is-active", active);
    tab.setAttribute("aria-selected", String(active));
  });

  if (options.activateTab !== false && appState.activeTab !== "discover") {
    activateTab("discover", { load: false, syncURL: false });
  }
  renderDiscover();

  if (section === "trending") {
    void ensureDiscoverTrending();
  } else {
    void ensureDiscoverCatalog().then(() => renderDiscoverCatalog()).catch(() => {});
  }
  if (options.syncURL !== false) scheduleAppURLSync();
}

async function ensureDiscoverCatalog() {
  const discover = appState.discover;
  if (discover.catalogLoaded) return;
  if (discover.catalogPromise) return discover.catalogPromise;
  discover.catalogLoading = true;
  discover.catalogNotice = "";
  renderDiscoverCatalog();
  discover.catalogPromise = (async () => {
    try {
      const payload = await fetchJSONWithTimeout(
        `${window.location.origin}${root}/api/discovery/catalog`,
        {},
        20000,
        "发现页渠道加载超时，请稍后重试。",
      );
      discover.catalog = Array.isArray(payload.sites) ? payload.sites : [];
      setDiscoverWarnings("catalog", Array.isArray(payload.warnings) ? payload.warnings : []);
      discover.catalogLoaded = true;
      if (!discover.activeSite || !discover.catalog.some((item) => item.site === discover.activeSite)) {
        discover.activeSite = discover.catalog[0] ? discover.catalog[0].site : "";
      }
    } catch (error) {
      discover.catalogNotice = compactDiscoveryError(error);
      setDiscoverWarnings("catalog", []);
    } finally {
      discover.catalogLoading = false;
      discover.catalogPromise = null;
      renderDiscoverCatalog();
      scheduleAppURLSync();
    }
  })();
  return discover.catalogPromise;
}

async function ensureDiscoverTrending() {
  const discover = appState.discover;
  if (discover.trendingLoaded) return;
  if (discover.trendingPromise) return discover.trendingPromise;
  discover.trendingLoading = true;
  discover.trendingNotice = "";
  renderDiscoverTrending();
  discover.trendingPromise = (async () => {
    try {
      const payload = await fetchJSONWithTimeout(
        `${window.location.origin}${root}/api/discovery/trending?limit=30`,
        {},
        30000,
        "正在读榜单加载超时，请稍后重试。",
      );
      discover.trendingBooks = Array.isArray(payload.books) ? payload.books : [];
      discover.trendingLoaded = true;
      setDiscoverWarnings("trending", Array.isArray(payload.warnings) ? payload.warnings : []);
    } catch (error) {
      discover.trendingNotice = compactDiscoveryError(error);
      setDiscoverWarnings("trending", []);
    } finally {
      discover.trendingLoading = false;
      discover.trendingPromise = null;
      renderDiscoverTrending();
    }
  })();
  return discover.trendingPromise;
}

function renderDiscover() {
  if (!discoverTabPanel) return;
  const discover = appState.discover;
  const listMode = Boolean(discover.activeList);
  discoverSectionTabs.forEach((tab) => {
    const active = tab.dataset.discoverSection === discover.section;
    tab.classList.toggle("is-active", active);
    tab.setAttribute("aria-selected", String(active));
  });
  if (discoverTrendingPanel) discoverTrendingPanel.hidden = listMode || discover.section !== "trending";
  if (discoverCatalogPanel) discoverCatalogPanel.hidden = listMode || discover.section === "trending";
  if (discoverListPanel) discoverListPanel.hidden = !listMode;
  renderDiscoverWarnings();
  if (listMode) {
    renderDiscoverList();
  } else if (discover.section === "trending") {
    renderDiscoverTrending();
  } else {
    renderDiscoverCatalog();
  }
}

function renderDiscoverTrending() {
  const discover = appState.discover;
  if (!discoverTrendingGridNode || !discoverTrendingStatusNode) return;
  discoverTrendingGridNode.innerHTML = "";
  discoverTrendingCountNode.textContent = String(discover.trendingBooks.length);
  if (discover.trendingLoading) {
    setDiscoverStatus(discoverTrendingStatusNode, "正在汇总各渠道热读作品…", true);
  } else if (discover.trendingNotice) {
    setDiscoverStatus(discoverTrendingStatusNode, discover.trendingNotice, true, "error");
  } else if (!discover.trendingBooks.length) {
    setDiscoverStatus(discoverTrendingStatusNode, "暂时没有可展示的热读作品。", true);
  } else {
    setDiscoverStatus(discoverTrendingStatusNode, "", false);
  }
  discover.trendingBooks.forEach((book, index) => {
    discoverTrendingGridNode.appendChild(createDiscoveryBookCard(book, index));
  });
}

function renderDiscoverCatalog() {
  const discover = appState.discover;
  if (!discoverCatalogPanel || discoverCatalogPanel.hidden) return;
  const booklistMode = discover.section === "booklist";
  discoverCatalogTitleNode.textContent = booklistMode ? "书单推荐" : "排行榜";
  discoverCatalogHintNode.textContent = booklistMode
    ? "只展示渠道公开维护的真实书单，不拼接或伪造内容。"
    : "选择渠道，再进入榜单查看当前热门作品。";

  const sites = discover.catalog.filter((source) => (
    Array.isArray(source.lists) && source.lists.some((list) => list.kind === (booklistMode ? "booklist" : "ranking"))
  ));
  if (discover.catalogLoading) {
    setDiscoverStatus(discoverStatusNode, "正在读取渠道榜单…", true);
  } else if (discover.catalogNotice) {
    setDiscoverStatus(discoverStatusNode, discover.catalogNotice, true, "error");
  } else if (!sites.length) {
    setDiscoverStatus(discoverStatusNode, booklistMode ? "当前渠道暂未公开书单。" : "当前没有可用的榜单渠道。", true);
  } else {
    setDiscoverStatus(discoverStatusNode, "", false);
  }

  if (!sites.some((source) => source.site === discover.activeSite)) {
    discover.activeSite = sites[0] ? sites[0].site : "";
  }
  discoverSiteTabsNode.innerHTML = "";
  discoverSiteTabsNode.hidden = !sites.length;
  sites.forEach((source) => {
    const listCount = source.lists.filter((list) => list.kind === (booklistMode ? "booklist" : "ranking")).length;
    const button = document.createElement("button");
    button.type = "button";
    button.className = "discover-site-tab";
    button.classList.toggle("is-active", source.site === discover.activeSite);
    button.setAttribute("aria-pressed", String(source.site === discover.activeSite));
    button.title = source.name || sourceLabel(source.site);
    const name = document.createElement("span");
    name.className = "discover-site-tab-name";
    name.textContent = discoverSiteShortLabel(source.site, source.name || sourceLabel(source.site));
    const count = document.createElement("span");
    count.className = "discover-site-tab-count";
    count.textContent = String(listCount);
    button.append(name, count);
    button.addEventListener("click", () => {
      discover.activeSite = source.site;
      renderDiscoverCatalog();
      scheduleAppURLSync();
    });
    discoverSiteTabsNode.appendChild(button);
  });

  discoverListGridNode.innerHTML = "";
  const activeSource = sites.find((source) => source.site === discover.activeSite);
  const lists = activeSource
    ? activeSource.lists.filter((list) => list.kind === (booklistMode ? "booklist" : "ranking"))
    : [];
  lists.forEach((list, index) => {
    discoverListGridNode.appendChild(createDiscoverListCard(activeSource, list, index));
  });
}

function discoverSiteShortLabel(siteKey, fallback = "") {
  const key = String(siteKey || "").trim();
  return discoverySiteShortLabels[key] || fallback || sourceLabel(key);
}

function createDiscoverListCard(source, list, index) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "discover-list-card";
  button.dataset.accent = String(index % 4);
  button.setAttribute("aria-label", `查看${source.name || sourceLabel(source.site)}${list.name}`);

  const cover = createDiscoverCover(list, source, index);
  const body = document.createElement("span");
  body.className = "discover-list-card-body";
  const title = document.createElement("strong");
  title.className = "discover-list-card-title";
  title.textContent = list.name || "未命名榜单";
  const description = document.createElement("span");
  description.className = "discover-list-card-description";
  description.textContent = list.description || "查看原站公开内容";
  const meta = document.createElement("span");
  meta.className = "discover-list-card-meta";
  meta.textContent = [list.update_frequency, list.book_count ? `${list.book_count} 本` : ""].filter(Boolean).join(" · ") || "原站实时内容";
  body.append(title, description, meta);
  button.append(cover, body);
  button.addEventListener("click", () => void openDiscoverList(source.site, list));
  return button;
}

function createDiscoverCover(list, source, index) {
  const cover = document.createElement("span");
  cover.className = "discover-list-cover";
  cover.dataset.accent = String(index % 4);
  if (list.cover_url) {
    const image = document.createElement("img");
    image.src = list.cover_url;
    image.alt = list.name || "书单封面";
    image.loading = "lazy";
    image.addEventListener("error", () => image.remove());
    cover.appendChild(image);
  }
  const glyph = document.createElement("span");
  glyph.className = "discover-list-glyph";
  glyph.textContent = list.kind === "booklist" ? "单" : "榜";
  const sequence = document.createElement("span");
  sequence.className = "discover-list-sequence";
  sequence.textContent = String(index + 1).padStart(2, "0");
  const sourceBadge = document.createElement("span");
  sourceBadge.className = "discover-list-source";
  sourceBadge.textContent = source.name || sourceLabel(source.site);
  cover.append(glyph, sequence, sourceBadge);
  return cover;
}

async function openDiscoverList(siteKey, list, page = 1) {
  const discover = appState.discover;
  const token = ++discover.requestToken;
  const pageSize = discoverListPageSize();
  const requestedPage = Math.max(1, Math.trunc(Number(page) || 1));
  const previousPage = discover.activePage || 1;
  const pageChanged = requestedPage !== previousPage;
  discover.activeSite = siteKey;
  discover.activeList = {
    ...list,
    site: siteKey,
    site_name: sourceLabel(siteKey),
  };
  discover.activePage = requestedPage;
  discover.activePageSize = pageSize;
  discover.activeBooks = requestedPage > 1 ? discover.activeBooks : [];
  discover.listLoading = true;
  discover.listError = "";
  renderDiscover();
  scheduleAppURLSync();

  try {
    const url = new URL(`${window.location.origin}${root}/api/discovery/list`);
    url.searchParams.set("site", siteKey);
    url.searchParams.set("id", list.id);
    url.searchParams.set("page", String(requestedPage));
    url.searchParams.set("page_size", String(pageSize));
    const payload = await fetchJSONWithTimeout(
      url.toString(),
      {},
      35000,
      `${sourceLabel(siteKey)}榜单加载超时，请稍后重试。`,
    );
    if (token !== discover.requestToken) return;
    discover.activeList = {
      ...(payload.list || list),
      site: siteKey,
      site_name: payload.name || sourceLabel(siteKey),
    };
    discover.activePage = Math.max(1, Number(payload.page) || requestedPage);
    discover.activePageSize = Math.max(1, Number(payload.page_size) || pageSize);
    discover.activeTotal = Math.max(0, Number(payload.total) || 0);
    discover.activeHasMore = Boolean(payload.has_more);
    discover.activeBooks = Array.isArray(payload.books) ? payload.books : [];
  } catch (error) {
    if (token !== discover.requestToken) return;
    discover.listError = compactDiscoveryError(error);
  } finally {
    if (token !== discover.requestToken) return;
    discover.listLoading = false;
    renderDiscoverList();
    if (pageChanged && discoverListPanel) {
      window.requestAnimationFrame(() => {
        discoverListPanel.scrollIntoView({ behavior: "smooth", block: "start" });
      });
    }
  }
}

function discoverListPageSize() {
  const configured = Number.parseInt(String(appState.pageSize || ""), 10);
  if (Number.isFinite(configured) && configured > 0) {
    return Math.min(100, configured);
  }
  return 30;
}

function closeDiscoverList(options = {}) {
  const discover = appState.discover;
  discover.requestToken += 1;
  discover.activeList = null;
  discover.activeBooks = [];
  discover.activePage = 1;
  discover.activeTotal = 0;
  discover.activeHasMore = false;
  discover.listLoading = false;
  discover.listError = "";
  renderDiscover();
  if (options.syncURL !== false) scheduleAppURLSync();
}

function renderDiscoverList() {
  const discover = appState.discover;
  if (!discoverListHeaderNode || !discoverBookGridNode || !discoverBookStatusNode) return;
  const list = discover.activeList;
  if (!list) return;

  discoverListHeaderNode.innerHTML = "";
  const cover = createDiscoverCover(list, {
    site: list.site,
    name: list.site_name || sourceLabel(list.site),
  }, 0);
  cover.classList.add("discover-list-header-cover");
  const copy = document.createElement("div");
  copy.className = "discover-list-header-copy";
  const source = document.createElement("span");
  source.className = "discover-eyebrow";
  source.textContent = `${list.site_name || sourceLabel(list.site)} · ${list.kind === "booklist" ? "BOOKLIST" : "RANKING"}`;
  const title = document.createElement("h2");
  title.textContent = list.name || "榜单详情";
  const description = document.createElement("p");
  description.textContent = list.description || "以下内容直接来自原站公开页面。";
  const meta = document.createElement("div");
  meta.className = "discover-list-header-meta";
  const totalLabel = discover.activeTotal > 0 ? `共 ${discover.activeTotal} 本` : (discover.activeBooks.length ? `${discover.activeBooks.length} 本` : "");
  [list.update_frequency, totalLabel]
    .filter(Boolean)
    .forEach((text) => {
      const chip = document.createElement("span");
      chip.textContent = text;
      meta.appendChild(chip);
    });
  copy.append(source, title, description, meta);
  discoverListHeaderNode.append(cover, copy);

  discoverBookGridNode.innerHTML = "";
  const rankOffset = Math.max(0, (discover.activePage - 1) * discover.activePageSize);
  discover.activeBooks.forEach((book, index) => {
    discoverBookGridNode.appendChild(createDiscoveryBookCard(book, index, rankOffset));
  });
  renderDiscoveryBookPagination();
  if (discover.listLoading) {
    setDiscoverStatus(discoverBookStatusNode, "正在加载榜单…", true);
  } else if (discover.listError) {
    setDiscoverStatus(discoverBookStatusNode, discover.listError, true, "error");
  } else if (!discover.activeBooks.length) {
    setDiscoverStatus(discoverBookStatusNode, "这个榜单暂时没有可展示的作品。", true);
  } else {
    setDiscoverStatus(discoverBookStatusNode, "", false);
  }
}

function renderDiscoveryBookPagination() {
  const discover = appState.discover;
  const node = discoverBookPaginationNode;
  if (!node) return;
  const page = Math.max(1, discover.activePage || 1);
  const hasPrev = page > 1;
  const hasNext = Boolean(discover.activeHasMore);
  node.hidden = !hasPrev && !hasNext;
  if (discoverBooksPrevPageButton) discoverBooksPrevPageButton.disabled = !hasPrev || discover.listLoading;
  if (discoverBooksNextPageButton) discoverBooksNextPageButton.disabled = !hasNext || discover.listLoading;
  if (discoverBookPageIndicatorNode) {
    const total = discover.activeTotal > 0 ? ` · 共 ${discover.activeTotal} 本` : "";
    discoverBookPageIndicatorNode.textContent = `第 ${page} 页${total}`;
  }
}

function createDiscoveryBookCard(book, index = 0, rankOffset = 0) {
  const canDownload = sourceSupportsDownload(book.site);
  const rankNumber = Math.max(1, Number(rankOffset) + Number(index) + 1);
  const result = {
    title: book.title || "",
    author: book.author || "",
    cover_url: book.cover_url || "",
    description: book.description || "",
    latest_chapter: book.latest_chapter || "",
    url: book.url || "",
    primary: book,
    variants: [book],
    sources: [book],
    source_count: 1,
  };
  const card = document.createElement("article");
  card.className = "result-card is-clickable discover-book-card";
  card.dataset.rank = String(rankNumber);
  card.setAttribute("tabindex", "0");
  card.setAttribute("aria-label", `${canDownload ? "查看" : "打开"}${book.title || "未命名小说"}`);
  const open = () => openResultVariant(result, book);
  card.addEventListener("click", open);
  card.addEventListener("keydown", (event) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      open();
    }
  });

  const coverButton = document.createElement("button");
  coverButton.type = "button";
  coverButton.className = "result-cover-button";
  coverButton.setAttribute("aria-label", `${canDownload ? "查看" : "打开"}${book.title || "未命名小说"}`);
  coverButton.appendChild(createCoverImage(book.cover_url, book.title, "result-cover"));
  const rank = document.createElement("span");
  rank.className = "discover-rank-badge";
  rank.textContent = String(rankNumber);
  coverButton.appendChild(rank);

  const body = document.createElement("div");
  body.className = "result-body";
  const title = document.createElement("h3");
  title.className = "result-title";
  title.textContent = book.title || book.book_id || "未命名小说";
  const author = document.createElement("p");
  author.className = "result-author";
  author.textContent = `作者：${book.author || "未知作者"}`;
  const source = document.createElement("p");
  source.className = "result-source";
  source.textContent = `来源：${sourceLabel(book.site)}${canDownload ? "" : " · 仅搜索"}`;
  body.append(title, author, source);
  if (book.description) {
    const description = document.createElement("p");
    description.className = "discover-book-description";
    description.textContent = book.description;
    body.appendChild(description);
  }
  if (book.latest_chapter) {
    const extra = document.createElement("p");
    extra.className = "result-extra";
    extra.textContent = `最新：${book.latest_chapter}`;
    body.appendChild(extra);
  }
  card.append(coverButton, body);
  return card;
}

function setDiscoverStatus(node, text, visible, tone = "") {
  if (!node) return;
  node.hidden = !visible;
  node.textContent = visible ? text : "";
  if (tone) node.dataset.tone = tone;
  else delete node.dataset.tone;
}

function setDiscoverWarnings(scope, warnings) {
  const discover = appState.discover;
  if (!discover.warningScopes) discover.warningScopes = {};
  discover.warningScopes[scope] = Array.isArray(warnings) ? warnings : [];
  discover.warnings = Object.values(discover.warningScopes).flat();
  renderDiscoverWarnings();
}

function renderDiscoverWarnings() {
  if (!discoverWarningsNode) return;
  discoverWarningsNode.innerHTML = "";
  const seen = new Set();
  const warnings = (appState.discover.warnings || []).filter((warning) => {
    const key = `${warning.site || ""}:${warning.error || warning.message || ""}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
  discoverWarningsNode.hidden = warnings.length === 0;
  warnings.forEach((warning) => {
    const item = document.createElement("div");
    item.className = "discover-warning";
    const site = document.createElement("strong");
    site.textContent = sourceLabel(warning.site);
    const message = document.createElement("span");
    message.textContent = compactDiscoveryError(warning.error || warning.message || "暂时不可用");
    item.append(site, message);
    discoverWarningsNode.appendChild(item);
  });
}

function compactDiscoveryError(error) {
  const text = String((error && (error.message || error.error)) || error || "请求失败")
    .replace(/\s+/g, " ")
    .trim();
  return text.length > 180 ? `${text.slice(0, 177)}...` : text;
}

function renderResults(results) {
  resultsNode.innerHTML = "";
  if (!results.length) return resultsNode.appendChild(createEmptyState("当前页没有可展示的结果。"));

  results.forEach((result) => {
    const canDownload = sourceSupportsDownload(result.preferred_site);
    const card = document.createElement("article");
    card.className = "result-card is-clickable";
    card.setAttribute("tabindex", "0");
    card.setAttribute("aria-label", `${canDownload ? "查看" : "打开"} ${displayResultTitle(result)}`);
    const open = () => openResultVariant(result, result.primary);
    card.addEventListener("click", open);
    card.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        open();
      }
    });

    const coverButton = document.createElement("button");
    coverButton.type = "button";
    coverButton.className = "result-cover-button";
    coverButton.setAttribute("aria-label", `查看 ${displayResultTitle(result)} 的详情`);
    coverButton.appendChild(createCoverImage(result.cover_url, displayResultTitle(result), "result-cover"));

    const overlay = document.createElement("span");
    overlay.className = "result-cover-overlay";
    overlay.textContent = canDownload ? "查看详情" : "打开原站";
    coverButton.appendChild(overlay);

    const body = document.createElement("div");
    body.className = "result-body";

    const title = document.createElement("h3");
    title.className = "result-title";
    title.textContent = displayResultTitle(result);

    const author = document.createElement("p");
    author.className = "result-author";
    author.textContent = `作者：${displayResultAuthor(result)}`;

    const source = document.createElement("p");
    source.className = "result-source";
    source.textContent = `源：${sourceLabel(result.preferred_site)}${canDownload ? "" : " · 仅搜索"}`;

    body.appendChild(title); body.appendChild(author); body.appendChild(source);

    if (result.latest_chapter) {
      const extra = document.createElement("p");
      extra.className = "result-extra";
      extra.textContent = `最新：${result.latest_chapter}`;
      body.appendChild(extra);
    }
    card.appendChild(coverButton); card.appendChild(body);
    resultsNode.appendChild(card);
  });
}

function openDetail(result, variant) {
  openDetailPage(result, variant, 1, true);
}

function openDetailPage(result, variant, chapterPage = 1, resetScroll = false) {
  const activeVariant = variant || result.primary;
  const pageSize = normalizedChapterPageSize();
  const baseKey = detailKey(activeVariant);
  const cacheKey = detailRequestKey(activeVariant, chapterPage, pageSize);
  appState.detailResult = result;
  appState.activeDetailVariant = activeVariant;
  appState.activeDetailKey = baseKey;
  appState.activeDetailPage = chapterPage;
  appState.activeDetailPageSize = pageSize;
  detailOverlay.hidden = false;
  document.body.classList.add("has-overlay");
  scheduleAppURLSync();
  const detailPanel = detailContentNode.closest(".detail-panel");
  if (detailPanel && resetScroll) detailPanel.scrollTop = 0;

  const cached = appState.detailCache.get(cacheKey);
  if (cached) return renderDetail(result, activeVariant, cached, false, "");

  renderDetail(result, activeVariant, null, true, "");
  void loadDetail(result, activeVariant, cacheKey, chapterPage, pageSize);
}

function closeDetail() {
  detailOverlay.hidden = true; document.body.classList.remove("has-overlay");
  scheduleAppURLSync();
}

async function loadDetail(result, variant, cacheKey, chapterPage = 1, chapterPageSize = normalizedChapterPageSize()) {
  const startedAt = performance.now();
  try {
    let pending = appState.detailPending.get(cacheKey);
    if (!pending) {
      pending = fetchBookDetail(variant, chapterPage, chapterPageSize).finally(() => {
        appState.detailPending.delete(cacheKey);
      });
      appState.detailPending.set(cacheKey, pending);
    }
    const detail = await pending;
    if (!detail || !detail.book) throw new Error("未返回详情数据");

    appState.detailCache.set(cacheKey, detail);
    appState.detailTimings.set(cacheKey, Math.max(1, Math.round(performance.now() - startedAt)));
    if (isActiveDetailRequest(result, variant, chapterPage, chapterPageSize)) {
      renderDetail(result, variant, detail, false, "");
    }
  } catch (error) {
    if (isActiveDetailRequest(result, variant, chapterPage, chapterPageSize)) {
      renderDetail(result, variant, null, false, error.message);
    }
  }
}

async function fetchBookDetail(variant, chapterPage = 1, chapterPageSize = normalizedChapterPageSize(), options = {}) {
  const url = new URL(`${window.location.origin}${root}/api/books/detail`);
  url.searchParams.set("site", variant.site);
  url.searchParams.set("book_id", variant.book_id);
  url.searchParams.set("chapter_page", String(chapterPage));
  url.searchParams.set("chapter_page_size", String(chapterPageSize));
  if (options.local || variant.local) url.searchParams.set("local", "1");
  const payload = await fetchJSONWithTimeout(
    url.toString(),
    {},
    detailLoadTimeoutMs(variant.site),
    `${sourceLabel(variant.site)} 详情/章节目录加载超时，请稍后重试或暂时切换来源`,
  );
  return {
    book: payload.book || null,
    chapterPage: payload.chapter_page || null,
  };
}

async function warmupDetailCache(results) {
  const token = ++appState.detailWarmupToken;
  const queue = results.slice(0, 6).map((result) => ({
    result,
    variant: result.primary,
  })).filter((item) => item.variant && item.variant.site && item.variant.book_id && sourceSupportsDownload(item.variant.site) && shouldWarmupDetail(item.variant.site) && !appState.detailCache.has(detailRequestKey(item.variant, 1, normalizedChapterPageSize())));
  const workers = Math.min(2, queue.length);
  let cursor = 0;

  async function worker() {
    while (token === appState.detailWarmupToken && cursor < queue.length) {
      const item = queue[cursor++];
      await loadDetail(item.result, item.variant, detailRequestKey(item.variant, 1, normalizedChapterPageSize()), 1, normalizedChapterPageSize());
    }
  }

  await Promise.all(Array.from({ length: workers }, worker));
}

function normalizeDetailTags(value) {
  const items = Array.isArray(value) ? value : (typeof value === "string" ? [value] : []);
  const seen = new Set();
  return items.map((tag) => String(tag == null ? "" : tag).trim()).filter((tag) => {
    if (!tag || seen.has(tag)) return false;
    seen.add(tag);
    return true;
  });
}

function searchDetailTag(tagText, sourceSite) {
  const tag = String(tagText || "").trim();
  if (!tag) return;
  const sites = sourceKeySet.has(sourceSite) ? [sourceSite] : Array.from(appState.selectedSites);
  keywordInput.value = tag;
  appState.page = 1;
  void performSearch(false, { sites, tag: sourceKeySet.has(sourceSite) ? tag : "" });
}

function renderDetail(result, variant, detail, loading, errorMessage) {
  detailContentNode.innerHTML = "";
  const activeVariant = variant || result.primary;
  const variants = Array.isArray(result.variants) && result.variants.length ? result.variants : [result.primary];
  const book = detail && detail.book ? detail.book : detail;
  const chapterPage = normalizeChapterPage(book, detail && detail.chapterPage);
  const chapterOffset = Math.max(0, (chapterPage.page - 1) * chapterPage.page_size);
  const title = displayDetailTitle(result, activeVariant, book);
  const author = displayDetailAuthor(result, activeVariant, book);
  const description = displayDetailDescription(result, book);
  const bookTags = normalizeDetailTags(book && book.tags);
  const chapters = Array.isArray(book && book.chapters) ? book.chapters : [];

  const hero = document.createElement("section");
  hero.className = "detail-hero";
  hero.appendChild(createCoverImage(book && book.cover_url ? book.cover_url : result.cover_url, title, "detail-cover"));

  const summary = document.createElement("div");
  summary.className = "detail-summary";

  const heading = document.createElement("h2");
  heading.id = "detailHeading"; heading.className = "detail-title";
  const detailURL = displayDetailURL(result, activeVariant, book);
  if (detailURL) {
    const titleLink = document.createElement("a");
    titleLink.className = "detail-title-link"; titleLink.href = detailURL;
    titleLink.target = "_blank"; titleLink.rel = "noopener noreferrer";
    titleLink.textContent = title;
    heading.appendChild(titleLink);
  } else heading.textContent = title;

  const authorNode = document.createElement("p");
  authorNode.className = "detail-author"; authorNode.textContent = `作者：${author}`;

  const sourceNode = document.createElement("p");
  sourceNode.className = "detail-source"; sourceNode.textContent = `当前源：${sourceLabel(activeVariant.site)}`;

  summary.appendChild(heading); summary.appendChild(authorNode); summary.appendChild(sourceNode);

  const meta = document.createElement("div");
  meta.className = "detail-meta";
  meta.appendChild(resultBadge(sourceLabel(activeVariant.site)));
  meta.appendChild(resultBadge(chapterPage.total ? `${chapterPage.total} 章` : loading ? "加载章节中" : "暂无章节"));
  if (result.latest_chapter) meta.appendChild(resultBadge(result.latest_chapter));
  if (result.source_count > 1) meta.appendChild(resultBadge(`${result.source_count} 个来源`));
  summary.appendChild(meta);

  if (bookTags.length) {
    const tagsNode = document.createElement("div");
    tagsNode.className = "detail-book-tags";
    tagsNode.setAttribute("aria-label", "原站标签");
    const tagsLabel = document.createElement("span");
    tagsLabel.className = "detail-book-tags-label";
    tagsLabel.textContent = "原站标签";
    tagsNode.appendChild(tagsLabel);
    bookTags.forEach((tagText) => {
      const tagNode = document.createElement("button");
      tagNode.type = "button";
      tagNode.className = "detail-book-tag";
      tagNode.textContent = tagText;
      tagNode.title = `按原站标签搜索：${tagText}`;
      tagNode.setAttribute("aria-label", `按原站标签搜索 ${tagText}`);
      tagNode.addEventListener("click", () => searchDetailTag(tagText, activeVariant.site));
      tagsNode.appendChild(tagNode);
    });
    summary.appendChild(tagsNode);
  }

  if (variants.length > 1) {
    const switchWrap = document.createElement("div");
    switchWrap.className = "detail-source-switch";
    variants.forEach((item) => {
      const canDownload = sourceSupportsDownload(item.site);
      const button = document.createElement("button");
      button.type = "button"; button.className = "detail-source-button";
      if (detailKey(item) === detailKey(activeVariant)) button.classList.add("is-active");
      button.textContent = `${sourceLabel(item.site)}${canDownload ? "" : "（原站）"}`;
      button.title = canDownload ? `切换到${sourceLabel(item.site)}` : `在${sourceLabel(item.site)}打开`;
      button.addEventListener("click", () => openResultVariant(result, item));
      switchWrap.appendChild(button);
    });
    summary.appendChild(switchWrap);
  }

  const actions = document.createElement("div");
  actions.className = "detail-actions";

  const continueButton = document.createElement("button");
  continueButton.type = "button";
  continueButton.className = "download-button detail-continue-button";
  continueButton.textContent = "继续阅读";
  continueButton.title = "从上次阅读进度继续";
  continueButton.addEventListener("click", () => void continueReadingFromDetail(result, activeVariant, detail, continueButton));
  actions.appendChild(continueButton);

  const cacheButton = document.createElement("button");
  cacheButton.type = "button";
  cacheButton.className = "tool-button";
  cacheButton.textContent = "缓存";
  cacheButton.title = "缓存（下载）到服务器本地";
  const taskTitle = title;
  const taskCoverURL = (book && book.cover_url) || result.cover_url || activeVariant.cover_url || "";
  cacheButton.addEventListener("click", () => void handleCacheButtonClick(
    cacheButton,
    activeVariant.site,
    activeVariant.book_id,
    { title: taskTitle, coverURL: taskCoverURL },
  ));
  actions.appendChild(cacheButton);
  void refreshCacheButtonState(cacheButton, activeVariant.site, activeVariant.book_id);

  const exportButton = document.createElement("button");
  exportButton.type = "button";
  exportButton.className = "tool-button";
  exportButton.textContent = "导出";
  exportButton.title = "导出已下载到服务器本地的文件";
  exportButton.addEventListener("click", () => void startDownloadTask({
    site: activeVariant.site,
    book_id: activeVariant.book_id,
    title: taskTitle,
    cover_url: taskCoverURL,
  }, exportButton, { target: "export" }));
  actions.appendChild(exportButton);

  const addShelfButton = document.createElement("button");
  addShelfButton.type = "button";
  addShelfButton.className = "tool-button";
  addShelfButton.textContent = "加入书架";
  addShelfButton.addEventListener("click", () => void toggleCurrentDetailBookshelf(result, activeVariant, detail, addShelfButton));
  actions.appendChild(addShelfButton);
  void syncDetailShelfState(addShelfButton, activeVariant.site, activeVariant.book_id);

  const similarForm = document.createElement("form");
  similarForm.className = "detail-similar-search";
  similarForm.setAttribute("aria-label", "搜索相似书名");
  const similarLabel = document.createElement("span");
  similarLabel.className = "detail-similar-label";
  similarLabel.textContent = "找相似";
  const similarInput = document.createElement("input");
  similarInput.type = "search";
  similarInput.className = "detail-similar-input";
  similarInput.value = title;
  similarInput.placeholder = "输入书名，检索其他版本";
  similarInput.setAttribute("aria-label", "搜索相似书名的书");
  const similarButton = document.createElement("button");
  similarButton.type = "submit";
  similarButton.className = "detail-similar-button";
  similarButton.textContent = "搜相似";
  similarButton.title = "用当前书名到所有渠道搜索相似的书";
  similarForm.append(similarLabel, similarInput, similarButton);
  similarForm.addEventListener("submit", (event) => {
    event.preventDefault();
    const query = similarInput.value.trim();
    if (!query) return setStatus("请输入要搜索的书名。");
    keywordInput.value = query;
    appState.page = 1;
    void performSearch(false, { sites: Array.from(appState.selectedSites), tag: "" });
  });
  summary.appendChild(actions);
  summary.appendChild(similarForm);

  hero.appendChild(summary); detailContentNode.appendChild(hero);

  const introSection = document.createElement("section");
  introSection.className = "detail-section";
  const introHead = document.createElement("div"); introHead.className = "detail-section-head";
  const introTitle = document.createElement("h3"); introTitle.textContent = "小说简介";
  introHead.appendChild(introTitle); introSection.appendChild(introHead);

  const introBody = document.createElement("p");
  introBody.className = "detail-description";
  introBody.textContent = (errorMessage && !description.trim()) ? `详情加载失败：${errorMessage}` : description;
  introSection.appendChild(introBody); detailContentNode.appendChild(introSection);

  const chapterSection = document.createElement("section");
  chapterSection.className = "detail-section";
  const chapterHead = document.createElement("div"); chapterHead.className = "detail-section-head";
  const chapterTitle = document.createElement("h3"); chapterTitle.textContent = "章节";
  chapterHead.appendChild(chapterTitle);
  if (chapters.length) {
    const controlsGroup = document.createElement("div");
    controlsGroup.className = "detail-controls-group";
    controlsGroup.appendChild(createChapterColumnsControl(() => {
      renderDetail(result, activeVariant, detail, false, "");
    }));
    controlsGroup.appendChild(createChapterPageSizeControl(() => {
      openDetailPage(result, activeVariant, 1, false);
    }));
    chapterHead.appendChild(controlsGroup);
  }
  chapterSection.appendChild(chapterHead);

  if (loading) chapterSection.appendChild(createEmptyInline("正在加载章节列表..."));
  else if (errorMessage) chapterSection.appendChild(createEmptyInline(`详情加载失败：${errorMessage}`));
  else if (!chapters.length) chapterSection.appendChild(createEmptyInline("当前源没有返回章节列表。"));
  else {
    if (chapterPage.total > chapterPage.page_size) chapterSection.appendChild(createChapterPaging(result, activeVariant, chapterPage));
    chapterSection.appendChild(renderChapterList(result, activeVariant, chapters, chapterOffset, chapterPage));
    if (chapterPage.total > chapterPage.page_size) chapterSection.appendChild(createChapterPaging(result, activeVariant, chapterPage));
  }

  detailContentNode.appendChild(chapterSection);
}

function renderChapterList(result, variant, chapters, chapterOffset = 0, chapterPage = null) {
  const container = document.createElement("div");
  container.className = "chapter-list-shell";
  const list = document.createElement("div");
  list.className = `chapter-list cols-${appState.chapterColumns || "auto"}`;
  const sentinel = document.createElement("div");
  sentinel.className = "chapter-sentinel";
  const pageSize = normalizedChapterPageSize();
  let rendered = 0;
  let lastVolume = "";
  let observer = null;

  function appendBatch() {
    const nextEnd = Math.min(rendered + pageSize, chapters.length);
    for (let i = rendered; i < nextEnd; i++) {
      const chapter = chapters[i];
      if (chapter.volume && chapter.volume !== lastVolume) {
        lastVolume = chapter.volume;
        const volume = document.createElement("div");
        volume.className = "chapter-volume"; volume.textContent = chapter.volume;
        list.appendChild(volume);
      }
      const item = document.createElement("div");
      item.className = "chapter-item is-clickable";
      const number = document.createElement("span");
      number.className = "chapter-index"; number.textContent = String(chapter.order || chapterOffset + i + 1);
      const content = document.createElement("div");
      const title = document.createElement("span");
      title.className = "chapter-title"; title.textContent = chapter.title || `第 ${chapterOffset + i + 1} 章`;
      content.appendChild(title);
      item.appendChild(number); item.appendChild(content);
      if (chapter.locked) {
        item.classList.add("is-locked");
        const badge = document.createElement("span");
        badge.className = "chapter-locked-badge";
        badge.textContent = "付费";
        badge.title = "付费章节，需要登录账号具备相应的购买或会员权限";
        item.appendChild(badge);
      }
      item.addEventListener("click", () => {
        void openReaderFromDetail(result, variant, chapter, chapterOffset + i, chapterPage, chapters);
      });
      list.appendChild(item);
    }
    rendered = nextEnd;
    sentinel.textContent = rendered >= chapters.length ? `已加载全部 ${chapters.length} 章` : `已加载 ${rendered}/${chapters.length}，继续向下滚动自动加载`;
    if (rendered >= chapters.length && observer) observer.disconnect();
  }

  appendBatch();
  container.appendChild(list);
  container.appendChild(sentinel);
  if ("IntersectionObserver" in window && rendered < chapters.length) {
    const scrollRoot = detailContentNode.closest(".detail-panel") || null;
    observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) appendBatch();
    }, { root: scrollRoot, rootMargin: "600px 0px" });
    observer.observe(sentinel);
  } else {
    while (rendered < chapters.length) appendBatch();
  }
  return container;
}

function createChapterPaging(result, variant, chapterPage) {
  const pageSize = chapterPage.page_size || normalizedChapterPageSize();
  const total = chapterPage.total || 0;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const page = Math.min(Math.max(1, chapterPage.page || 1), totalPages);
  const wrap = document.createElement("div");
  wrap.className = "chapter-paging";

  const prev = document.createElement("button");
  prev.type = "button"; prev.className = "page-button"; prev.textContent = "上一页";
  prev.disabled = !chapterPage.has_prev;
  prev.addEventListener("click", () => {
    if (!prev.disabled) openDetailPage(result, variant, page - 1, false);
  });

  const indicator = document.createElement("span");
  indicator.className = "page-indicator";
  indicator.textContent = `第 ${page}/${totalPages} 页 · 共 ${total} 章`;

  const next = document.createElement("button");
  next.type = "button"; next.className = "page-button"; next.textContent = "下一页";
  next.disabled = !chapterPage.has_next;
  next.addEventListener("click", () => {
    if (!next.disabled) openDetailPage(result, variant, page + 1, false);
  });

  wrap.append(prev, indicator, next);
  return wrap;
}

async function openReaderFromDetail(result, variant, clickedChapter, chapterIndex, chapterPage, pageChapters) {
  const activeVariant = variant || (result && result.primary) || appState.activeDetailVariant;
  const fallbackChapters = Array.isArray(pageChapters) ? pageChapters : [];
  const localIndex = Math.max(0, fallbackChapters.indexOf(clickedChapter));
  if (!activeVariant || !activeVariant.site || !activeVariant.book_id) {
    if (fallbackChapters.length) openReader(fallbackChapters, localIndex);
    return;
  }

  const detailTitle = displayDetailTitle(result, activeVariant, result && result.primary);
  const detailBook = (result && result.primary) || activeVariant || {};
  setupReaderTracker(activeVariant.site, activeVariant.book_id, detailTitle, {
    author: displayDetailAuthor(result, activeVariant, detailBook),
    coverURL: (detailBook.cover_url) || (result && result.cover_url) || "",
    description: (detailBook.description) || (result && result.description) || "",
    latestChapter: (result && result.latest_chapter) || (detailBook.latest_chapter) || "",
    sourceURL: displayDetailURL(result, activeVariant, detailBook),
  });

  try {
    const expectedTotal = Number(chapterPage && chapterPage.total) || fallbackChapters.length;
    if (expectedTotal > fallbackChapters.length) setStatus("正在加载完整目录…");
    const chapters = await loadReaderCatalog(activeVariant, chapterPage, fallbackChapters);
    const readerIndex = resolveReaderChapterIndex(chapters, clickedChapter, chapterIndex);
    openReader(chapters, readerIndex);
  } catch (error) {
    setStatus("完整目录加载失败，已先打开当前分页。", "warning");
    if (fallbackChapters.length) openReader(fallbackChapters, localIndex);
  }
}

async function loadReaderCatalog(variant, chapterPage, currentChapters) {
  const key = readerCatalogKey(variant);
  const cached = appState.readerCatalogCache.get(key);
  if (cached && cached.length) return cached;

  let pending = appState.readerCatalogPending.get(key);
  if (!pending) {
    pending = fetchCompleteReaderCatalog(variant, chapterPage, currentChapters).then((chapters) => {
      if (chapters.length) appState.readerCatalogCache.set(key, chapters);
      return chapters;
    }).finally(() => {
      appState.readerCatalogPending.delete(key);
    });
    appState.readerCatalogPending.set(key, pending);
  }
  return pending;
}

async function fetchCompleteReaderCatalog(variant, chapterPage, currentChapters) {
  const fallback = Array.isArray(currentChapters) ? currentChapters.slice() : [];
  const currentTotal = Number(chapterPage && chapterPage.total) || fallback.length;
  if (fallback.length && currentTotal <= fallback.length && !(chapterPage && (chapterPage.has_prev || chapterPage.has_next))) return fallback;

  const pageSize = 500;
  const firstDetail = await fetchDetailPageForCatalog(variant, 1, pageSize);
  if (!firstDetail || !firstDetail.book) return fallback;
  const firstPage = normalizeChapterPage(firstDetail.book, firstDetail.chapterPage);
  const total = Math.max(firstPage.total || 0, currentTotal, fallback.length);
  if (total <= 0) return fallback;

  const chapters = new Array(total);
  mergeCatalogPage(chapters, firstDetail.book && firstDetail.book.chapters, 0);
  if (chapterPage && fallback.length) {
    mergeCatalogPage(chapters, fallback, Math.max(0, ((chapterPage.page || 1) - 1) * (chapterPage.page_size || fallback.length)));
  }

  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const pages = [];
  for (let page = 2; page <= totalPages; page++) pages.push(page);
  let cursor = 0;
  async function worker() {
    while (cursor < pages.length) {
      const page = pages[cursor++];
      const detail = await fetchDetailPageForCatalog(variant, page, pageSize);
      if (!detail || !detail.book) continue;
      const pageInfo = normalizeChapterPage(detail.book, detail.chapterPage);
      mergeCatalogPage(chapters, detail.book && detail.book.chapters, Math.max(0, (pageInfo.page - 1) * pageInfo.page_size));
    }
  }
  await Promise.all(Array.from({ length: Math.min(4, pages.length) }, worker));

  const compact = chapters.filter(Boolean);
  return compact.length === total ? chapters : compact.length > fallback.length ? compact : fallback;
}

async function fetchDetailPageForCatalog(variant, chapterPage, chapterPageSize) {
  const cacheKey = detailRequestKey(variant, chapterPage, chapterPageSize);
  const cached = appState.detailCache.get(cacheKey);
  if (cached) return cached;
  let pending = appState.detailPending.get(cacheKey);
  if (!pending) {
    pending = fetchBookDetail(variant, chapterPage, chapterPageSize, { local: Boolean(variant.local) }).then((detail) => {
      if (detail && detail.book) appState.detailCache.set(cacheKey, detail);
      return detail;
    }).finally(() => {
      appState.detailPending.delete(cacheKey);
    });
    appState.detailPending.set(cacheKey, pending);
  }
  return pending;
}

function mergeCatalogPage(target, chapters, offset) {
  if (!Array.isArray(target) || !Array.isArray(chapters)) return;
  chapters.forEach((chapter, index) => {
    if (chapter) target[offset + index] = chapter;
  });
}

function resolveReaderChapterIndex(chapters, clickedChapter, fallbackIndex) {
  if (!Array.isArray(chapters) || !chapters.length) return 0;
  const safeFallback = Math.min(Math.max(0, Number(fallbackIndex) || 0), chapters.length - 1);
  if (sameChapterIdentity(chapters[safeFallback], clickedChapter)) return safeFallback;
  const matched = chapters.findIndex((chapter) => sameChapterIdentity(chapter, clickedChapter));
  return matched >= 0 ? matched : safeFallback;
}

function sameChapterIdentity(left, right) {
  if (!left || !right) return false;
  if (left.id && right.id && left.id === right.id) return true;
  if (left.url && right.url && left.url === right.url) return true;
  return Boolean(left.title && right.title && left.title === right.title && left.order && right.order && left.order === right.order);
}

function createChapterPageSizeControl(onChange) {
  const wrap = document.createElement("label");
  wrap.className = "chapter-control-item";
  const text = document.createElement("span");
  text.textContent = "每页章节";
  const select = document.createElement("select");
  select.className = "chapter-control-select";
  [50, 100, 200, 500].forEach((size) => {
    const option = document.createElement("option");
    option.value = String(size);
    option.textContent = `${size} 章`;
    if (size === appState.chapterPageSize) option.selected = true;
    select.appendChild(option);
  });
  select.addEventListener("change", () => {
    appState.chapterPageSize = Number.parseInt(select.value, 10) || 100;
    localStorage.setItem("chapter-page-size", String(appState.chapterPageSize));
    onChange();
  });
  wrap.append(text, select);
  return wrap;
}

function createChapterColumnsControl(onChange) {
  const wrap = document.createElement("label");
  wrap.className = "chapter-control-item";
  const text = document.createElement("span");
  text.textContent = "排版格式";
  const select = document.createElement("select");
  select.className = "chapter-control-select";
  [
    { label: "自动适配", value: "auto" },
    { label: "1 列 (纯列表)", value: "1" },
    { label: "2 列", value: "2" },
    { label: "3 列", value: "3" },
    { label: "4 列", value: "4" },
    { label: "5 列", value: "5" },
  ].forEach((opt) => {
    const option = document.createElement("option");
    option.value = opt.value;
    option.textContent = opt.label;
    if (opt.value === appState.chapterColumns) option.selected = true;
    select.appendChild(option);
  });
  select.addEventListener("change", () => {
    appState.chapterColumns = select.value;
    localStorage.setItem("chapter-columns", select.value);
    onChange();
  });
  wrap.append(text, select);
  return wrap;
}

function loadChapterPageSize() {
  const value = Number.parseInt(localStorage.getItem("chapter-page-size") || "", 10);
  return [50, 100, 200, 500].includes(value) ? value : 100;
}

function loadChapterColumns() {
  const value = localStorage.getItem("chapter-columns");
  return ["auto", "1", "2", "3", "4", "5"].includes(value) ? value : "auto";
}

function normalizedChapterPageSize() {
  return [50, 100, 200, 500].includes(appState.chapterPageSize) ? appState.chapterPageSize : 100;
}

function normalizeChapterPage(book, chapterPage) {
  const pageSize = [50, 100, 200, 500].includes(Number(chapterPage && chapterPage.page_size)) ? Number(chapterPage.page_size) : normalizedChapterPageSize();
  const total = Math.max(0, Number(chapterPage && chapterPage.total) || (Array.isArray(book && book.chapters) ? book.chapters.length : 0));
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const page = Math.min(Math.max(1, Number(chapterPage && chapterPage.page) || appState.activeDetailPage || 1), totalPages);
  return {
    page,
    page_size: pageSize,
    total,
    has_prev: Boolean(chapterPage && chapterPage.has_prev) || page > 1,
    has_next: Boolean(chapterPage && chapterPage.has_next) || page < totalPages,
  };
}

function isActiveDetailRequest(result, variant, chapterPage, chapterPageSize) {
  return appState.detailResult === result
    && appState.activeDetailKey === detailKey(variant)
    && appState.activeDetailPage === chapterPage
    && appState.activeDetailPageSize === chapterPageSize;
}

// ===== Chapter Reader =====
const readerOverlay = document.getElementById("readerOverlay");
const readerDetailButton = document.getElementById("readerDetailButton");
const readerCatalogButton = document.getElementById("readerCatalogButton");
const readerCatalogCloseButton = document.getElementById("readerCatalogCloseButton");
const readerCatalogPanel = document.getElementById("readerCatalogPanel");
const readerCatalogList = document.getElementById("readerCatalogList");
const readerTitle = document.getElementById("readerTitle");
const readerContent = document.getElementById("readerContent");
const readerBody = document.getElementById("readerBody");
let readerPageViewport = document.getElementById("readerPageViewport");
const readerPageMeter = document.getElementById("readerPageMeter");
const readerProgress = document.getElementById("readerProgress");
const readerTopbar = document.querySelector(".reader-topbar");
const readerTransitionLayer = document.getElementById("readerTransitionLayer");
const readerTransitionKicker = document.getElementById("readerTransitionKicker");
const readerTransitionPosition = document.getElementById("readerTransitionPosition");
const readerTransitionTitle = document.getElementById("readerTransitionTitle");
const readerTransitionStatus = document.getElementById("readerTransitionStatus");

function ensureReaderPageViewport() {
  const existing = document.getElementById("readerPageViewport");
  if (existing) {
    readerPageViewport = existing;
    return existing;
  }
  const body = document.getElementById("readerBody") || readerBody;
  const content = document.getElementById("readerContent") || readerContent;
  if (!body || !content) return null;

  const viewport = document.createElement("div");
  viewport.id = "readerPageViewport";
  viewport.className = "reader-page-viewport";
  body.insertBefore(viewport, content.parentElement === body ? content : null);
  if (content.parentElement !== viewport) viewport.appendChild(content);
  readerPageViewport = viewport;
  return viewport;
}

readerPageViewport = ensureReaderPageViewport();

const READER_SESSION_KEY = "novel-dl:reader-session";
const READER_CONTENT_CACHE_KEY = "novel-dl:reader-content-cache:v9";
const READER_CONTENT_CACHE_LIMIT = 32;
const READER_CONTENT_CACHE_MAX_CHARS = 2000000;
const READER_PAGE_SWIPE_THRESHOLD = 52;
const READER_CHAPTER_FETCH_ATTEMPTS = 2;
const READER_CHAPTER_RETRY_BASE_MS = 360;
const READER_PREFETCH_RETRY_COOLDOWN_MS = 2500;
const READER_TRANSITION_SETTLE_MS = 260;
const READER_TRANSITION_CACHED_MS = 120;
const READER_TRANSITION_EXIT_MS = 360;
const readerState = {
  chapters: [],
  currentIndex: 0,
  transitioning: false,
  token: 0,
  pending: new Map(),
  contentCache: loadReaderContentCache(),
  prefetched: new Set(),
  prefetchBlockedUntil: new Map(),
  pageIndex: 0,
  pageCount: 1,
  pageWidth: 0,
  pinLastPage: false,
  pageTurnToken: 0,
  touchStartX: null,
  touchStartY: null,
  touchLastX: null,
  touchStartedAt: 0,
  touchDragging: false,
  lastWheelTurnAt: 0,
};
let readerSessionPersistTimer = 0;
let readerControlsHideTimer = 0;
let readerPageLayoutFrame = 0;
let readerContentCachePersistTimer = 0;

function readAppURLState() {
  const params = new URLSearchParams(window.location.search);
  let view = String(params.get("view") || "").trim().toLowerCase();
  if (!["search", "discover", "favorites", "bookshelf", "history", "tasks", "detail", "reader", "settings"].includes(view)) {
    if (params.get("chapter_id") || params.get("chapter_url")) view = "reader";
    else if (params.get("book_id")) view = "detail";
    else if (params.get("q")) view = "search";
    else view = "";
  }
  return { params, view };
}

function setAppURLState(state) {
  if (appURLRestoring) return;
  const url = new URL(window.location.href);
  APP_URL_KEYS.forEach((key) => url.searchParams.delete(key));
  Object.entries(state).forEach(([key, value]) => {
    if (!APP_URL_KEYS.includes(key)) return;
    const text = value === null || value === undefined ? "" : String(value).trim();
    if (text) url.searchParams.set(key, text);
  });
  const next = `${url.pathname}${url.search}${url.hash}`;
  const current = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  if (next === current) return;
  window.history.replaceState(window.history.state, "", next);
}

function syncAppURL() {
  if (appURLRestoring) return;
  const readerSnapshot = readerOverlay && !readerOverlay.hidden ? currentReaderSnapshot() : null;
  if (readerSnapshot) {
    setAppURLState({
      view: "reader",
      site: readerSnapshot.site,
      book_id: readerSnapshot.book_id,
      chapter_id: readerSnapshot.chapter_id,
      chapter_url: readerSnapshot.chapter_id ? "" : readerSnapshot.chapter_url,
      chapter_index: readerSnapshot.chapter_index,
      page: readerSnapshot.page_index + 1,
      title: readerSnapshot.title,
      local: readerSnapshot.local_only ? "1" : "",
    });
    return;
  }

  if (siteConfigOverlay && !siteConfigOverlay.hidden) {
    setAppURLState({
      view: "settings",
      panel: settingsActivePanel,
      site: settingsActivePanel === "site" && siteConfigKeyNode ? siteConfigKeyNode.value : "",
    });
    return;
  }

  const detailVariant = appState.activeDetailVariant;
  if (detailOverlay && !detailOverlay.hidden && detailVariant && detailVariant.site && detailVariant.book_id) {
    setAppURLState({
      view: "detail",
      site: detailVariant.site,
      book_id: detailVariant.book_id,
      chapter_page: appState.activeDetailPage || 1,
      chapter_size: appState.activeDetailPageSize || normalizedChapterPageSize(),
      local: detailVariant.local ? "1" : "",
    });
    return;
  }

  if (appState.activeTab === "discover") {
    const discover = appState.discover;
    setAppURLState({
      view: "discover",
      section: discover.section || "trending",
      site: discover.activeSite,
      list: discover.activeList ? discover.activeList.id : "",
      page: discover.activeList && discover.activePage > 1 ? discover.activePage : "",
    });
    return;
  }

  if (appState.activeTab === "favorites") {
    const state = { view: appState.activeSubTab || "bookshelf" };
    if (state.view === "bookshelf") {
      state.folder = appState.bookshelf.parentId || "";
      state.page = appState.bookshelf.page || 1;
    } else if (state.view === "history") {
      state.page = appState.history.page || 1;
    } else {
      state.page = appState.tasksPage || 1;
    }
    setAppURLState(state);
    return;
  }

  const searchState = { view: "search" };
  if (appState.lastKeyword) {
    searchState.q = appState.lastKeyword;
    searchState.mode = appState.lastExact ? "exact" : "fuzzy";
    searchState.page = appState.page || 1;
    searchState.sites = (appState.lastSearchSites.length
      ? appState.lastSearchSites
      : Array.from(appState.selectedSites)).join(",");
    searchState.tags = Array.from(appState.selectedSourceTags).join(",");
    if (appState.lastTagSearch) searchState.tag_search = "1";
  }
  setAppURLState(searchState);
}

function scheduleAppURLSync() {
  if (appURLRestoring) return;
  if (appURLSyncTimer) window.clearTimeout(appURLSyncTimer);
  appURLSyncTimer = window.setTimeout(() => {
    appURLSyncTimer = 0;
    syncAppURL();
  }, 80);
}

function urlPositiveInt(params, key, fallback = 1) {
  const value = Number.parseInt(String(params.get(key) || ""), 10);
  return Number.isFinite(value) && value > 0 ? value : fallback;
}

function urlVariant(params) {
  const site = String(params.get("site") || "").trim();
  const bookID = String(params.get("book_id") || "").trim();
  if (!site || !bookID) return null;
  return {
    site,
    book_id: bookID,
    local: params.get("local") === "1",
  };
}

function syntheticDetailResult(variant, book = {}) {
  return {
    title: book.title || "",
    author: book.author || "",
    cover_url: book.cover_url || "",
    description: book.description || "",
    latest_chapter: book.latest_chapter || "",
    url: book.source_url || "",
    primary: variant,
    sources: [variant],
    source_count: 1,
  };
}

async function restoreSearchFromURL(params) {
  const keyword = String(params.get("q") || "").trim();
  const siteKeys = String(params.get("sites") || "").split(",").map((item) => item.trim()).filter((item) => sourceKeySet.has(item));
  const tags = String(params.get("tags") || "").split(",").map((item) => item.trim()).filter((item) => sourceTagSet.has(item));
  if (siteKeys.length) appState.selectedSites = new Set(siteKeys);
  if (tags.length) appState.selectedSourceTags = new Set(tags);
  renderSourceTagFilters();
  renderSourceSelector();
  keywordInput.value = keyword;
  const exact = params.get("mode") === "exact";
  const tagSearch = params.get("tag_search") === "1" && siteKeys.length === 1;
  if (searchModeSelect) searchModeSelect.value = exact ? "exact" : "fuzzy";
  appState.page = urlPositiveInt(params, "page", 1);
  if (!keyword) {
    activateTab("search", { syncURL: false });
    return;
  }
  await performSearch(exact, {
    sites: siteKeys.length ? siteKeys : Array.from(appState.selectedSites),
    tag: tagSearch ? keyword : "",
  });
}

async function restoreFavoritesFromURL(view, params) {
  const subTab = view === "favorites" ? String(params.get("tab") || "bookshelf") : view;
  activateTab("favorites", { load: false, syncURL: false });
  activateSubTab(subTab, { load: false, syncURL: false });
  if (appState.activeSubTab === "bookshelf") {
    const folder = urlPositiveInt(params, "folder", 0) || null;
    await loadBookshelf(folder, urlPositiveInt(params, "page", 1));
  } else if (appState.activeSubTab === "history") {
    await loadHistory(urlPositiveInt(params, "page", 1));
  } else {
    await hydrateTasksFromServer();
    appState.tasksPage = urlPositiveInt(params, "page", 1);
    renderTasks();
  }
}

async function restoreDiscoverFromURL(params) {
  const section = normalizeDiscoverSection(String(params.get("section") || "trending"));
  activateTab("discover", { load: false, syncURL: false });
  activateDiscoverSection(section, {
    activateTab: false,
    preserveList: false,
    syncURL: false,
  });
  if (section === "trending") {
    await ensureDiscoverTrending();
    return;
  }

  await ensureDiscoverCatalog();
  const requestedSite = String(params.get("site") || "").trim();
  if (requestedSite) {
    appState.discover.activeSite = requestedSite;
  }
  renderDiscoverCatalog();

  const listID = String(params.get("list") || "").trim();
  if (!listID) return;
  const source = appState.discover.catalog.find((item) => item.site === appState.discover.activeSite);
  let list = source && source.lists.find((item) => item.id === listID && item.kind === section);
  if (!list && section === "booklist" && /^booklist:\d+$/.test(listID) && appState.discover.activeSite) {
    const rawID = listID.slice("booklist:".length);
    list = {
      id: listID,
      name: `书单 #${rawID}`,
      description: "正在从原站直接加载书单…",
      kind: "booklist",
      update_frequency: "原站书单",
    };
  }
  if (!list) throw new Error("URL 中的榜单不存在或已下线");
  await openDiscoverList(appState.discover.activeSite, list, urlPositiveInt(params, "page", 1));
}

async function restoreDetailFromURL(params) {
  const variant = urlVariant(params);
  if (!variant) throw new Error("详情链接缺少 site 或 book_id");
  const chapterPage = urlPositiveInt(params, "chapter_page", 1);
  const requestedSize = urlPositiveInt(params, "chapter_size", normalizedChapterPageSize());
  if ([50, 100, 200, 500].includes(requestedSize)) {
    appState.chapterPageSize = requestedSize;
    appState.activeDetailPageSize = requestedSize;
  }
  const detail = await fetchBookDetail(variant, chapterPage, normalizedChapterPageSize(), { local: variant.local });
  if (!detail || !detail.book) throw new Error("详情数据为空");
  appState.detailCache.set(detailRequestKey(variant, chapterPage, normalizedChapterPageSize()), detail);
  openDetailPage(syntheticDetailResult(variant, detail.book), variant, chapterPage, true);
}

async function restoreReaderFromURL(params) {
  const variant = urlVariant(params);
  if (!variant) throw new Error("阅读链接缺少 site 或 book_id");
  const chapterID = String(params.get("chapter_id") || "").trim();
  const chapterURL = String(params.get("chapter_url") || "").trim();
  const item = {
    site: variant.site,
    book_id: variant.book_id,
    title: String(params.get("title") || "").trim(),
    last_read_chapter_id: chapterID || chapterURL,
    last_read_chapter_index: Math.max(0, Number.parseInt(String(params.get("chapter_index") || "0"), 10) || 0),
    last_read_chapter_title: "",
    source_url: "",
    local_only: variant.local,
  };
  const restored = await openBookshelfReader(item, null, {
    readerOptions: { pageIndex: urlPositiveInt(params, "page", 1) - 1 },
  });
  if (!restored) throw new Error("阅读器章节加载失败");
}

async function restoreAppFromLocation() {
  const { params, view } = readAppURLState();
  if (!view) {
    await restoreReaderSession();
    syncAppURL();
    return;
  }

  appURLRestoring = true;
  try {
    if (view === "search") {
      await restoreSearchFromURL(params);
    } else if (view === "discover") {
      await restoreDiscoverFromURL(params);
    } else if (["favorites", "bookshelf", "history", "tasks"].includes(view)) {
      await restoreFavoritesFromURL(view, params);
    } else if (view === "detail") {
      await restoreDetailFromURL(params);
    } else if (view === "reader") {
      await restoreReaderFromURL(params);
    } else if (view === "settings") {
      await openSiteConfig({ panel: params.get("panel") || undefined, siteKey: params.get("site") || "" });
    }
  } catch (error) {
    activateTab("search", { syncURL: false });
    setStatus(`URL 状态恢复失败：${error.message}`, "error");
  } finally {
    appURLRestoring = false;
    syncAppURL();
  }
}

function openReader(chapters, index, options = {}) {
  const pageViewport = ensureReaderPageViewport();
  if (!readerOverlay || !readerContent || !pageViewport) {
    showToast("阅读器加载失败", "页面资源版本不一致，请刷新页面后重试。", "error");
    return;
  }
  readerState.chapters = chapters;
  readerState.currentIndex = index;
  readerState.transitioning = false;
  readerState.prefetched.clear();
  readerState.prefetchBlockedUntil.clear();
  readerState.pending.clear();
  readerState.pageIndex = 0;
  readerState.pageCount = 1;
  readerState.pinLastPage = false;
  resetReaderSwipe();
  hideReaderTransition();
  pageViewport.classList.remove(
    "is-reader-leaving-next",
    "is-reader-leaving-previous",
    "is-reader-entering-next",
    "is-reader-entering-previous",
  );
  const startPage = Number.isFinite(Number(options.pageIndex)) && Number(options.pageIndex) >= 0
    ? Math.floor(Number(options.pageIndex))
    : 0;
  const token = ++readerState.token;
  readerOverlay.hidden = false;
  scheduleAppURLSync();
  setReaderControlsVisible(false);
  hideReaderCatalog();

  document.body.classList.add("has-overlay");
  readerContent.innerHTML = "";
  setReaderScrollLocked(true);
  readerContent.style.transform = "translate3d(0, 0, 0)";
  applyReaderSettings(loadReaderSettings());
  updateReaderTitle(index);
  renderReaderWindow(index).then((rendered) => {
    if (!rendered || token !== readerState.token || readerOverlay.hidden) return;
    setReaderPagePosition(startPage, { animate: false });
    prefetchReaderAdjacentChapters(index);
    persistReaderSession();
    requestAnimationFrame(() => {
      if (token !== readerState.token || readerOverlay.hidden) return;
      setReaderScrollLocked(false);
      scheduleReaderPageLayout(startPage);
    });
  });
}

async function renderReaderWindow(centerIndex, options = {}) {
  const ch = readerState.chapters[centerIndex];
  if (!ch) return false;
  const token = Number.isFinite(options.token) ? options.token : readerState.token;

  if (!options.preserveContent) {
    readerContent.innerHTML = "";
    const loading = document.createElement("div");
    loading.className = "reader-loading";
    loading.textContent = "正在加载当前章节...";
    readerContent.appendChild(loading);
  }

  let content = "";
  let loadError = "";
  try {
    content = await fetchChapterContentForReader(ch, centerIndex);
  } catch (error) {
    loadError = error && error.message ? error.message : "加载失败";
  }
  if (token !== readerState.token || readerOverlay.hidden) return false;
  if (loadError && options.failOnError) throw new Error(loadError);

  readerContent.innerHTML = "";
  const divider = document.createElement("div");
  divider.className = "reader-chapter-divider";
  divider.dataset.chapterIndex = centerIndex;
  divider.textContent = ch.title || `第 ${centerIndex + 1} 章`;
  const block = document.createElement("div");
  block.className = "reader-chapter-block";
  block.dataset.chapterIndex = centerIndex;
  readerContent.append(divider, block);
  if (loadError) {
    block.innerHTML = `<div class="reader-error">加载失败：${loadError}</div>`;
  } else {
    renderChapterBlock(block, content);
    if (ch.locked) {
      const notice = document.createElement("div");
      notice.className = "reader-locked-notice";
      notice.textContent = "该章为付费章节，当前仅显示可访问内容。购买并配置登录 Cookie 后才能获取全文。";
      block.prepend(notice);
    }
  }
  if (options.pinLastPage !== undefined) {
    readerState.pinLastPage = Boolean(options.pinLastPage);
  }
  readerState.pageIndex = 0;
  layoutReaderPages({
    pageIndex: readerState.pinLastPage ? Number.MAX_SAFE_INTEGER : 0,
    animate: false,
  });
  return true;
}

function layoutReaderPages(options = {}) {
  if (readerOverlay.hidden || !readerPageViewport || !readerContent) return;
  const width = Math.floor(readerPageViewport.clientWidth);
  const height = Math.floor(readerPageViewport.clientHeight);
  if (width < 40 || height < 40) return;

  readerState.pageWidth = width;
  readerContent.style.width = `${width}px`;
  readerContent.style.height = `${height}px`;
  readerContent.style.columnWidth = `${width}px`;
  readerContent.style.columnGap = "0px";
  readerState.pageCount = Math.max(1, Math.ceil((readerContent.scrollWidth - 0.5) / width));
  const requestedPage = readerState.pinLastPage
    ? Number.MAX_SAFE_INTEGER
    : (Number.isFinite(Number(options.pageIndex))
      ? Math.floor(Number(options.pageIndex))
      : readerState.pageIndex);
  setReaderPagePosition(requestedPage, { animate: Boolean(options.animate) });
}

function scheduleReaderPageLayout(pageIndex = readerState.pageIndex) {
  if (readerPageLayoutFrame) window.cancelAnimationFrame(readerPageLayoutFrame);
  readerPageLayoutFrame = window.requestAnimationFrame(() => {
    readerPageLayoutFrame = 0;
    layoutReaderPages({ pageIndex });
  });
}

function setReaderPagePosition(index, options = {}) {
  const page = Math.min(Math.max(0, Math.floor(Number(index) || 0)), Math.max(0, readerState.pageCount - 1));
  const target = `translate3d(${-page * readerState.pageWidth}px, 0, 0)`;
  const token = ++readerState.pageTurnToken;
  readerState.pageIndex = page;
  updateReaderPageMeter();

  if (options.dragging) {
    readerContent.classList.add("is-page-dragging");
    readerContent.style.transform = target;
  } else if (options.animate && !readerMotionReduced()) {
    readerContent.classList.remove("is-page-dragging");
    window.requestAnimationFrame(() => {
      if (token === readerState.pageTurnToken) readerContent.style.transform = target;
    });
  } else {
    readerContent.classList.add("is-page-dragging");
    readerContent.style.transform = target;
    void readerContent.offsetWidth;
    readerContent.classList.remove("is-page-dragging");
  }

  prefetchReaderAdjacentChapters();
  scheduleReaderSessionPersist();
  scheduleProgressReport(
    readerState.chapters[readerState.currentIndex],
    readerState.currentIndex,
  );
}

function setReaderPageOffset(offset) {
  readerContent.classList.add("is-page-dragging");
  readerContent.style.transform = `translate3d(${offset}px, 0, 0)`;
}

function updateReaderPageMeter() {
  if (!readerPageMeter) return;
  const atBookEnd = readerState.currentIndex === readerState.chapters.length - 1
    && readerState.pageIndex === readerState.pageCount - 1;
  readerPageMeter.textContent = `第 ${readerState.pageIndex + 1} / ${readerState.pageCount} 页${atBookEnd ? " · 全书完" : ""}`;
  readerPageMeter.hidden = false;
}

function scheduleReaderSessionPersist() {
  if (readerSessionPersistTimer) window.clearTimeout(readerSessionPersistTimer);
  readerSessionPersistTimer = window.setTimeout(() => {
    readerSessionPersistTimer = 0;
    persistReaderSession();
  }, 300);
}

function turnReaderPage(direction) {
  if (readerOverlay.hidden || readerState.transitioning || readerState.touchDragging) return false;
  const step = Number(direction) < 0 ? -1 : 1;
  readerState.pinLastPage = false;
  const targetPage = readerState.pageIndex + step;
  setReaderControlsVisible(false);
  if (targetPage >= 0 && targetPage < readerState.pageCount) {
    setReaderPagePosition(targetPage, { animate: true });
    return true;
  }
  if (readerState.currentIndex + step >= 0 && readerState.currentIndex + step < readerState.chapters.length) {
    void transitionReaderChapter(step);
    return true;
  }
  setReaderPagePosition(readerState.pageIndex, { animate: true });
  return false;
}

function setReaderScrollLocked(locked) {
  readerBody.style.overflowY = locked ? "hidden" : "";
}

function closeReader() {
  flushProgressReport();
  hideReaderCatalog();
  setReaderControlsVisible(false);
  resetReaderSwipe();
  hideReaderTransition();
  const pageViewport = ensureReaderPageViewport();
  if (pageViewport) {
    pageViewport.classList.remove(
      "is-reader-leaving-next",
      "is-reader-leaving-previous",
      "is-reader-entering-next",
      "is-reader-entering-previous",
    );
  }
  readerOverlay.hidden = true;
  document.body.classList.remove("has-overlay");
  setReaderScrollLocked(false);
  if (readerPageLayoutFrame) {
    window.cancelAnimationFrame(readerPageLayoutFrame);
    readerPageLayoutFrame = 0;
  }
  if (readerPageMeter) readerPageMeter.hidden = true;
  readerState.transitioning = false;
  readerState.token += 1;
  readerState.pending.clear();
  readerState.prefetched.clear();
  readerState.prefetchBlockedUntil.clear();
  flushReaderContentCache();
  clearReaderSession();
  if (appState.reader) {
    appState.reader.site = "";
    appState.reader.bookID = "";
    appState.reader.title = "";
    appState.reader.author = "";
    appState.reader.coverURL = "";
    appState.reader.description = "";
    appState.reader.latestChapter = "";
    appState.reader.sourceURL = "";
    appState.reader.localOnly = false;
    appState.reader.lastReportedProgressKey = "";
  }
  syncAppURL();
}

function updateReaderTitle(index) {
  const ch = readerState.chapters[index];
  readerState.currentIndex = index;
  readerTitle.textContent = ch ? (ch.title || `第 ${index + 1} 章`) : "";
  readerProgress.textContent = `${index + 1} / ${readerState.chapters.length}`;
  renderReaderCatalogActive(index);
  if (ch) scheduleProgressReport(ch, index);
}

function openReaderDetail() {
  const result = appState.detailResult;
  const variant = appState.activeDetailVariant || (result && result.primary);
  closeReader();
  if (result && variant) openDetail(result, variant);
}

function toggleReaderCatalog() {
  if (!readerCatalogPanel || !readerCatalogButton) return;
  if (readerCatalogPanel.hidden) showReaderCatalog();
  else hideReaderCatalog();
}

function showReaderCatalog() {
  if (!readerCatalogPanel || !readerCatalogList || !readerCatalogButton) return;
  setReaderControlsVisible(true, { autoHideMs: 0 });
  readerCatalogPanel.hidden = false;
  readerCatalogButton.setAttribute("aria-expanded", "true");
  renderReaderCatalog();
}

function hideReaderCatalog() {
  if (readerCatalogPanel) readerCatalogPanel.hidden = true;
  if (readerCatalogButton) readerCatalogButton.setAttribute("aria-expanded", "false");
  if (readerControlsVisible() && !readerOverlay.hidden) scheduleReaderControlsHide();
}

function clearReaderControlsHideTimer() {
  if (!readerControlsHideTimer) return;
  window.clearTimeout(readerControlsHideTimer);
  readerControlsHideTimer = 0;
}

function scheduleReaderControlsHide(delay = 4200) {
  clearReaderControlsHideTimer();
  if (!readerOverlay || readerOverlay.hidden || !readerControlsVisible()) return;
  if (readerCatalogPanel && !readerCatalogPanel.hidden) return;
  const timeout = Number(delay);
  if (!Number.isFinite(timeout) || timeout <= 0) return;
  readerControlsHideTimer = window.setTimeout(() => {
    readerControlsHideTimer = 0;
    if (!readerOverlay.hidden && (!readerCatalogPanel || readerCatalogPanel.hidden)) {
      setReaderControlsVisible(false, { autoHideMs: 0 });
    }
  }, timeout);
}

function setReaderControlsVisible(visible, options = {}) {
  if (!readerOverlay) return;
  const show = Boolean(visible);
  clearReaderControlsHideTimer();
  readerOverlay.classList.toggle("reader-controls-visible", show);
  if (!show) {
    hideReaderCatalog();
    return;
  }
  if (readerCatalogPanel && !readerCatalogPanel.hidden) return;
  const autoHideMs = options.autoHideMs === undefined ? 4200 : Number(options.autoHideMs);
  if (Number.isFinite(autoHideMs) && autoHideMs > 0) scheduleReaderControlsHide(autoHideMs);
}

function readerControlsVisible() {
  return Boolean(readerOverlay && readerOverlay.classList.contains("reader-controls-visible"));
}

function handleReaderBodyClick(event) {
  if (readerOverlay.hidden) return;
  if (event.target.closest("a, button, input, select, textarea, label")) return;
  const rect = readerBody.getBoundingClientRect();
  if (!rect.width || !rect.height) return;
  const x = event.clientX - rect.left;
  if (x <= rect.width * 0.28) {
    turnReaderPage(-1);
    return;
  }
  if (x >= rect.width * 0.72) {
    turnReaderPage(1);
    return;
  }
  setReaderControlsVisible(!readerControlsVisible());
}

function renderReaderCatalog() {
  if (!readerCatalogList) return;
  readerCatalogList.innerHTML = "";
  readerState.chapters.forEach((chapter, index) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "reader-catalog-item";
    if (index === readerState.currentIndex) {
      button.classList.add("is-active");
      button.setAttribute("aria-current", "true");
    }
    button.textContent = `${index + 1}. ${chapter.title || `第 ${index + 1} 章`}`;
    if (chapter.locked) {
      button.classList.add("is-locked");
      button.textContent += "（付费）";
      button.title = "付费章节，需要登录账号具备相应的购买或会员权限";
    }
    button.addEventListener("click", () => jumpReaderToChapter(index));
    readerCatalogList.appendChild(button);
  });
  centerReaderCatalogActive(readerState.currentIndex);
}

function renderReaderCatalogActive(index) {
  if (!readerCatalogList || !readerCatalogPanel || readerCatalogPanel.hidden) return;
  const nodes = readerCatalogList.querySelectorAll(".reader-catalog-item");
  nodes.forEach((node, idx) => {
    const active = idx === index;
    node.classList.toggle("is-active", active);
    if (active) node.setAttribute("aria-current", "true");
    else node.removeAttribute("aria-current");
  });
  centerReaderCatalogActive(index);
}

function centerReaderCatalogActive(index = readerState.currentIndex) {
  if (!readerCatalogList || !readerCatalogPanel || readerCatalogPanel.hidden) return;
  const active = readerCatalogList.querySelectorAll(".reader-catalog-item")[index];
  if (!active) return;
  const scrollActiveIntoView = () => {
    if (!active.isConnected || readerCatalogPanel.hidden) return;
    const listRect = readerCatalogList.getBoundingClientRect();
    const activeRect = active.getBoundingClientRect();
    const target = readerCatalogList.scrollTop
      + (activeRect.top - listRect.top)
      - Math.max(0, (listRect.height - activeRect.height) / 2);
    readerCatalogList.scrollTop = Math.max(0, target);
  };
  if (typeof window.requestAnimationFrame === "function") {
    window.requestAnimationFrame(scrollActiveIntoView);
  } else {
    scrollActiveIntoView();
  }
}

function jumpReaderToChapter(index) {
  if (!readerState.chapters.length) return;
  const next = Math.min(Math.max(0, index), readerState.chapters.length - 1);
  hideReaderCatalog();
  if (next === readerState.currentIndex) return;
  const direction = next > readerState.currentIndex ? 1 : -1;
  void transitionReaderChapter(direction, { targetIndex: next, viaCatalog: true });
}

async function transitionReaderChapter(direction, options = {}) {
  const requestedTarget = Number(options.targetIndex);
  const targetIndex = Number.isFinite(requestedTarget)
    ? Math.floor(requestedTarget)
    : readerState.currentIndex + (Number(direction) < 0 ? -1 : 1);
  const step = targetIndex === readerState.currentIndex
    ? 0
    : (targetIndex > readerState.currentIndex ? 1 : -1);
  const viaCatalog = Boolean(options.viaCatalog);
  if (
    readerState.transitioning
    || readerOverlay.hidden
    || step === 0
    || targetIndex < 0
    || targetIndex >= readerState.chapters.length
  ) return;

  readerState.transitioning = true;
  resetReaderSwipe();
  setReaderPagePosition(readerState.pageIndex, { animate: false });
  setReaderControlsVisible(false);
  hideReaderCatalog();
  flushProgressReport();
  const token = readerState.token;
  const previousIndex = readerState.currentIndex;
  const previousPage = readerState.pageIndex;
  const leavingClass = step > 0 ? "is-reader-leaving-next" : "is-reader-leaving-previous";
  const enteringClass = step > 0 ? "is-reader-entering-next" : "is-reader-entering-previous";
  const targetCached = isReaderChapterCached(targetIndex);

  updateReaderTitle(targetIndex);
  setReaderScrollLocked(true);
  showReaderTransition(step, targetIndex, targetCached, { viaCatalog });
  readerPageViewport.classList.add(leavingClass);
  const startedAt = performance.now();

  try {
    let rendered = false;
    try {
      rendered = await renderReaderWindow(targetIndex, {
        preserveContent: true,
        token,
        failOnError: true,
        pinLastPage: step < 0 && !viaCatalog,
      });
    } catch (error) {
      if (token !== readerState.token || readerOverlay.hidden) return;
      console.warn("reader chapter transition failed", error);
      readerState.prefetchBlockedUntil.set(
        targetIndex,
        performance.now() + READER_PREFETCH_RETRY_COOLDOWN_MS,
      );
      updateReaderTitle(previousIndex);
      setReaderPagePosition(previousPage, { animate: false });
      showToast(
        viaCatalog ? "章节跳转失败" : (step > 0 ? "下一章加载失败" : "上一章加载失败"),
        viaCatalog
          ? "章节请求暂时失败，已保留当前阅读位置，请再次选择章节。"
          : "章节请求暂时失败，已保留当前阅读位置，请再次翻页重试。",
        "warning",
        { durationMs: 5200 },
      );
      return;
    }
    if (!rendered || token !== readerState.token || readerOverlay.hidden) return;
    const minimumWait = readerMotionReduced()
      ? 0
      : (targetCached ? READER_TRANSITION_CACHED_MS : READER_TRANSITION_SETTLE_MS);
    const remainingWait = minimumWait - (performance.now() - startedAt);
    if (remainingWait > 0) await waitForReaderAnimation(remainingWait);
    if (token !== readerState.token || readerOverlay.hidden) return;

    readerPageViewport.classList.remove(leavingClass);
    readerTransitionLayer.classList.add("is-leaving");
    readerPageViewport.classList.add(enteringClass);
    await waitForReaderAnimation(readerMotionReduced() ? 0 : READER_TRANSITION_EXIT_MS);
    if (token !== readerState.token || readerOverlay.hidden) return;

    readerPageViewport.classList.remove(enteringClass);
    hideReaderTransition();
    setReaderScrollLocked(false);
    readerState.transitioning = false;
    persistReaderSession();
    updateReaderPageMeter();
    prefetchReaderAdjacentChapters(targetIndex);
  } finally {
    if (token === readerState.token) {
      readerPageViewport.classList.remove(leavingClass, enteringClass);
      hideReaderTransition();
      setReaderScrollLocked(false);
      readerState.transitioning = false;
    }
  }
}

function showReaderTransition(direction, targetIndex, cached = false, options = {}) {
  if (!readerTransitionLayer) return;
  const target = readerState.chapters[targetIndex];
  if (readerTransitionKicker) {
    readerTransitionKicker.textContent = options.viaCatalog
      ? "跳转章节"
      : (direction > 0 ? "下一章" : "上一章");
  }
  if (readerTransitionPosition) readerTransitionPosition.textContent = `${targetIndex + 1} / ${readerState.chapters.length}`;
  if (readerTransitionTitle) readerTransitionTitle.textContent = (target && target.title) || `第 ${targetIndex + 1} 章`;
  if (readerTransitionStatus) {
    readerTransitionStatus.textContent = cached ? "章节已预加载，正在翻页..." : "正在载入章节...";
  }
  readerTransitionLayer.classList.remove("is-visible", "is-leaving");
  readerTransitionLayer.classList.toggle("is-ready", cached);
  readerTransitionLayer.hidden = false;
  void readerTransitionLayer.offsetWidth;
  readerTransitionLayer.classList.add("is-visible");
}

function hideReaderTransition() {
  if (!readerTransitionLayer) return;
  readerTransitionLayer.classList.remove("is-visible", "is-leaving", "is-ready");
  readerTransitionLayer.hidden = true;
}

function readerMotionReduced() {
  return Boolean(window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches);
}

function waitForReaderAnimation(ms) {
  if (!ms || ms <= 0) return Promise.resolve();
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function renderChapterBlock(block, text) {
  block.innerHTML = "";
  if (!text.trim()) { block.innerHTML = '<div class="reader-error">章节内容为空</div>'; return; }
  text.split(/\n/).forEach(line => {
    if (!line.trim()) return;
    const image = readerImageFromLine(line);
    if (image && image.src) {
      block.appendChild(createReaderImage(image.src, image.alt));
      return;
    }
    const p = document.createElement("p"); p.textContent = line; block.appendChild(p);
  });
}

function readerImageFromLine(line) {
  const value = (line || "").trim();
  if (!value) return null;
  const markdown = value.match(/^!\[([^\]]*)\]\((.+)\)$/);
  if (markdown) return { alt: markdown[1] || "", src: markdown[2].trim() };
  const placeholder = value.match(/^\[(?:\u56fe\u7247|\u5716\u7247|\u63d2\u56fe|\u63d2\u5716|\?\?)\]\s+(\S+)\s*$/);
  if (placeholder) return { alt: "", src: placeholder[1].trim() };
  const htmlImage = value.match(/<img\b[^>]*(?:data-original|data-src|src)=["']([^"']+)["'][^>]*>/i);
  if (htmlImage) return { alt: "", src: htmlImage[1].trim() };
  return null;
}

function createReaderImage(src, alt) {
  const figure = document.createElement("figure");
  figure.className = "reader-image-figure";
  const img = document.createElement("img");
  img.className = "reader-image";
  img.src = src;
  img.alt = alt || "\u7ae0\u8282\u56fe\u7247";
  img.loading = "lazy";
  img.addEventListener("load", () => scheduleReaderPageLayout(), { once: true });
  figure.appendChild(img);
  return figure;
}

async function fetchChapterContentForReader(ch, index) {
  const key = chapterReaderPendingKey(ch, index);
  const cached = getReaderCachedChapterContent(key);
  if (cached !== null) return cached;
  const pending = readerState.pending.get(key);
  if (pending) return pending;
  const variant = appState.activeDetailVariant || (appState.detailResult && appState.detailResult.primary) || {};
  const site = variant.site || ""; const bookID = variant.book_id || "";
  if (!site || !bookID) throw new Error("缺少站点信息");
  const task = fetchChapterContentTextWithRetry(site, bookID, ch)
    .then((content) => {
      readerState.prefetchBlockedUntil.delete(index);
      rememberReaderChapterContent(key, content);
      return content;
    })
    .finally(() => {
      readerState.pending.delete(key);
    });
  readerState.pending.set(key, task);
  return task;
}

function prefetchReaderChapter(index) {
  if (index < 0 || index >= readerState.chapters.length || readerState.prefetched.has(index)) return;
  const blockedUntil = Number(readerState.prefetchBlockedUntil.get(index) || 0);
  if (blockedUntil > performance.now()) return;
  readerState.prefetchBlockedUntil.delete(index);
  const chapter = readerState.chapters[index];
  if (!chapter) return;
  if (isReaderChapterCached(index)) {
    readerState.prefetched.add(index);
    return;
  }
  readerState.prefetched.add(index);
  void fetchChapterContentForReader(chapter, index).catch(() => {
    readerState.prefetched.delete(index);
  });
}

function prefetchReaderAdjacentChapters(index = readerState.currentIndex) {
  prefetchReaderChapter(index - 1);
  prefetchReaderChapter(index + 1);
}

function loadReaderContentCache() {
  const cache = new Map();
  try {
    const raw = window.sessionStorage.getItem(READER_CONTENT_CACHE_KEY);
    const entries = raw ? JSON.parse(raw) : [];
    if (!Array.isArray(entries)) return cache;
    entries.forEach((entry) => {
      if (!Array.isArray(entry) || entry.length !== 2) return;
      const key = String(entry[0] || "");
      const content = entry[1];
      if (key && typeof content === "string" && content.trim()) cache.set(key, content);
    });
    pruneReaderContentCache(cache);
  } catch (_) {
  }
  return cache;
}

function getReaderCachedChapterContent(key) {
  if (!readerState.contentCache.has(key)) return null;
  const content = readerState.contentCache.get(key);
  if (typeof content !== "string" || !content.trim()) {
    readerState.contentCache.delete(key);
    scheduleReaderContentCachePersist();
    return null;
  }
  readerState.contentCache.delete(key);
  readerState.contentCache.set(key, content);
  scheduleReaderContentCachePersist();
  return content;
}

function rememberReaderChapterContent(key, content) {
  if (typeof content !== "string" || !content.trim()) return;
  readerState.contentCache.delete(key);
  readerState.contentCache.set(key, content);
  pruneReaderContentCache(readerState.contentCache);
  scheduleReaderContentCachePersist();
}

function pruneReaderContentCache(cache) {
  let totalChars = 0;
  cache.forEach((content) => {
    totalChars += typeof content === "string" ? content.length : 0;
  });
  while (
    cache.size > READER_CONTENT_CACHE_LIMIT
    || totalChars > READER_CONTENT_CACHE_MAX_CHARS
  ) {
    const oldestKey = cache.keys().next().value;
    if (oldestKey === undefined) break;
    const oldest = cache.get(oldestKey);
    totalChars -= typeof oldest === "string" ? oldest.length : 0;
    cache.delete(oldestKey);
  }
}

function scheduleReaderContentCachePersist() {
  if (readerContentCachePersistTimer) window.clearTimeout(readerContentCachePersistTimer);
  readerContentCachePersistTimer = window.setTimeout(() => {
    readerContentCachePersistTimer = 0;
    persistReaderContentCache();
  }, 250);
}

function persistReaderContentCache() {
  const serialized = JSON.stringify(Array.from(readerState.contentCache.entries()));
  try {
    window.sessionStorage.setItem(READER_CONTENT_CACHE_KEY, serialized);
    return;
  } catch (_) {
  }
  while (readerState.contentCache.size > 0) {
    readerState.contentCache.delete(readerState.contentCache.keys().next().value);
    try {
      window.sessionStorage.setItem(
        READER_CONTENT_CACHE_KEY,
        JSON.stringify(Array.from(readerState.contentCache.entries())),
      );
      return;
    } catch (_) {
    }
  }
  try {
    window.sessionStorage.removeItem(READER_CONTENT_CACHE_KEY);
  } catch (_) {
  }
}

function flushReaderContentCache() {
  if (readerContentCachePersistTimer) {
    window.clearTimeout(readerContentCachePersistTimer);
    readerContentCachePersistTimer = 0;
  }
  persistReaderContentCache();
}

function isReaderChapterCached(index) {
  const chapter = readerState.chapters[index];
  if (!chapter) return false;
  const content = readerState.contentCache.get(chapterReaderPendingKey(chapter, index));
  return typeof content === "string" && Boolean(content.trim());
}

async function fetchChapterContentText(site, bookID, ch) {
  const url = new URL(`${window.location.origin}${root}/api/chapter-content`);
  url.searchParams.set("site", site); url.searchParams.set("book_id", bookID);
  url.searchParams.set("chapter_id", ch.id || ""); url.searchParams.set("title", ch.title || "");
  url.searchParams.set("url", ch.url || "");
  if (appState.reader && appState.reader.localOnly) url.searchParams.set("local", "1");
  const data = await fetchJSONWithTimeout(
    url.toString(),
    {},
    chapterLoadTimeoutMs(site),
    `${sourceLabel(site)} 章节加载超时，请稍后重试或切换来源`,
  );
  const content = (data.chapter && data.chapter.content) || "";
  if (!content.trim()) {
    const error = new Error("章节内容为空");
    error.retryable = true;
    throw error;
  }
  return content;
}

async function fetchChapterContentTextWithRetry(site, bookID, ch) {
  let lastError = null;
  for (let attempt = 0; attempt < READER_CHAPTER_FETCH_ATTEMPTS; attempt += 1) {
    try {
      return await fetchChapterContentText(site, bookID, ch);
    } catch (error) {
      lastError = error;
      if (
        attempt + 1 >= READER_CHAPTER_FETCH_ATTEMPTS
        || !isRetryableReaderRequestError(error)
      ) {
        throw error;
      }
      await waitForReaderAnimation(READER_CHAPTER_RETRY_BASE_MS * (attempt + 1));
    }
  }
  throw lastError || new Error("章节加载失败");
}

function isRetryableReaderRequestError(error) {
  if (!error || error.retryable === false) return false;
  const status = Number(error.status);
  if (Number.isFinite(status) && status >= 400 && status < 500 && status !== 429) {
    return false;
  }
  return true;
}

async function fetchJSONWithTimeout(resource, options, timeoutMs, timeoutMessage) {
  const controller = new AbortController();
  const timer = window.setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await fetch(resource, { ...(options || {}), signal: controller.signal });
    const text = await response.text();
    let data = {};
    if (text) {
      try {
        data = JSON.parse(text);
      } catch (_) {
        throw new Error("接口返回内容不是 JSON");
      }
    }
    if (!response.ok) {
      const error = new Error(data.error || `HTTP ${response.status}`);
      error.status = response.status;
      error.retryable = response.status === 429 || response.status >= 500;
      throw error;
    }
    return data;
  } catch (error) {
    if (error && error.name === "AbortError") {
      const timeoutError = new Error(timeoutMessage || "请求超时");
      timeoutError.retryable = true;
      timeoutError.timedOut = true;
      throw timeoutError;
    }
    throw error;
  } finally {
    window.clearTimeout(timer);
  }
}

function detailLoadTimeoutMs(site) {
  switch (String(site || "").toLowerCase()) {
    case "alicesw":
      return 12000;
    case "aaatxt":
      return 120000;
    case "esjzone":
    case "n8novel":
    case "tongrenshe":
      return 30000;
    case "linovelib":
    case "tianyabooks":
      return 60000;
    default:
      return 18000;
  }
}

function chapterLoadTimeoutMs(site) {
  switch (String(site || "").toLowerCase()) {
    case "alicesw":
      return 12000;
    case "aaatxt":
      return 120000;
    case "n8novel":
    case "esjzone":
      return 30000;
    default:
      return 20000;
  }
}

function shouldWarmupDetail(site) {
  switch (String(site || "").toLowerCase()) {
    case "aaatxt":
      return false;
    default:
      return true;
  }
}

function chapterReaderPendingKey(ch, index) {
  const variant = appState.activeDetailVariant || (appState.detailResult && appState.detailResult.primary) || {};
  const site = (variant && variant.site) || "";
  const bookID = (variant && variant.book_id) || "";
  const sourceMode = appState.reader && appState.reader.localOnly ? "local" : "remote";
  return `${site}::${bookID}::${sourceMode}::${ch.id || ch.url || `${index}:${ch.title || ""}`}`;
}

if (readerDetailButton) readerDetailButton.addEventListener("click", openReaderDetail);
if (readerCatalogButton) readerCatalogButton.addEventListener("click", toggleReaderCatalog);
if (readerCatalogCloseButton) readerCatalogCloseButton.addEventListener("click", hideReaderCatalog);
readerBody.addEventListener("click", handleReaderBodyClick);
if (readerTopbar) {
  readerTopbar.addEventListener("pointerenter", clearReaderControlsHideTimer);
  readerTopbar.addEventListener("pointerleave", () => scheduleReaderControlsHide(1800));
  readerTopbar.addEventListener("focusin", clearReaderControlsHideTimer);
  readerTopbar.addEventListener("focusout", () => scheduleReaderControlsHide(1800));
}

function handleReaderTouchStart(event) {
  if (readerOverlay.hidden || readerState.transitioning || !event.touches || event.touches.length !== 1) return;
  resetReaderSwipe();
  const touch = event.touches[0];
  readerState.touchStartX = touch.clientX;
  readerState.touchStartY = touch.clientY;
  readerState.touchLastX = touch.clientX;
  readerState.touchStartedAt = performance.now();
}

function handleReaderTouchMove(event) {
  if (
    readerOverlay.hidden
    || readerState.transitioning
    || readerState.touchStartX === null
    || readerState.touchStartY === null
    || !event.touches
    || event.touches.length !== 1
  ) return;

  const touch = event.touches[0];
  const deltaX = touch.clientX - readerState.touchStartX;
  const deltaY = touch.clientY - readerState.touchStartY;
  if (!readerState.touchDragging) {
    if (Math.abs(deltaX) < 8 || Math.abs(deltaX) <= Math.abs(deltaY)) return;
    readerState.touchDragging = true;
  }

  if (event.cancelable) event.preventDefault();
  readerState.touchLastX = touch.clientX;
  const baseOffset = -readerState.pageIndex * readerState.pageWidth;
  const minOffset = -(readerState.pageCount - 1) * readerState.pageWidth;
  let offset = baseOffset + deltaX;
  if (offset > 0) offset *= 0.28;
  if (offset < minOffset) offset = minOffset + (offset - minOffset) * 0.28;
  setReaderPageOffset(offset);
}

function handleReaderTouchEnd() {
  if (!readerState.touchDragging) {
    resetReaderSwipe();
    return;
  }
  const deltaX = Number(readerState.touchLastX || 0) - Number(readerState.touchStartX || 0);
  const elapsed = Math.max(1, performance.now() - Number(readerState.touchStartedAt || 0));
  const velocity = Math.abs(deltaX) / elapsed;
  const shouldTurn = Math.abs(deltaX) >= READER_PAGE_SWIPE_THRESHOLD
    || (Math.abs(deltaX) >= 20 && velocity >= 0.45);
  readerState.touchDragging = false;
  if (shouldTurn) turnReaderPage(deltaX < 0 ? 1 : -1);
  else setReaderPagePosition(readerState.pageIndex, { animate: true });
  resetReaderSwipe();
}

function handleReaderWheel(event) {
  if (
    readerOverlay.hidden
    || readerState.transitioning
    || event.ctrlKey
    || (!Number.isFinite(event.deltaX) && !Number.isFinite(event.deltaY))
    || (event.deltaX === 0 && event.deltaY === 0)
  ) return;

  const now = performance.now();
  if (now - readerState.lastWheelTurnAt < 420) return;
  if (event.cancelable) event.preventDefault();
  readerState.lastWheelTurnAt = now;
  const delta = Math.abs(event.deltaX) > Math.abs(event.deltaY) ? event.deltaX : event.deltaY;
  turnReaderPage(delta > 0 ? 1 : -1);
}

function handleReaderKeydown(event) {
  const target = event.target;
  if (
    readerOverlay.hidden
    || readerState.transitioning
    || event.altKey
    || event.ctrlKey
    || event.metaKey
    || (target instanceof Element && target.closest("input, select, textarea"))
  ) return;
  if (event.key === "ArrowLeft" || event.key === "PageUp") {
    event.preventDefault();
    turnReaderPage(-1);
  } else if (event.key === "ArrowRight" || event.key === "PageDown" || event.key === " ") {
    event.preventDefault();
    turnReaderPage(1);
  }
}

function resetReaderSwipe() {
  readerState.touchStartX = null;
  readerState.touchStartY = null;
  readerState.touchLastX = null;
  readerState.touchStartedAt = 0;
  readerState.touchDragging = false;
  if (readerContent) readerContent.classList.remove("is-page-dragging");
}

function handleReaderTouchCancel() {
  if (readerState.touchDragging) setReaderPagePosition(readerState.pageIndex, { animate: true });
  resetReaderSwipe();
}

readerBody.addEventListener("touchstart", handleReaderTouchStart, { passive: true });
readerBody.addEventListener("touchmove", handleReaderTouchMove, { passive: false });
readerBody.addEventListener("touchend", handleReaderTouchEnd);
readerBody.addEventListener("touchcancel", handleReaderTouchCancel);
readerBody.addEventListener("wheel", handleReaderWheel, { passive: false });
window.addEventListener("keydown", handleReaderKeydown);
window.addEventListener("resize", () => scheduleReaderPageLayout());
if (document.fonts && document.fonts.ready) {
  document.fonts.ready.then(() => scheduleReaderPageLayout());
}

function currentReaderSnapshot() {
  const tracker = appState.reader;
  const chapter = readerState.chapters[readerState.currentIndex];
  if (!tracker || !tracker.site || !tracker.bookID || !chapter) return null;
  return {
    site: tracker.site,
    book_id: tracker.bookID,
    title: tracker.title,
    author: tracker.author,
    cover_url: tracker.coverURL,
    description: tracker.description,
    latest_chapter: tracker.latestChapter,
    source_url: tracker.sourceURL,
    local_only: tracker.localOnly,
    chapter_id: String((chapter && (chapter.chapter_id || chapter.id)) || "").trim(),
    chapter_url: String((chapter && chapter.url) || "").trim(),
    chapter_title: (chapter && chapter.title) || "",
    chapter_index: Math.max(0, readerState.currentIndex || 0),
    page_index: Math.max(0, readerState.pageIndex || 0),
    scroll_offset: 0,
  };
}

function persistReaderSession() {
  if (!readerOverlay || readerOverlay.hidden || !readerState.chapters.length) return;
  const snapshot = currentReaderSnapshot();
  if (!snapshot) return;
  try {
    window.sessionStorage.setItem(READER_SESSION_KEY, JSON.stringify(snapshot));
  } catch (_) {
  }
  syncAppURL();
}

function clearReaderSession() {
  try {
    window.sessionStorage.removeItem(READER_SESSION_KEY);
  } catch (_) {
  }
}

function readReaderSession() {
  try {
    const raw = window.sessionStorage.getItem(READER_SESSION_KEY);
    return raw ? JSON.parse(raw) : null;
  } catch (_) {
    return null;
  }
}

function restoreReaderPageFromSession() {
  if (!readerOverlay || readerOverlay.hidden || !readerState.chapters.length) return;
  const saved = readReaderSession();
  if (!saved) return;
  const index = Math.min(Math.max(0, Number(saved.chapter_index) || 0), readerState.chapters.length - 1);
  if (index === readerState.currentIndex) {
    setReaderPagePosition(Math.max(0, Number(saved.page_index) || 0), { animate: false });
  }
}

async function restoreReaderSession() {
  const saved = readReaderSession();
  if (!saved) return;
  if (!readerOverlay || !readerOverlay.hidden || readerState.chapters.length) return;
  const item = {
    site: saved.site,
    book_id: saved.book_id,
    title: saved.title,
    author: saved.author,
    cover_url: saved.cover_url,
    description: saved.description,
    latest_chapter: saved.latest_chapter,
    source_url: saved.source_url,
    last_read_chapter_id: saved.chapter_id || saved.chapter_url,
    last_read_chapter_index: saved.chapter_index,
    last_read_chapter_title: saved.chapter_title,
    local_only: saved.local_only,
  };
  try {
    const restored = await openBookshelfReader(item, null, {
      readerOptions: { pageIndex: Math.max(0, Number(saved.page_index) || 0) },
    });
    if (!restored) clearReaderSession();
  } catch (_) {
    clearReaderSession();
  }
}

document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "hidden") {
    persistReaderSession();
    flushReaderContentCache();
  } else if (document.visibilityState === "visible") {
    restoreReaderPageFromSession();
  }
});
window.addEventListener("pagehide", persistReaderSession);
window.addEventListener("pagehide", flushReaderContentCache);
window.addEventListener("pageshow", (event) => {
  if (event.persisted) restoreReaderPageFromSession();
});

// The same palette is applied to the entire application and the reader.
const appThemes = [
  { key: "paper", label: "米纸", description: "温暖柔和", bg: "#f8f2e6", text: "#4b3f35" },
  { key: "white", label: "白色", description: "清爽明亮", bg: "#ffffff", text: "#243044" },
  { key: "mint", label: "浅青", description: "低饱和绿", bg: "#eefaf5", text: "#243b3f" },
  { key: "blue", label: "浅蓝", description: "安静冷色", bg: "#eef6ff", text: "#27364a" },
  { key: "rose", label: "浅粉", description: "柔和暖色", bg: "#fff1f2", text: "#4a2d35" },
  { key: "night", label: "夜间", description: "深色低亮度", bg: "#000000", text: "#d9e2ec", night: true },
];

function initReaderSettings() {
  const settings = loadReaderSettings();
  applyReaderSettings(settings);
  renderAppearanceThemeOptions(settings);
  const picker = document.getElementById("readerBgPicker");
  if (!picker) return;
  picker.innerHTML = "";

  appThemes.forEach((theme) => {
    const dot = document.createElement("button");
    dot.type = "button";
    dot.className = "reader-bg-dot" + (theme.key === settings.theme ? " is-active" : "");
    dot.style.background = theme.bg;
    dot.title = theme.label;
    dot.setAttribute("aria-label", `阅读背景：${theme.label}`);
    dot.addEventListener("click", () => {
      selectAppTheme(theme.key);
    });
    picker.appendChild(dot);
  });

  const fontWrap = document.createElement("label");
  fontWrap.className = "reader-font-control";
  const fontText = document.createElement("span");
  fontText.textContent = "字号";
  const fontSelect = document.createElement("select");
  fontSelect.className = "reader-setting-select";
  [14, 16, 18, 20, 22, 24].forEach((size) => {
    const option = document.createElement("option");
    option.value = String(size);
    option.textContent = `${size}px`;
    if (size === settings.fontSize) option.selected = true;
    fontSelect.appendChild(option);
  });
  fontSelect.addEventListener("change", () => {
    const next = { ...loadReaderSettings(), fontSize: Number.parseInt(fontSelect.value, 10) || 18 };
    saveReaderSettings(next);
    applyReaderSettings(next);
  });
  fontWrap.append(fontText, fontSelect);
  picker.appendChild(fontWrap);

  const nightButton = document.createElement("button");
  nightButton.type = "button";
  nightButton.className = "reader-night-toggle";
  nightButton.textContent = "夜间";
  nightButton.addEventListener("click", () => {
    const current = loadReaderSettings();
    selectAppTheme(current.theme === "night" ? "paper" : "night");
  });
  picker.appendChild(nightButton);
}

function renderAppearanceThemeOptions(settings) {
  const container = document.getElementById("appearanceThemeOptions");
  if (!container) return;
  container.innerHTML = "";
  appThemes.forEach((theme) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "appearance-theme-button" + (theme.key === settings.theme ? " is-active" : "");
    button.dataset.themeKey = theme.key;
    button.setAttribute("role", "radio");
    button.setAttribute("aria-checked", String(theme.key === settings.theme));
    button.setAttribute("aria-label", `${theme.label}主题：${theme.description}`);

    const swatch = document.createElement("span");
    swatch.className = "appearance-theme-swatch";
    swatch.style.background = theme.bg;

    const copy = document.createElement("span");
    copy.className = "appearance-theme-copy";
    const label = document.createElement("strong");
    label.textContent = theme.label;
    const description = document.createElement("small");
    description.textContent = theme.description;
    copy.append(label, description);
    button.append(swatch, copy);
    button.addEventListener("click", () => selectAppTheme(theme.key));
    container.appendChild(button);
  });
}

function selectAppTheme(themeKey) {
  const current = loadReaderSettings();
  const next = { ...current, theme: themeKey };
  saveReaderSettings(next);
  applyReaderSettings(next);
  renderReaderThemeActive();
}

function renderReaderThemeActive() {
  const settings = loadReaderSettings();
  document.querySelectorAll(".appearance-theme-button").forEach((button) => {
    const active = button.dataset.themeKey === settings.theme;
    button.classList.toggle("is-active", active);
    button.setAttribute("aria-checked", String(active));
  });
  const picker = document.getElementById("readerBgPicker");
  if (!picker) return;
  picker.querySelectorAll(".reader-bg-dot").forEach((dot, index) => {
    dot.classList.toggle("is-active", appThemes[index] && appThemes[index].key === settings.theme);
  });
}

function loadReaderSettings() {
  const fallback = { theme: "paper", fontSize: 18 };
  try {
    const parsed = JSON.parse(localStorage.getItem("reader-settings") || "{}");
    const theme = appThemes.some((item) => item.key === parsed.theme) ? parsed.theme : fallback.theme;
    const fontSize = [14, 16, 18, 20, 22, 24].includes(Number(parsed.fontSize)) ? Number(parsed.fontSize) : fallback.fontSize;
    return { theme, fontSize };
  } catch {
    return fallback;
  }
}

function saveReaderSettings(settings) {
  localStorage.setItem("reader-settings", JSON.stringify(settings));
}

function applyReaderSettings(settings) {
  const theme = appThemes.find((item) => item.key === settings.theme) || appThemes[0];
  document.documentElement.dataset.appTheme = theme.key;
  readerOverlay.style.setProperty("--reader-bg", theme.bg);
  readerOverlay.style.setProperty("--reader-text", theme.text);
  readerContent.style.fontSize = `${settings.fontSize || 18}px`;
  readerOverlay.classList.toggle("is-night", Boolean(theme.night));
  if (!readerOverlay.hidden) scheduleReaderPageLayout();
}

initReaderSettings();

function resultBadge(text) {
  const node = document.createElement("span"); node.className = "result-badge"; node.textContent = text; return node;
}

function renderPaging() {
  resultCountNode.textContent = appState.lastKeyword ? totalLabel(appState.total, appState.totalExact) : "0";
  const pagedCount = appState.results.length;
  const indicatorText = pagedCount > 0
    ? `第 ${appState.page} 页 · 本页 ${pagedCount} 本（每页上限 ${appState.pageSize}）`
    : `第 ${appState.page} 页 · 每页 ${appState.pageSize} 本`;
  pageIndicatorNode.textContent = indicatorText;
  prevPageButton.disabled = !appState.hasPrev; nextPageButton.disabled = !appState.hasNext;

  if (pageIndicatorBottomNode) pageIndicatorBottomNode.textContent = indicatorText;
  if (prevPageBottomButton) prevPageBottomButton.disabled = !appState.hasPrev;
  if (nextPageBottomButton) nextPageBottomButton.disabled = !appState.hasNext;
  if (paginationBottomNode) {
    const showBottom = Boolean(appState.lastKeyword) && pagedCount > 0;
    paginationBottomNode.hidden = !showBottom;
  }
}

function scrollToResultsTop() {
  if (!resultsNode) return;
  const top = resultsNode.getBoundingClientRect().top + window.scrollY - 12;
  window.scrollTo({ top: Math.max(0, top), behavior: "smooth" });
}

function renderResultMeta() {
  if (!appState.lastKeyword) return resultMetaNode.textContent = "输入关键词后开始搜索。";
  const queryLabel = appState.lastTagSearch
    ? `原站标签“${appState.lastTagSearch}”`
    : `关键词“${appState.lastKeyword}”`;
  const modeLabel = appState.lastTagSearch
    ? (appState.lastTagSearchUsed ? " · 原站标签检索" : " · 关键词回退")
    : "";
  if (!appState.results.length) return resultMetaNode.textContent = `${queryLabel}暂无结果${modeLabel}。`;
  const start = (appState.page - 1) * appState.pageSize + 1;
  const end = start + appState.results.length - 1;
  resultMetaNode.textContent = `${queryLabel}当前显示 ${start}-${end}，共 ${totalLabel(appState.total, appState.totalExact)} 条${modeLabel}。`;
}

// bookCached 查询服务器本地是否已缓存该书（轻量接口，只查元数据）。
async function bookCached(site, bookID) {
  if (!site || !bookID) return false;
  try {
    const response = await fetch(`${root}/api/books/cached?site=${encodeURIComponent(site)}&book_id=${encodeURIComponent(bookID)}`);
    if (!response.ok) return false;
    const payload = await response.json();
    return !!payload.cached;
  } catch { return false; }
}

// refreshCacheButtonState 打开详情时刷新「缓存」按钮的已缓存状态。
async function refreshCacheButtonState(button, site, bookID) {
  if (!button) return;
  const cached = await bookCached(site, bookID);
  if (!button.isConnected) return;
  button.textContent = cached ? "已缓存" : "缓存";
  button.dataset.cached = cached ? "1" : "0";
  button.title = cached ? "已缓存到服务器本地，点击可重新缓存" : "缓存（下载）到服务器本地";
}

// handleCacheButtonClick 点击「缓存」按钮：已缓存时二次确认后允许重新缓存。
async function handleCacheButtonClick(button, site, bookID, metadata = {}) {
  const cached = await bookCached(site, bookID);
  if (cached && !window.confirm("这本书已缓存到服务器本地，确定要重新缓存吗？")) return;
  await startDownloadTask({
    site,
    book_id: bookID,
    title: metadata.title || "",
    cover_url: metadata.coverURL || "",
  }, button, { buttonLoadingText: "正在缓存..." });
}

async function startDownloadTask(target, button, options = {}) {
  const site = target.site || (target.primary && target.primary.site);
  const bookID = target.book_id || (target.primary && target.primary.book_id);
  if (!site || !bookID) return setStatus("下载目标缺失。");

  const taskTarget = options.target === "export" ? "export" : "local";
  const originalText = button ? button.textContent : "";
  if (button) { button.disabled = true; button.textContent = options.buttonLoadingText || (taskTarget === "export" ? "正在导出..." : "正在下载..."); }

  try {
    const body = { site, book_id: bookID, target: taskTarget };
    const title = String(target.title || "").trim();
    const coverURL = String(target.cover_url || target.coverURL || "").trim();
    if (title) body.title = title;
    if (coverURL) body.cover_url = coverURL;
    if (Array.isArray(options.formats) && options.formats.length) body.formats = options.formats;
    const response = await fetch(`${root}/api/download-tasks`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || "download failed");

    if (options.inlineView) appState.inlineTaskViews.set(payload.task.id, options.inlineView);
    upsertTask(payload.task);
    startPollingTask(payload.task.id);
    if (!options.keepDetailOpen) closeDetail();
    if (!options.noActivate) { activateTab("favorites"); activateSubTab("tasks"); }
    const label = taskTarget === "export" ? "导出" : "下载";
    setStatus(`已创建${label}任务：${payload.task.site}/${payload.task.book_id}`);
  } catch (error) { setStatus(`创建任务失败：${error.message}`); }
  finally { if (button) { button.disabled = false; button.textContent = originalText; } }
}

function startPollingTask(taskId) {
  if (appState.pollers.has(taskId)) return;
  const poll = async () => {
    try {
      const response = await fetch(`${root}/api/download-tasks/${taskId}`);
      const payload = await response.json();
      if (!response.ok) throw new Error(payload.error || "task fetch failed");
      const task = payload.task; upsertTask(task);
      if (task.status === "completed") {
        stopPollingTask(taskId);
        const label = task.target === "export" || task.target === "browser" ? "导出完成" : "下载完成";
        setStatus(`${label}：${task.site}/${task.book_id}`);
        if (task.target === "local" || task.target === "shelf") refreshBookshelfAfterTask(task);
      }
      else if (task.status === "failed") { stopPollingTask(taskId); setStatus(`任务失败：${task.error}`); }
    } catch (error) { stopPollingTask(taskId); setStatus(`读取任务状态失败：${error.message}`); }
  };
  void poll();
  appState.pollers.set(taskId, window.setInterval(poll, 1000));
}

function refreshBookshelfAfterTask(task) {
  if (!appState.bookshelf.loaded) return;
  if (task) {
    const key = `${task.site || ""}::${task.book_id || ""}`;
    if (!appState.bookshelf.booksByKey.has(key)) return;
  }
  void loadBookshelf(appState.bookshelf.parentId);
}

function stopPollingTask(taskId) {
  if (!appState.pollers.has(taskId)) return;
  window.clearInterval(appState.pollers.get(taskId)); appState.pollers.delete(taskId);
}

function upsertTask(task) {
  appState.tasks.set(task.id, task);
  updateInlineTaskViews(task);
  renderTasks();
}

function updateInlineTaskViews(task) {
  const view = appState.inlineTaskViews.get(task.id);
  if (!view) return;
  const details = [`来源：${sourceLabel(task.site)}`, `ID：${task.book_id}`];
  if (task.eta) details.push(`剩余：${task.eta}`);
  const current = task.current_chapter ? ` · ${task.current_chapter}` : "";
  view.innerHTML = `
    <div class="inline-progress-head"><span>${formatTaskStatus(task)}</span><span>${taskProgressPercent(task)}%</span></div>
    <div class="task-progress-bar"><div class="task-progress-fill" style="width:${taskProgressPercent(task)}%"></div></div>
    <div class="inline-progress-meta">${details.join(" · ")}${current}</div>
  `;
  if (task.status === "completed" || task.status === "failed") {
    window.setTimeout(() => appState.inlineTaskViews.delete(task.id), 3000);
  }
}

async function hydrateTasksFromServer() {
  try {
    const response = await fetch(`${root}/api/download-tasks`);
    if (!response.ok) return;
    const payload = await response.json();
    const tasks = Array.isArray(payload.tasks) ? payload.tasks : [];
    tasks.forEach((task) => {
      appState.tasks.set(task.id, task);
      if (task.status === "queued" || task.status === "running") {
        startPollingTask(task.id);
      }
    });
    appState.tasksLoaded = true;
    renderTasks();
  } catch (error) {
    setStatus(`任务列表加载失败：${error.message}`);
  }
}

async function deleteTask(taskId) {
  if (!taskId) return;
  stopPollingTask(taskId);
  try {
    const response = await fetch(`${root}/api/download-tasks/${encodeURIComponent(taskId)}`, { method: "DELETE" });
    if (!response.ok && response.status !== 404) {
      const payload = await response.json().catch(() => ({}));
      throw new Error(payload.error || `delete failed (${response.status})`);
    }
    appState.tasks.delete(taskId);
    renderTasks();
  } catch (error) {
    setStatus(`删除任务失败：${error.message}`);
  }
}

async function clearFinishedTasks() {
  const tasks = Array.from(appState.tasks.values()).filter((task) => task.status === "completed" || task.status === "failed");
  if (!tasks.length) return setStatus("没有可清理的任务。");
  for (const task of tasks) {
    // eslint-disable-next-line no-await-in-loop
    await deleteTask(task.id);
  }
  setStatus(`已清理 ${tasks.length} 个任务。`);
}

function favoritesPageSize() {
  const n = Number.parseInt(String(appState.pageSize || ""), 10);
  return n > 0 ? n : 20;
}

function paginate(items, page, pageSize) {
  const total = items.length;
  const pages = Math.max(1, Math.ceil(total / pageSize));
  const current = Math.min(Math.max(1, page), pages);
  const start = (current - 1) * pageSize;
  return { slice: items.slice(start, start + pageSize), current, pages, total, start };
}

function renderSubPagination(node, prevBtn, nextBtn, indicator, info) {
  if (!node) return;
  const show = info.total > info.pageSize;
  node.hidden = !show;
  if (!show) return;
  if (prevBtn) prevBtn.disabled = info.current <= 1;
  if (nextBtn) nextBtn.disabled = info.current >= info.pages;
  if (indicator) indicator.textContent = `第 ${info.current} / ${info.pages} 页 · 共 ${info.total} 项`;
}

function renderTasks() {
  scheduleAppURLSync();
  const tasks = Array.from(appState.tasks.values()).sort((l, r) => new Date(r.updated_at).getTime() - new Date(l.updated_at).getTime());
  if (taskCountNode) taskCountNode.textContent = String(tasks.length);
  if (taskTabCountNode) taskTabCountNode.textContent = String(tasks.length);
  tasksNode.innerHTML = "";

  if (!tasks.length) {
    appState.tasksPage = 1;
    renderSubPagination(tasksPaginationNode, tasksPrevPageButton, tasksNextPageButton, tasksPageIndicatorNode, { total: 0, pageSize: favoritesPageSize(), current: 1, pages: 1 });
    return tasksNode.appendChild(createEmptyState("还没有下载任务。", true));
  }

  const pageSize = favoritesPageSize();
  const info = paginate(tasks, appState.tasksPage, pageSize);
  appState.tasksPage = info.current;
  info.slice.forEach((task) => {
    tasksNode.appendChild(buildTaskCard(task));
  });
  renderSubPagination(tasksPaginationNode, tasksPrevPageButton, tasksNextPageButton, tasksPageIndicatorNode, { ...info, pageSize });
}

function buildTaskCard(task) {
  const card = document.createElement("article");
  card.className = `task-card is-${task.status}`;

  const hasProgress = (task.total_chapters || 0) > 0;
  const percent = taskProgressPercent(task);
  const statusText = formatTaskStatus(task);
  const statusLabel = hasProgress && task.status === "running"
    ? `${statusText} · ${percent}%`
    : statusText;
  const displayTitle = task.title || `${sourceLabel(task.site)}/${task.book_id}`;

  const main = document.createElement("div");
  main.className = "task-main";

  const coverFrame = document.createElement("div");
  coverFrame.className = "task-cover-frame";
  coverFrame.appendChild(createCoverImage(task.cover_url, displayTitle, "task-cover"));
  main.appendChild(coverFrame);

  const content = document.createElement("div");
  content.className = "task-content";

  const head = document.createElement("div");
  head.className = "task-head";
  const title = document.createElement("div");
  title.className = "task-title";
  title.textContent = displayTitle;
  title.title = displayTitle;
  head.appendChild(title);

  const headRight = document.createElement("div");
  headRight.className = "task-head-right";
  const badge = document.createElement("span");
  badge.className = `task-status status-${task.status}`;
  badge.textContent = statusLabel;
  headRight.appendChild(badge);
  if (taskCanReExport(task)) {
    const exportBtn = document.createElement("button");
    exportBtn.type = "button";
    exportBtn.className = "task-export-btn";
    exportBtn.setAttribute("aria-label", "重新导出");
    exportBtn.title = "重新导出（可改格式）";
    exportBtn.textContent = "📤";
    exportBtn.addEventListener("click", (event) => {
      event.stopPropagation();
      openExportFormatPicker(task, exportBtn);
    });
    headRight.appendChild(exportBtn);
  }
  if (task.status === "completed" || task.status === "failed") {
    const delBtn = document.createElement("button");
    delBtn.type = "button";
    delBtn.className = "task-delete-btn";
    delBtn.setAttribute("aria-label", "删除任务");
    delBtn.title = "删除任务";
    delBtn.textContent = "✕";
    delBtn.addEventListener("click", () => void deleteTask(task.id));
    headRight.appendChild(delBtn);
  }
  head.appendChild(headRight);
  content.appendChild(head);

  const chips = document.createElement("div");
  chips.className = "task-chips";
  const sourceChip = document.createElement("span");
  sourceChip.className = "task-chip task-chip-source";
  sourceChip.textContent = sourceLabel(task.site);
  chips.appendChild(sourceChip);
  const targetChip = document.createElement("span");
  targetChip.className = "task-chip";
  targetChip.textContent = taskTargetLabel(task);
  chips.appendChild(targetChip);
  if (task.eta) {
    const etaChip = document.createElement("span");
    etaChip.className = "task-chip task-chip-eta";
    etaChip.textContent = `剩余 ${task.eta}`;
    chips.appendChild(etaChip);
  }
  if (task.current_chapter) {
    const curChip = document.createElement("span");
    curChip.className = "task-chip task-chip-current";
    curChip.textContent = task.current_chapter;
    curChip.title = task.current_chapter;
    chips.appendChild(curChip);
  }
  content.appendChild(chips);

  if (hasProgress) {
    const wrap = document.createElement("div");
    wrap.className = "task-progress-row";
    const bar = document.createElement("div");
    bar.className = "task-progress-bar";
    bar.setAttribute("role", "progressbar");
    bar.setAttribute("aria-label", `${displayTitle} 进度`);
    bar.setAttribute("aria-valuemin", "0");
    bar.setAttribute("aria-valuemax", "100");
    bar.setAttribute("aria-valuenow", String(percent));
    const fill = document.createElement("div");
    fill.className = "task-progress-fill";
    fill.style.width = `${percent}%`;
    bar.appendChild(fill);
    wrap.appendChild(bar);
    const count = document.createElement("span");
    count.className = "task-progress-count";
    count.textContent = `${task.completed_chapters || 0} / ${task.total_chapters}`;
    wrap.appendChild(count);
    content.appendChild(wrap);
  }

  main.appendChild(content);
  card.appendChild(main);

  const support = document.createElement("div");
  support.className = "task-support";

  if (task.error) {
    const error = document.createElement("div");
    error.className = "task-error";
    error.textContent = task.error;
    support.appendChild(error);
  }

  if (Array.isArray(task.exported) && task.exported.length) {
    const files = document.createElement("div");
    files.className = "task-files";
    task.exported.forEach((p) => {
      const link = document.createElement("a");
      link.className = "task-file-chip";
      link.href = `${root}/api/download-file?path=${encodeURIComponent(p)}`;
      link.title = p;
      const basename = p.split(/[/\\]/).pop() || p;
      link.download = basename;
      const dot = basename.lastIndexOf(".");
      const hasExt = dot > 0 && dot < basename.length - 1;
      const stem = hasExt ? basename.slice(0, dot) : basename;
      const ext = hasExt ? basename.slice(dot) : "";
      const nameSpan = document.createElement("span");
      nameSpan.className = "task-file-chip-name";
      nameSpan.textContent = stem;
      link.appendChild(nameSpan);
      if (ext) {
        const extSpan = document.createElement("span");
        extSpan.className = "task-file-chip-ext";
        extSpan.textContent = ext;
        link.appendChild(extSpan);
      }
      files.appendChild(link);
    });
    support.appendChild(files);
  }

  if (Array.isArray(task.messages) && task.messages.length) {
    const messages = document.createElement("div");
    messages.className = "task-messages";
    task.messages.slice(-2).forEach((msg) => {
      const item = document.createElement("div");
      item.className = `task-message level-${msg.level}`;
      item.textContent = msg.text;
      messages.appendChild(item);
    });
    support.appendChild(messages);
  }

  if (support.childElementCount) card.appendChild(support);
  return card;
}

function formatTaskStatus(task) {
  if (task.status === "completed") return "已完成";
  if (task.status === "failed") return "失败";
  if (task.phase === "exporting") return "导出中";
  if (task.phase === "saving") return "保存中";
  if (task.phase === "loading_chapters") return "加载章节中";
  if (task.status === "running") return "下载中";
  return "排队中";
}

function taskProgressPercent(task) {
  return !task.total_chapters ? 0 : Math.min(100, Math.round((task.completed_chapters / task.total_chapters) * 100));
}

function taskTargetLabel(task) {
  if (task.target === "export" || task.target === "browser") return "导出本地文件";
  return "下载到服务器本地";
}

const SUPPORTED_EXPORT_FORMATS = ["txt", "epub", "html"];

function taskCanReExport(task) {
  if (!task || !task.site || !task.book_id) return false;
  // 进行中的任务不允许重新导出，避免和当前作业冲突
  if (task.status === "running") return false;
  return task.status === "completed" || task.status === "failed" || task.status === "queued";
}

function defaultExportFormatsFor(task) {
  if (Array.isArray(task && task.formats) && task.formats.length) {
    const cleaned = task.formats.map((f) => String(f).toLowerCase().trim()).filter((f) => SUPPORTED_EXPORT_FORMATS.includes(f));
    if (cleaned.length) return cleaned;
  }
  const general = appState.generalConfig && Array.isArray(appState.generalConfig.formats) ? appState.generalConfig.formats : [];
  const fromGeneral = general.map((f) => String(f).toLowerCase().trim()).filter((f) => SUPPORTED_EXPORT_FORMATS.includes(f));
  if (fromGeneral.length) return fromGeneral;
  return ["txt", "epub"];
}

let activeExportPicker = null;

function closeExportFormatPicker() {
  if (!activeExportPicker) return;
  const { node, onDocClick, onKey } = activeExportPicker;
  document.removeEventListener("click", onDocClick, true);
  document.removeEventListener("keydown", onKey, true);
  if (node && node.parentNode) node.parentNode.removeChild(node);
  activeExportPicker = null;
}

function openExportFormatPicker(task, anchor) {
  closeExportFormatPicker();
  if (!task || !anchor) return;

  const node = document.createElement("div");
  node.className = "export-format-picker";
  node.setAttribute("role", "dialog");
  node.setAttribute("aria-label", "选择导出格式");

  const heading = document.createElement("h4");
  heading.textContent = "选择导出格式";
  node.appendChild(heading);

  const opts = document.createElement("div");
  opts.className = "export-format-picker-options";
  const initial = new Set(defaultExportFormatsFor(task));
  const checkboxes = SUPPORTED_EXPORT_FORMATS.map((fmt) => {
    const label = document.createElement("label");
    const cb = document.createElement("input");
    cb.type = "checkbox";
    cb.value = fmt;
    cb.checked = initial.has(fmt);
    const text = document.createElement("span");
    text.textContent = fmt.toUpperCase();
    label.appendChild(cb);
    label.appendChild(text);
    opts.appendChild(label);
    return cb;
  });
  node.appendChild(opts);

  const hint = document.createElement("p");
  hint.className = "export-format-picker-hint";
  hint.textContent = "将创建新任务，复用已下载的章节。";
  node.appendChild(hint);

  const actions = document.createElement("div");
  actions.className = "export-format-picker-actions";

  const cancel = document.createElement("button");
  cancel.type = "button";
  cancel.className = "page-button";
  cancel.textContent = "取消";
  cancel.addEventListener("click", closeExportFormatPicker);
  actions.appendChild(cancel);

  const confirm = document.createElement("button");
  confirm.type = "button";
  confirm.className = "search-button is-compact";
  confirm.textContent = "导出";
  confirm.addEventListener("click", () => {
    const selected = checkboxes.filter((cb) => cb.checked).map((cb) => cb.value);
    if (!selected.length) {
      setStatus("请至少选择一种导出格式。");
      return;
    }
    confirm.disabled = true;
    confirm.textContent = "提交中…";
    void startDownloadTask({
      site: task.site,
      book_id: task.book_id,
      title: task.title || "",
      cover_url: task.cover_url || "",
    }, confirm, {
      target: "export",
      formats: selected,
    }).finally(() => {
      closeExportFormatPicker();
    });
  });
  actions.appendChild(confirm);
  node.appendChild(actions);

  document.body.appendChild(node);

  // Position popover next to the anchor button (fixed positioning).
  const rect = anchor.getBoundingClientRect();
  const pickerWidth = node.offsetWidth || 240;
  const pickerHeight = node.offsetHeight || 160;
  const margin = 8;
  let left = Math.min(window.innerWidth - pickerWidth - margin, Math.max(margin, rect.right - pickerWidth));
  let top = rect.bottom + 6;
  if (top + pickerHeight + margin > window.innerHeight) {
    top = Math.max(margin, rect.top - pickerHeight - 6);
  }
  node.style.position = "fixed";
  node.style.left = `${left}px`;
  node.style.top = `${top}px`;

  // Close on outside click / Escape.
  const onDocClick = (event) => {
    if (node.contains(event.target) || anchor.contains(event.target)) return;
    closeExportFormatPicker();
  };
  const onKey = (event) => {
    if (event.key === "Escape") closeExportFormatPicker();
  };
  // Defer so the click that opened it doesn't close it.
  setTimeout(() => {
    document.addEventListener("click", onDocClick, true);
    document.addEventListener("keydown", onKey, true);
  }, 0);

  activeExportPicker = { node, onDocClick, onKey };
}

function createInlineTaskView() {
  const node = document.createElement("div");
  node.className = "inline-progress";
  node.innerHTML = `
    <div class="inline-progress-head"><span>正在排队</span><span>0%</span></div>
    <div class="task-progress-bar"><div class="task-progress-fill" style="width:0%"></div></div>
    <div class="inline-progress-meta">任务创建中...</div>
  `;
  return node;
}

function createCoverImage(src, alt, className) {
  const image = document.createElement("img"); image.className = className; image.loading = "lazy";
  image.alt = alt || "Novel cover"; image.src = src || DEFAULT_COVER_SRC;
  image.addEventListener("error", handleCoverError); return image;
}

function handleCoverError(event) {
  const image = event.currentTarget;
  if (image.dataset.fallbackApplied === "true") return;
  image.dataset.fallbackApplied = "true"; image.src = DEFAULT_COVER_SRC;
}

function createEmptyState(text, compact) {
  const node = document.createElement("div"); node.className = compact ? "empty-state empty-state-compact" : "empty-state"; node.textContent = text; return node;
}

function createEmptyInline(text) {
  const node = document.createElement("div"); node.className = "empty-inline"; node.textContent = text; return node;
}

function displayResultTitle(result) { return result.title || (result.primary && result.primary.title) || (result.primary && result.primary.book_id) || "未命名小说"; }
function displayResultAuthor(result) { return result.author || (result.primary && result.primary.author) || "未知作者"; }
function displayResultURL(result) { return result.url || (result.primary && result.primary.url) || ""; }
function displayDetailURL(result, variant, book) { return (book && book.source_url) || (variant && variant.url) || displayResultURL(result); }
function displayDetailTitle(result, variant, book) { return (book && book.title) || result.title || (variant && variant.title) || (variant && variant.book_id) || "未命名小说"; }
function displayDetailAuthor(result, variant, book) { return (book && book.author) || result.author || (variant && variant.author) || "未知作者"; }
function displayDetailDescription(result, book) { return (book && book.description) || result.description || "暂无简介。"; }
function sourceLabel(siteKey) { return sourceLabelMap.get(siteKey) || siteKey || "未知来源"; }
function detailKey(variant) { return `${variant.site}/${variant.book_id}`; }
function detailRequestKey(variant, chapterPage = 1, chapterPageSize = normalizedChapterPageSize()) { return `${detailKey(variant)}?chapter_page=${chapterPage}&chapter_page_size=${chapterPageSize}${variant && variant.local ? "&local=1" : ""}`; }
function readerCatalogKey(variant) { return `${detailKey(variant)}?reader_catalog=all${variant && variant.local ? "&local=1" : ""}`; }
function totalLabel(total, exact) { return exact ? `${total}` : `${total}+`; }

async function loadSiteConfigs() {
  try {
    const response = await fetch(`${root}/api/site-configs`);
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || "site config load failed");
    const items = Array.isArray(payload.items) ? payload.items : [];
    appState.siteConfigs = new Map(items.map((item) => [item.key, item]));
    appState.paramSupports = Array.isArray(payload.param_supports) ? payload.param_supports : [];
    renderSiteConfigSelector(items);
  } catch (error) { setStatus(`站点配置加载失败：${error.message}`); }
}

function renderSiteConfigSelector(items) {
  siteConfigKeyNode.innerHTML = "";
  const visibleItems = items
    .filter((item) => configurableSiteKeys.includes(item.key))
    .sort((left, right) => configurableSiteKeys.indexOf(left.key) - configurableSiteKeys.indexOf(right.key));
  visibleItems.forEach((item) => {
    const option = document.createElement("option");
    option.value = item.key; option.textContent = sourceLabel(item.key);
    siteConfigKeyNode.appendChild(option);
  });
  if (visibleItems.length > 0) {
    siteConfigKeyNode.value = visibleItems[0].key; populateSiteConfigForm(visibleItems[0].key);
  } else {
    const option = document.createElement("option");
    option.value = ""; option.textContent = "暂无可配置站点";
    siteConfigKeyNode.appendChild(option);
    renderSiteConfigStatus(null);
    renderSiteConfigHelp("");
  }
}

function setRangeVal(id, val) {
  const el = document.getElementById(id);
  if (el) { el.value = val; document.getElementById(id + "Val").textContent = val; }
}

function populateSiteConfigForm(siteKey) {
  const item = appState.siteConfigs.get(siteKey);
  if (!item) return;
  siteUsernameNode.value = item.username || "";
  sitePasswordNode.value = item.password || "";
  sitePasswordNode.type = "password";
  
  // 恢复密码小眼睛状态为隐藏
  toggleSitePasswordButton.innerHTML = `<svg viewBox="0 0 24 24" width="20" height="20" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path><circle cx="12" cy="12" r="3"></circle></svg>`;
  
  siteCookieNode.value = item.cookie || "";
  siteMirrorHostsNode.value = Array.isArray(item.mirror_hosts) ? item.mirror_hosts.join("\n") : "";
  renderSiteConfigStatus(item);
  renderSiteConfigHelp(siteKey);
}

function renderSiteConfigStatus(item) {
  if (!siteConfigStatusNode) return;
  const configured = Boolean(item && (item.cookie || item.username || item.password));
  siteConfigStatusNode.textContent = configured ? "已配置" : "未配置";
  siteConfigStatusNode.dataset.tone = configured ? "configured" : "empty";
}

function renderSiteConfigHelp(siteKey) {
  if (!siteConfigHintNode || !siteCookieHelpNode) return;
  if (siteKey === "novalpie") {
    siteConfigHintNode.textContent = "Novalpie 使用 Token 读取加密正文。";
    siteCookieHelpNode.textContent = "从浏览器开发者工具复制 Authorization 请求头，填写 Bearer eyJ...；也支持只填裸 JWT。";
    return;
  }
  if (siteKey === "esjzone") {
    siteConfigHintNode.textContent = "ESJ Zone 只有账号章节或镜像访问不稳定时才需要配置。";
    siteCookieHelpNode.textContent = "可填写 Cookie，也可填写用户名和密码；成功登录后会复用 Cookie。";
    return;
  }
  if (siteKey === "wenku8") {
    siteConfigHintNode.textContent = "轻小说文库（wenku8）搜索与下载需登录（站点有 Cloudflare 保护）。";
    siteCookieHelpNode.textContent = "可填写账号密码自动登录；也可从浏览器复制 Cookie（含 cf_clearance、PHPSESSID、jieqiUserInfo）直接使用。";
    return;
  }
  siteConfigHintNode.textContent = "";
  siteCookieHelpNode.textContent = "";
}

function setupSettingsNavigation() {
  if (!settingsTabs.length) return;
  settingsTabs.forEach((tab) => {
    tab.addEventListener("click", () => activateSettingsPanel(tab.dataset.settingsTab));
    tab.addEventListener("keydown", (event) => {
      if (!["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End"].includes(event.key)) return;
      event.preventDefault();
      const currentIndex = settingsTabs.indexOf(tab);
      let nextIndex = currentIndex;
      if (event.key === "Home") nextIndex = 0;
      else if (event.key === "End") nextIndex = settingsTabs.length - 1;
      else if (event.key === "ArrowLeft" || event.key === "ArrowUp") {
        nextIndex = (currentIndex - 1 + settingsTabs.length) % settingsTabs.length;
      } else {
        nextIndex = (currentIndex + 1) % settingsTabs.length;
      }
      settingsTabs[nextIndex].focus();
      activateSettingsPanel(settingsTabs[nextIndex].dataset.settingsTab);
    });
  });
  activateSettingsPanel(settingsActivePanel, { scroll: false });
}

function activateSettingsPanel(panelKey, options = {}) {
  const validKeys = settingsTabs.map((tab) => tab.dataset.settingsTab);
  const nextKey = validKeys.includes(panelKey) ? panelKey : "performance";
  settingsActivePanel = nextKey;
  settingsTabs.forEach((tab) => {
    const active = tab.dataset.settingsTab === nextKey;
    tab.classList.toggle("is-active", active);
    tab.setAttribute("aria-selected", String(active));
    tab.tabIndex = active ? 0 : -1;
  });
  settingsPanels.forEach((panel) => {
    panel.hidden = panel.id !== `settingsPanel${nextKey[0].toUpperCase()}${nextKey.slice(1)}`;
  });
  if (options.scroll !== false && settingsContentNode) settingsContentNode.scrollTop = 0;
  if (siteConfigOverlay && !siteConfigOverlay.hidden) scheduleAppURLSync();
}

async function openSiteConfig(options = {}) {
  const panelKey = options.panel || settingsActivePanel;
  const requestedSiteKey = options.siteKey || "";
  if (siteConfigOverlay.hidden) {
    settingsPreviousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  }
  hideTemporaryWarningDialog();
  activateSettingsPanel(panelKey, { scroll: false });
  siteConfigOverlay.hidden = false;
  document.body.classList.add("has-overlay");
  scheduleAppURLSync();
  setSettingsSaveStatus("");
  window.requestAnimationFrame(() => {
    const activeTab = settingsTabs.find((tab) => tab.dataset.settingsTab === settingsActivePanel);
    if (activeTab) {
      activeTab.scrollIntoView({ block: "nearest", inline: "nearest" });
      activeTab.focus({ preventScroll: true });
    }
  });
  try {
    await Promise.all([loadGeneralConfig(), loadSiteConfigs()]);
    if (requestedSiteKey && appState.siteConfigs.has(requestedSiteKey)) {
      siteConfigKeyNode.value = requestedSiteKey;
      populateSiteConfigForm(requestedSiteKey);
      scheduleAppURLSync();
    }
  } catch (error) {
    setSettingsSaveStatus(`设置刷新失败：${error.message}`, "error");
  }
}

function closeSiteConfig() {
  siteConfigOverlay.hidden = true;
  document.body.classList.remove("has-overlay");
  syncAppURL();
  const focusTarget = settingsPreviousFocus;
  settingsPreviousFocus = null;
  if (focusTarget && focusTarget.isConnected) focusTarget.focus({ preventScroll: true });
}

function renderGeneralConfigForm(item) {
  if (!item) return;
  setRangeVal("generalWorkers", item.workers || 4);
  setRangeVal("generalTimeout", item.timeout || 10);
  setRangeVal("generalRequestInterval", item.request_interval || 0.5);
  generalLocaleStyleNode.value = item.locale_style || "simplified";
  generalFormatsNode.value = Array.isArray(item.formats) ? item.formats.join(",") : "txt,epub";
  generalAppendTimestampNode.checked = item.append_timestamp !== false;
  generalIncludePictureNode.checked = item.include_picture !== false;
  generalDisableCacheNode.checked = item.disable_cache === true;
  generalBlurWebImagesNode.checked = item.blur_web_images === true;
  setRangeVal("generalWebPageSize", item.web_page_size || 50);
  setRangeVal("generalCLIPageSize", item.cli_page_size || 30);
  generalRawDataDirNode.value = item.raw_data_dir || "./data/raw_data";
  generalCacheDirNode.value = item.cache_dir || "./data/novel_cache";
  generalOutputDirNode.value = item.output_dir || "./data/downloads";
  applyImageBlurSetting(item);
}

async function loadGeneralConfig() {
  const response = await fetch(`${root}/api/general-config`);
  const payload = await response.json();
  if (!response.ok) throw new Error(payload.error || "general config load failed");
  appState.generalConfig = payload.item || appState.generalConfig;
  if (payload.item) {
    setSearchPageSize(payload.item.web_page_size || defaultPageSize);
    renderPaging();
    renderResultMeta();
  }
  renderGeneralConfigForm(appState.generalConfig);
}

function setSettingsSaveStatus(text, tone) {
  if (!settingsSaveStatusNode) return;
  if (!text) {
    settingsSaveStatusNode.hidden = true;
    settingsSaveStatusNode.textContent = "";
    delete settingsSaveStatusNode.dataset.tone;
    return;
  }
  settingsSaveStatusNode.hidden = false;
  settingsSaveStatusNode.textContent = text;
  settingsSaveStatusNode.dataset.tone = tone || "info";
}

async function saveSettingsConfig() {
  setSettingsSaveStatus("正在保存系统设置...", "info");
  if (saveSettingsConfigButton) saveSettingsConfigButton.disabled = true;
  try {
    await saveGeneralConfig({ showStatus: false, refreshSearch: false });
    await saveSiteConfig({ showStatus: false });
    const message = appState.lastKeyword ? "系统设置已保存，正在按最新设置刷新搜索结果。" : "系统设置已保存。";
    setSettingsSaveStatus(message, "success");
    setStatus(message);
    if (appState.lastKeyword) await performSearch();
  } catch (error) {
    const message = `保存系统设置失败：${error.message}`;
    setSettingsSaveStatus(message, "error");
    setStatus(message, "error");
  } finally {
    if (saveSettingsConfigButton) saveSettingsConfigButton.disabled = false;
  }
}

async function saveGeneralConfig(options = {}) {
  const showStatus = options.showStatus !== false;
  const refreshSearch = options.refreshSearch !== false;
  const payload = {
    workers: Math.max(1, Number.parseInt(generalWorkersNode.value || "4", 10) || 4),
    timeout: Math.max(1, Number.parseFloat(generalTimeoutNode.value || "10") || 10),
    request_interval: Math.max(0, Number.parseFloat(generalRequestIntervalNode.value || "0.5") || 0.5),
    locale_style: (generalLocaleStyleNode.value || "simplified").trim(),
    formats: (generalFormatsNode.value || "txt,epub").split(",").map((item) => item.trim()).filter(Boolean),
    filename_template: (generalFilenameTemplateNode && generalFilenameTemplateNode.value.trim()) || "{title}_{author}",
    append_timestamp: generalAppendTimestampNode.checked,
    include_picture: generalIncludePictureNode.checked,
    disable_cache: generalDisableCacheNode.checked,
    blur_web_images: generalBlurWebImagesNode.checked,
    web_page_size: Math.max(1, Number.parseInt(generalWebPageSizeNode.value || "50", 10) || 50),
    cli_page_size: Math.max(1, Number.parseInt(generalCLIPageSizeNode.value || "30", 10) || 30),
    raw_data_dir: generalRawDataDirNode.value.trim(),
    cache_dir: generalCacheDirNode.value.trim(),
    output_dir: generalOutputDirNode.value.trim(),
  };

  const response = await fetch(`${root}/api/general-config`, {
    method: "PUT", headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error || "save general config failed");
  appState.generalConfig = data.item || payload;
  setSearchPageSize(appState.generalConfig.web_page_size || payload.web_page_size || defaultPageSize);
  appState.page = 1;
  appState.detailCache.clear();
  appState.detailPending.clear();
  appState.detailTimings.clear();
  appState.readerCatalogCache.clear();
  appState.readerCatalogPending.clear();
  readerState.pending.clear();
  renderGeneralConfigForm(appState.generalConfig);
  renderPaging();
  renderResultMeta();
  applyImageBlurSetting(appState.generalConfig);
  if (appState.lastKeyword && refreshSearch) {
    if (showStatus) setStatus("已保存全局配置，正在按新的每页数量刷新搜索结果。");
    await performSearch();
  } else if (showStatus) {
    setStatus("已保存全局配置。后续搜索、详情和下载将使用最新参数。");
  }
}

function applyImageBlurSetting(item) {
  document.body.classList.toggle("web-images-blurred", Boolean(item && item.blur_web_images));
}

async function saveSiteConfig(options = {}) {
  const showStatus = options.showStatus !== false;
  const siteKey = siteConfigKeyNode.value;
  if (!siteKey) return;
  const payload = {
    username: siteUsernameNode.value.trim(),
    password: sitePasswordNode.value.trim(),
    cookie: siteCookieNode.value.trim(),
    mirror_hosts: siteMirrorHostsNode.value.split(/\r?\n/).map((item) => item.trim()).filter(Boolean),
  };

  const response = await fetch(`${root}/api/site-configs/${encodeURIComponent(siteKey)}`, {
    method: "PUT", headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error || "save site config failed");

  if (data.item) appState.siteConfigs.set(siteKey, data.item);
  appState.detailCache.clear();
  appState.detailPending.clear();
  appState.detailTimings.clear();
  appState.readerCatalogCache.clear();
  appState.readerCatalogPending.clear();
  readerState.pending.clear();
  siteWarnings = Array.isArray(data.site_warnings) ? data.site_warnings : [];
  siteStats = Array.isArray(data.site_stats) ? data.site_stats : [];
  renderSiteWarnings();
  if (showStatus) setStatus(`已保存 ${sourceLabel(siteKey)} 配置。`);
}

function setStatus(text, tone) {
  statusNode.textContent = text;
  statusNode.dataset.tone = tone || "info";
}

// -----------------------------------------------------------------------------
// 书架（bookshelf）
// -----------------------------------------------------------------------------

function bookshelfBookKey(item) {
  return `${item.site || ""}::${item.book_id || ""}`;
}

function setBookshelfStatus(text) {
  if (bookshelfStatusNode) bookshelfStatusNode.textContent = text;
}

async function loadBookshelf(parentId, page = 1) {
  if (!bookshelfNode) return;
  const shelf = appState.bookshelf;
  shelf.loading = true;
  setBookshelfStatus("正在加载书架...");
  const params = new URLSearchParams();
  if (parentId) params.set("parent_id", String(parentId));
  const requestedPage = Math.max(1, Math.trunc(Number(page) || 1));
  const pageSize = favoritesPageSize();
  params.set("page", String(requestedPage));
  params.set("page_size", String(pageSize));
  try {
    const response = await fetch(`${root}/api/bookshelf/items?${params.toString()}`);
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || "load bookshelf failed");
    shelf.parentId = parentId || null;
    shelf.items = Array.isArray(payload.items) ? payload.items : [];
    shelf.breadcrumb = Array.isArray(payload.breadcrumb) ? payload.breadcrumb : [];
    shelf.total = Math.max(0, Number(payload.total) || 0);
    shelf.bookTotal = Math.max(0, Number(payload.book_total) || 0);
    shelf.pages = Math.max(1, Number(payload.pages) || 1);
    shelf.pageSize = Math.max(1, Number(payload.page_size) || pageSize);
    shelf.page = Math.min(Math.max(1, Number(payload.page) || requestedPage), shelf.pages);
    shelf.loaded = true;
    updateBookshelfFolderActions();
    rebuildBookshelfIndex();
    renderBookshelf();
    renderBookshelfBreadcrumb();
    const total = shelf.total;
    setBookshelfStatus(total === 0 ? "当前目录为空。" : `当前目录共 ${total} 项。`);
    // Deleting the last entry of the final page leaves an empty window; step
    // back to the new last page instead of showing a blank shelf.
    if (!shelf.items.length && shelf.page > 1 && requestedPage === shelf.page && !shelf.reloading) {
      shelf.reloading = true;
      try {
        await loadBookshelf(parentId, shelf.page - 1);
      } finally {
        shelf.reloading = false;
      }
    }
  } catch (error) {
    setBookshelfStatus(`加载书架失败：${error.message}`);
  } finally {
    shelf.loading = false;
  }
}

function updateBookshelfFolderActions() {
  if (!bookshelfNewFolderButton) return;
  const insideFolder = Boolean(appState.bookshelf.parentId);
  // 子目录里完全隐藏「新建文件夹」按钮：分类只能建在根目录
  bookshelfNewFolderButton.hidden = insideFolder;
  bookshelfNewFolderButton.disabled = insideFolder;
  bookshelfNewFolderButton.title = insideFolder ? "分类只能建在书架根目录" : "";
}

function rebuildBookshelfIndex() {
  const map = appState.bookshelf.booksByKey;
  map.clear();
  appState.bookshelf.items.forEach((item) => {
    if (item.kind === "book") map.set(bookshelfBookKey(item), item);
  });
  if (bookshelfTabCountNode) {
    bookshelfTabCountNode.textContent = String(appState.bookshelf.bookTotal || 0);
  }
}

function renderBookshelfBreadcrumb() {
  if (!bookshelfBreadcrumbNode) return;
  bookshelfBreadcrumbNode.innerHTML = "";
  const rootCrumb = document.createElement("button");
  rootCrumb.type = "button";
  rootCrumb.className = "bookshelf-crumb is-root";
  rootCrumb.dataset.parentId = "";
  rootCrumb.textContent = "书架";
  bookshelfBreadcrumbNode.appendChild(rootCrumb);
  appState.bookshelf.breadcrumb.forEach((item, idx) => {
    const separator = document.createElement("span");
    separator.className = "bookshelf-crumb-sep";
    separator.textContent = "/";
    bookshelfBreadcrumbNode.appendChild(separator);
    const node = document.createElement("button");
    node.type = "button";
    node.className = "bookshelf-crumb";
    node.dataset.parentId = String(item.id);
    node.textContent = item.name || item.title || `#${item.id}`;
    if (idx === appState.bookshelf.breadcrumb.length - 1) node.classList.add("is-current");
    bookshelfBreadcrumbNode.appendChild(node);
  });
}

function renderBookshelf() {
  if (!bookshelfNode) return;
  scheduleAppURLSync();
  bookshelfNode.innerHTML = "";
  const shelf = appState.bookshelf;
  const items = shelf.items;
  const pageInfo = {
    total: shelf.total,
    pageSize: shelf.pageSize || favoritesPageSize(),
    current: shelf.page || 1,
    pages: shelf.pages || 1,
  };
  if (!items.length) {
    renderSubPagination(bookshelfPaginationNode, bookshelfPrevPageButton, bookshelfNextPageButton, bookshelfPageIndicatorNode, pageInfo);
    bookshelfNode.appendChild(createEmptyState(shelf.parentId ? "这个文件夹是空的。" : "书架还是空的。", true));
    return;
  }
  items.forEach((item) => {
    if (item.kind === "folder") {
      bookshelfNode.appendChild(renderBookshelfFolderCard(item));
    } else {
      bookshelfNode.appendChild(renderBookshelfBookCard(item));
    }
  });
  renderSubPagination(bookshelfPaginationNode, bookshelfPrevPageButton, bookshelfNextPageButton, bookshelfPageIndicatorNode, pageInfo);
}

function setHistoryStatus(text) {
  if (historyStatusNode) historyStatusNode.textContent = text;
}

// refreshHistoryCount keeps the "历史" tab badge populated without loading the
// full list. One-row requests are cheap and the server reports the real total.
async function refreshHistoryCount() {
  const history = appState.history;
  if (historyTabCountNode && history.loaded) {
    historyTabCountNode.textContent = String(history.total || 0);
    return;
  }
  if (history.countPromise) return history.countPromise;
  history.countPromise = (async () => {
    try {
      const params = new URLSearchParams();
      params.set("page", "1");
      params.set("page_size", "1");
      const response = await fetch(`${root}/api/bookshelf/history?${params.toString()}`);
      if (!response.ok) return;
      const payload = await response.json();
      const total = Math.max(0, Number(payload.total) || 0);
      history.total = total;
      if (historyTabCountNode) historyTabCountNode.textContent = String(total);
    } catch (_) {
      // A stale badge is better than blocking the bookshelf view.
    } finally {
      history.countPromise = null;
    }
  })();
  return history.countPromise;
}

async function loadHistory(page = 1) {
  if (!historyListNode) return;
  const history = appState.history;
  if (history.loading) return;
  history.loading = true;
  setHistoryStatus("正在加载阅读历史...");
  const pageSize = favoritesPageSize();
  const requestedPage = Math.max(1, Math.trunc(Number(page) || 1));
  const params = new URLSearchParams();
  params.set("page", String(requestedPage));
  params.set("page_size", String(pageSize));
  try {
    const response = await fetch(`${root}/api/bookshelf/history?${params.toString()}`);
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || "load history failed");
    history.items = Array.isArray(payload.items) ? payload.items : [];
    history.total = Math.max(0, Number(payload.total) || 0);
    history.pages = Math.max(1, Number(payload.pages) || 1);
    history.pageSize = Math.max(1, Number(payload.page_size) || pageSize);
    history.page = Math.min(Math.max(1, Number(payload.page) || requestedPage), history.pages);
    history.loaded = true;
    renderHistory();
    if (historyTabCountNode) historyTabCountNode.textContent = String(history.total);
    if (history.total === 0) {
      setHistoryStatus("尚未读过任何小说，先去书架点击书籍开始阅读吧。");
    } else {
      setHistoryStatus(`共 ${history.total} 条阅读记录。`);
    }
    if (!history.items.length && history.page > 1 && requestedPage === history.page && !history.reloading) {
      history.reloading = true;
      history.loading = false;
      try {
        await loadHistory(history.page - 1);
      } finally {
        history.reloading = false;
      }
    }
  } catch (error) {
    setHistoryStatus(`加载阅读历史失败：${error.message}`);
  } finally {
    history.loading = false;
  }
}

function renderHistory() {
  if (!historyListNode) return;
  scheduleAppURLSync();
  historyListNode.innerHTML = "";
  const history = appState.history;
  const items = history.items;
  const pageInfo = {
    total: history.total,
    pageSize: history.pageSize || favoritesPageSize(),
    current: history.page || 1,
    pages: history.pages || 1,
  };
  if (!items.length) {
    renderSubPagination(historyPaginationNode, historyPrevPageButton, historyNextPageButton, historyPageIndicatorNode, pageInfo);
    historyListNode.appendChild(createEmptyState("还没有阅读记录。", true));
    return;
  }
  items.forEach((item) => historyListNode.appendChild(renderHistoryCard(item)));
  renderSubPagination(historyPaginationNode, historyPrevPageButton, historyNextPageButton, historyPageIndicatorNode, pageInfo);
}

function renderHistoryCard(item) {
  const card = document.createElement("article");
  card.className = "history-card is-clickable";
  card.setAttribute("role", "button");
  card.setAttribute("tabindex", "0");
  card.setAttribute("aria-label", `继续阅读《${item.title || item.book_id}》`);
  const open = () => void openHistoryReader(item);
  card.addEventListener("click", open);
  card.addEventListener("keydown", (event) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      open();
    }
  });

  const coverWrap = document.createElement("div");
  coverWrap.className = "history-cover-wrap";
  coverWrap.appendChild(createCoverImage(item.cover_url, item.title || "Cover", "history-cover"));
  card.appendChild(coverWrap);

  const body = document.createElement("div");
  body.className = "history-body";

  const title = document.createElement("h3");
  title.className = "history-title";
  title.textContent = item.title || item.book_id || "未命名";
  body.appendChild(title);

  const meta = document.createElement("div");
  meta.className = "history-meta";
  meta.appendChild(resultBadge(sourceLabel(item.site)));
  const resumePage = Math.max(1, Number(item.last_read_page) || 1);
  if (item.last_read_chapter_title) {
    meta.appendChild(resultBadge(`读到：${item.last_read_chapter_title} · 第 ${resumePage} 页`));
  } else if (item.last_read_chapter_id) {
    meta.appendChild(resultBadge(`已读第 ${item.last_read_chapter_index + 1 || ""} 章 · 第 ${resumePage} 页`.trim()));
  }
  if (item.total_chapters > 0) {
    const idx = Math.max(0, item.last_read_chapter_index || 0);
    const pct = Math.min(100, Math.round(((idx + 1) / item.total_chapters) * 100));
    meta.appendChild(resultBadge(`${pct}% · ${idx + 1}/${item.total_chapters}`));
  }
  if (item.last_read_at) meta.appendChild(resultBadge(formatHistoryTimestamp(item.last_read_at)));
  body.appendChild(meta);

  if (item.author) {
    const author = document.createElement("p");
    author.className = "history-author";
    author.textContent = `作者：${item.author}`;
    body.appendChild(author);
  }

  card.appendChild(body);
  return card;
}

function formatHistoryTimestamp(iso) {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  const diffMs = Date.now() - date.getTime();
  const diffMin = Math.round(diffMs / 60000);
  if (diffMin < 1) return "刚刚";
  if (diffMin < 60) return `${diffMin} 分钟前`;
  const diffHour = Math.round(diffMin / 60);
  if (diffHour < 24) return `${diffHour} 小时前`;
  const diffDay = Math.round(diffHour / 24);
  if (diffDay < 30) return `${diffDay} 天前`;
  return date.toLocaleDateString();
}

async function openHistoryReader(item) {
  const resumePage = Math.max(1, Number(item.last_read_page) || 1);
  await openBookshelfReader(item, null, {
    readerOptions: { pageIndex: resumePage - 1 },
  });
}

async function reportReadingProgress(payload) {
  if (!payload || !payload.site || !payload.book_id) return;
  try {
    await fetch(`${root}/api/bookshelf/progress`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    // History tab cache becomes stale once any progress is recorded.
    appState.history.loaded = false;
  } catch (error) {
    // Silent best-effort; progress can be re-tried on the next chapter change.
    console.warn("report reading progress failed", error);
  }
}

function buildProgressPayload(tracker, chapter, index) {
  return {
    site: tracker.site,
    book_id: tracker.bookID,
    chapter_id: String((chapter && (chapter.chapter_id || chapter.id)) || "").trim(),
    chapter_index: Math.max(0, index || 0),
    chapter_title: (chapter && chapter.title) || "",
    page: Math.max(1, readerState.pageIndex + 1),
    title: tracker.title || "",
    author: tracker.author || "",
    cover_url: tracker.coverURL || "",
    description: tracker.description || "",
    latest_chapter: tracker.latestChapter || "",
    source_url: tracker.sourceURL || "",
  };
}

function scheduleProgressReport(chapter, index) {
  const tracker = appState.reader;
  if (!tracker || !tracker.site || !tracker.bookID) return;
  const chapterID = String((chapter && (chapter.chapter_id || chapter.id)) || "").trim();
  if (!chapterID) return;
  const page = Math.max(1, readerState.pageIndex + 1);
  const progressKey = `${chapterID}:${page}`;
  if (progressKey === tracker.lastReportedProgressKey) return;
  tracker.lastReportedProgressKey = progressKey;
  if (tracker.progressTimer) window.clearTimeout(tracker.progressTimer);
  const payload = buildProgressPayload(tracker, chapter, index);
  tracker.progressTimer = window.setTimeout(() => {
    tracker.progressTimer = 0;
    void reportReadingProgress(payload);
  }, 1500);
}

function flushProgressReport() {
  const tracker = appState.reader;
  if (!tracker) return;
  if (tracker.progressTimer) {
    window.clearTimeout(tracker.progressTimer);
    tracker.progressTimer = 0;
  }
  if (!tracker.site || !tracker.bookID) return;
  const idx = readerState.currentIndex;
  const chapter = readerState.chapters[idx];
  if (!chapter) return;
  const chapterID = String(chapter.chapter_id || chapter.id || "").trim();
  if (!chapterID) return;
  void reportReadingProgress(buildProgressPayload(tracker, chapter, idx));
}

function renderBookshelfFolderCard(item) {
  const card = document.createElement("article");
  card.className = "result-card bookshelf-card is-folder";
  const cover = document.createElement("button");
  cover.type = "button";
  cover.className = "result-cover bookshelf-folder-cover";
  cover.setAttribute("aria-label", `打开文件夹 ${item.name}`);
  cover.innerHTML = `<svg viewBox="0 0 64 48" width="64" height="48" aria-hidden="true"><path fill="currentColor" d="M6 6h18l6 6h28a4 4 0 0 1 4 4v24a4 4 0 0 1-4 4H6a4 4 0 0 1-4-4V10a4 4 0 0 1 4-4z"/></svg>`;
  cover.addEventListener("click", () => void loadBookshelf(item.id));
  card.appendChild(cover);

  const body = document.createElement("div");
  body.className = "result-body";
  const title = document.createElement("h3");
  title.className = "result-title";
  title.textContent = item.name || `文件夹 #${item.id}`;
  body.appendChild(title);
  const meta = document.createElement("p");
  meta.className = "result-author";
  meta.textContent = `${item.child_count || 0} 项内容`;
  body.appendChild(meta);

  const actions = document.createElement("div");
  actions.className = "bookshelf-actions-row";
  const renameButton = document.createElement("button");
  renameButton.type = "button";
  renameButton.className = "tool-button is-ghost";
  renameButton.textContent = "重命名";
  renameButton.addEventListener("click", () => void renameBookshelfItem(item));
  const deleteButton = document.createElement("button");
  deleteButton.type = "button";
  deleteButton.className = "tool-button is-ghost";
  deleteButton.textContent = "删除";
  deleteButton.addEventListener("click", () => void removeBookshelfItem(item));
  actions.append(renameButton, deleteButton);
  body.appendChild(actions);

  card.appendChild(body);
  return card;
}

function renderBookshelfBookCard(item) {
  const card = document.createElement("article");
  card.className = "result-card bookshelf-card is-book";

  const coverButton = document.createElement("button");
  coverButton.type = "button";
  coverButton.className = "result-cover-button";
  coverButton.setAttribute("aria-label", `打开《${item.title || item.book_id}》阅读`);
  coverButton.appendChild(createCoverImage(item.cover_url, item.title || "Bookshelf cover", "result-cover"));
  const overlay = document.createElement("span");
  overlay.className = "result-cover-overlay";
  overlay.textContent = "立即阅读";
  coverButton.appendChild(overlay);
  coverButton.addEventListener("click", () => void openBookshelfReader(item, null));
  card.appendChild(coverButton);

  const body = document.createElement("div");
  body.className = "result-body";
  const title = document.createElement("h3");
  title.className = "result-title";
  const titleButton = document.createElement("button");
  titleButton.type = "button";
  titleButton.className = "bookshelf-title-button";
  titleButton.textContent = item.title || item.book_id || "未命名小说";
  titleButton.addEventListener("click", () => void openBookshelfReader(item, null));
  title.appendChild(titleButton);
  body.appendChild(title);

  const author = document.createElement("p");
  author.className = "result-author";
  author.textContent = item.author || "未知作者";
  body.appendChild(author);

  const meta = document.createElement("div");
  meta.className = "result-meta";
  meta.appendChild(resultBadge(sourceLabel(item.site)));
  if (item.cached_chapters > 0 && item.total_chapters > 0) {
    meta.appendChild(resultBadge(`已下载 ${item.cached_chapters}/${item.total_chapters} 章`));
  } else if (item.total_chapters > 0) {
    meta.appendChild(resultBadge(`${item.total_chapters} 章`));
  } else {
    meta.appendChild(resultBadge("尚未下载"));
  }
  if (item.latest_chapter) meta.appendChild(resultBadge(item.latest_chapter));
  body.appendChild(meta);

  const actions = document.createElement("div");
  actions.className = "bookshelf-actions-row";

  const downloadButton = document.createElement("button");
  downloadButton.type = "button";
  downloadButton.className = "tool-button is-ghost";
  downloadButton.textContent = "下载";
  downloadButton.addEventListener("click", () => void startDownloadTask({
    site: item.site,
    book_id: item.book_id,
    title: item.title || "",
    cover_url: item.cover_url || "",
  }, downloadButton));
  actions.appendChild(downloadButton);

  const exportButton = document.createElement("button");
  exportButton.type = "button";
  exportButton.className = "tool-button is-ghost";
  exportButton.textContent = "导出";
  exportButton.addEventListener("click", () => void startDownloadTask({
    site: item.site,
    book_id: item.book_id,
    title: item.title || "",
    cover_url: item.cover_url || "",
  }, exportButton, { target: "export" }));
  actions.appendChild(exportButton);

  const deleteButton = document.createElement("button");
  deleteButton.type = "button";
  deleteButton.className = "tool-button is-ghost";
  deleteButton.textContent = "移除";
  deleteButton.addEventListener("click", () => void removeBookshelfItem(item));
  actions.appendChild(deleteButton);

  body.appendChild(actions);
  card.appendChild(body);
  return card;
}

async function createBookshelfFolderPrompt() {
  if (appState.bookshelf.parentId) {
    setBookshelfStatus("分类只能建在书架根目录。");
    return;
  }
  const name = window.prompt("文件夹名称");
  if (!name || !name.trim()) return;
  try {
    const response = await fetch(`${root}/api/bookshelf/folders`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ parent_id: appState.bookshelf.parentId || null, name: name.trim() }),
    });
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || "create folder failed");
    setBookshelfStatus(`已创建文件夹「${payload.item.name}」`);
    await loadBookshelf(appState.bookshelf.parentId);
  } catch (error) {
    setBookshelfStatus(`新建文件夹失败：${error.message}`);
  }
}

async function renameBookshelfItem(item) {
  const fallback = item.kind === "folder" ? (item.name || "") : (item.title || "");
  const next = window.prompt("新名称", fallback);
  if (next === null) return;
  const trimmed = next.trim();
  if (!trimmed || trimmed === fallback) return;
  try {
    const response = await fetch(`${root}/api/bookshelf/items/${item.id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: trimmed }),
    });
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || "rename failed");
    await loadBookshelf(appState.bookshelf.parentId);
  } catch (error) {
    setBookshelfStatus(`重命名失败：${error.message}`);
  }
}

async function removeBookshelfItem(item) {
  const labelName = item.kind === "folder" ? (item.name || `文件夹 #${item.id}`) : (item.title || item.book_id || `条目 #${item.id}`);
  const message = item.kind === "folder"
    ? `确认删除文件夹「${labelName}」及其下全部内容？`
    : `确认从书架移除「${labelName}」？本地已下载内容会保留。`;
  if (!window.confirm(message)) return;
  try {
    const response = await fetch(`${root}/api/bookshelf/items/${item.id}`, { method: "DELETE" });
    if (!response.ok) {
      const payload = await response.json().catch(() => ({}));
      throw new Error(payload.error || `delete failed (${response.status})`);
    }
    await loadBookshelf(appState.bookshelf.parentId);
  } catch (error) {
    setBookshelfStatus(`删除失败：${error.message}`);
  }
}

function setDetailShelfButtonState(button, onShelf) {
  if (!button) return;
  button.dataset.shelfState = onShelf ? "saved" : "idle";
  button.textContent = onShelf ? "移出书架" : "加入书架";
  button.title = onShelf ? "从书架中移出，阅读历史和已下载内容会保留" : "加入书架";
}

async function toggleCurrentDetailBookshelf(result, variant, detail, button, options = {}) {
  if (!result || !variant) return;
  const silent = Boolean(options && options.silent);
  const site = variant.site || (result.primary && result.primary.site);
  const bookID = variant.book_id || (result.primary && result.primary.book_id);
  if (!site || !bookID) {
    if (!silent) setStatus("更新书架失败：缺少站点或 book_id。");
    return;
  }
  const book = (detail && detail.book) || {};
  const payload = {
    parent_id: appState.bookshelf.parentId || null,
    site,
    book_id: bookID,
    title: book.title || displayDetailTitle(result, variant, book),
    author: book.author || displayDetailAuthor(result, variant, book),
    cover_url: book.cover_url || result.cover_url || variant.cover_url || "",
    description: book.description || result.description || "",
    latest_chapter: result.latest_chapter || "",
    source_url: book.source_url || variant.url || result.url || "",
  };

  const original = button ? button.textContent : "";
  const originalTitle = button ? button.title : "";
  if (button) { button.disabled = true; button.textContent = "处理中..."; }
  try {
    const current = await fetchBookshelfProgress(site, bookID);
    if (current && !current.implicit) {
      const response = await fetch(`${root}/api/bookshelf/items/${current.id}`, { method: "DELETE" });
      const data = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(data.error || "remove bookshelf failed");
      appState.bookshelf.loaded = false;
      setDetailShelfButtonState(button, false);
      if (!silent) setStatus(`已从书架移出：${current.title || bookID}。阅读历史和已下载内容会保留。`);
      return;
    }

    const response = await fetch(`${root}/api/bookshelf/books`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || "add bookshelf failed");
    appState.bookshelf.loaded = false;
    if (!silent) setStatus(`已加入书架：${data.item.title || data.item.book_id}。需要离线阅读时可点击「缓存」下载。`);
    setDetailShelfButtonState(button, true);
  } catch (error) {
    if (!silent) setStatus(`更新书架失败：${error.message}`);
    if (button) {
      button.disabled = false;
      button.textContent = original;
      button.title = originalTitle;
    }
  } finally {
    if (button) button.disabled = false;
  }
}

// fetchBookshelfProgress 查询某本书（含仅历史记录的隐式条目）的书架数据。
async function fetchBookshelfProgress(site, bookID) {
  if (!site || !bookID) return null;
  try {
    const response = await fetch(`${root}/api/bookshelf/progress?site=${encodeURIComponent(site)}&book_id=${encodeURIComponent(bookID)}`);
    if (!response.ok) return null;
    const payload = await response.json();
    return payload && payload.item ? payload.item : null;
  } catch (_) {
    return null;
  }
}

// syncDetailShelfState 打开详情时同步书架按钮的加入/移出状态。
async function syncDetailShelfState(button, site, bookID) {
  if (!button || !site || !bookID) return;
  const item = await fetchBookshelfProgress(site, bookID);
  if (!button.isConnected || button.disabled) return;
  setDetailShelfButtonState(button, Boolean(item && !item.implicit));
}

// continueReadingFromDetail 详情页「继续阅读」：有阅读进度则跳到上次章节，否则从第一章开始。
async function continueReadingFromDetail(result, variant, detail, button) {
  const activeVariant = variant || (result && result.primary);
  const site = activeVariant && activeVariant.site;
  const bookID = activeVariant && activeVariant.book_id;
  if (!site || !bookID) {
    setStatus("继续阅读失败：缺少站点或 book_id。");
    return;
  }
  const book = (detail && detail.book) || {};
  const item = {
    site,
    book_id: bookID,
    title: book.title || displayDetailTitle(result, activeVariant, book),
    author: book.author || displayDetailAuthor(result, activeVariant, book),
    cover_url: book.cover_url || result.cover_url || activeVariant.cover_url || "",
    description: book.description || result.description || "",
    latest_chapter: result.latest_chapter || book.latest_chapter || "",
    source_url: book.source_url || activeVariant.url || result.url || "",
  };
  const progress = await fetchBookshelfProgress(site, bookID);
  if (progress) {
    item.last_read_chapter_id = progress.last_read_chapter_id || "";
    item.last_read_chapter_index = progress.last_read_chapter_index || 0;
    item.last_read_chapter_title = progress.last_read_chapter_title || "";
    item.last_read_page = Math.max(1, progress.last_read_page || 1);
  }
  // openBookshelfReader 会改写详情状态，这里保留原详情以便阅读器「返回」时恢复。
  const savedResult = appState.detailResult;
  const savedVariant = appState.activeDetailVariant;
  await openBookshelfReader(item, button, {
    readerOptions: { pageIndex: Math.max(0, Number(item.last_read_page || 1) - 1) },
  });
  appState.detailResult = savedResult;
  appState.activeDetailVariant = savedVariant;
}

// touchBookshelfItem marks a shelf entry as just used so the shelf can sort by
// newest activity, mirroring how history brings the latest read to the front.
async function touchBookshelfItem(id) {
  const itemID = Math.trunc(Number(id) || 0);
  if (itemID <= 0) return;
  try {
    await fetch(`${root}/api/bookshelf/items/${itemID}/touch`, { method: "POST" });
    // The next shelf visit reloads from the server so ordering stays fresh.
    appState.bookshelf.loaded = false;
  } catch (_) {
    // Best effort: reading still works if the touch request fails.
  }
}

async function openBookshelfReader(item, button, options = {}) {
  const original = button ? button.textContent : "";
  if (button) { button.disabled = true; button.textContent = "加载中..."; }
  try {
    if (!item.site || !item.book_id) throw new Error("缺少站点或 book_id");
    if (item.id) void touchBookshelfItem(item.id);

    setStatus(`正在打开《${item.title || item.book_id}》...`);
    const { detail, variant, localOnly } = await loadDetailWithLocalFallback(item);
    if (!detail || !detail.book) throw new Error("未返回详情数据");

    const synthetic = {
      title: item.title,
      author: item.author,
      cover_url: item.cover_url,
      description: item.description,
      latest_chapter: item.latest_chapter,
      url: item.source_url,
      primary: variant,
      sources: [variant],
      source_count: 1,
    };
    appState.detailResult = synthetic;
    appState.activeDetailVariant = variant;

    const initialChapters = Array.isArray(detail.book.chapters) ? detail.book.chapters : [];
    const chapters = await loadReaderCatalog(variant, detail.chapterPage, initialChapters);
    if (!chapters.length) throw new Error("没有可用的章节");

    setupReaderTracker(item.site, item.book_id, item.title, {
      localOnly,
      author: item.author || "",
      coverURL: item.cover_url || "",
      description: item.description || "",
      latestChapter: item.latest_chapter || "",
      sourceURL: item.source_url || "",
    });
    const startIndex = resolveBookshelfStartIndex(chapters, item);
    const readerOptions = { ...((options && options.readerOptions) || {}) };
    const savedPageIndex = Math.max(0, (Number(item.last_read_page) || 1) - 1);
    if (!Number.isFinite(Number(readerOptions.pageIndex)) && savedPageIndex > 0) {
      readerOptions.pageIndex = savedPageIndex;
    }
    openReader(chapters, startIndex, readerOptions);
    const sourceTag = localOnly ? "本地缓存" : "在线";
    if (startIndex > 0) {
      const startChapter = chapters[startIndex];
      const label = (startChapter && startChapter.title) || `第 ${startIndex + 1} 章`;
      setStatus(`已跳到《${item.title || item.book_id}》上次阅读位置：${label}（${sourceTag}）`);
    } else {
      setStatus(`已打开《${item.title || item.book_id}》（${sourceTag}）`);
    }
    return true;
  } catch (error) {
    setStatus(`加载阅读器失败：${error.message}`);
    return false;
  } finally {
    if (button) { button.disabled = false; button.textContent = original; }
  }
}

// loadDetailWithLocalFallback prefers cached local data so downloaded books open
// instantly, then transparently falls back to the online source for items that
// have never been downloaded (e.g. implicit history rows from web reading).
async function loadDetailWithLocalFallback(item) {
  const baseVariant = {
    site: item.site,
    book_id: item.book_id,
    title: item.title,
    author: item.author,
    url: item.source_url,
  };
  const localVariant = { ...baseVariant, local: true };
  try {
    const detail = await fetchBookDetail(localVariant, 1, normalizedChapterPageSize(), { local: true });
    if (detail && detail.book && Array.isArray(detail.book.chapters) && detail.book.chapters.length > 0) {
      return { detail, variant: localVariant, localOnly: true };
    }
  } catch (_) {
    // Ignore local miss and fall through to online lookup.
  }
  const onlineVariant = { ...baseVariant, local: false };
  const detail = await fetchBookDetail(onlineVariant, 1, normalizedChapterPageSize());
  return { detail, variant: onlineVariant, localOnly: false };
}

function resolveBookshelfStartIndex(chapters, item) {
  if (!Array.isArray(chapters) || chapters.length === 0) return 0;
  const lastID = String((item && item.last_read_chapter_id) || "").trim();
  if (lastID) {
    for (let i = 0; i < chapters.length; i += 1) {
      const ch = chapters[i];
      const candidate = String((ch && (ch.chapter_id || ch.id)) || "").trim();
      if (candidate && candidate === lastID) return i;
    }
  }
  // Fall back to the stored index when chapter id lookup fails (e.g. catalog updated).
  const idxHint = Number(item && item.last_read_chapter_index);
  if (Number.isFinite(idxHint) && idxHint > 0) {
    return Math.min(Math.max(0, Math.trunc(idxHint)), chapters.length - 1);
  }
  return 0;
}

function setupReaderTracker(site, bookID, title, options = {}) {
  const tracker = appState.reader;
  if (!tracker) return;
  if (tracker.progressTimer) {
    window.clearTimeout(tracker.progressTimer);
    tracker.progressTimer = 0;
  }
  const opts = (options && typeof options === "object" && !Array.isArray(options)) ? options : { localOnly: Boolean(options) };
  tracker.site = String(site || "").trim();
  tracker.bookID = String(bookID || "").trim();
  tracker.title = String(title || "").trim();
  tracker.author = String(opts.author || "").trim();
  tracker.coverURL = String(opts.coverURL || opts.cover_url || "").trim();
  tracker.description = String(opts.description || "").trim();
  tracker.latestChapter = String(opts.latestChapter || opts.latest_chapter || "").trim();
  tracker.sourceURL = String(opts.sourceURL || opts.source_url || "").trim();
  tracker.localOnly = Boolean(opts.localOnly);
  tracker.lastReportedProgressKey = "";
}
