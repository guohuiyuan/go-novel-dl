const root = window.__NOVEL_DL__.root;
const defaultSources = window.__NOVEL_DL__.defaultSources || [];
const allSources = window.__NOVEL_DL__.allSources || [];
const configurableSiteKeys = ["novalpie", "esjzone"];
const defaultPageSize = window.__NOVEL_DL__.pageSize || 50;
const initialGeneralConfig = window.__NOVEL_DL__.generalConfig || {};
const versionInfo = window.__NOVEL_DL__.version || { current: "", repo: "", mirrors: [] };
const sourceLabelMap = new Map(
  allSources.map((source) => [source.key, source.display_name || source.key]),
);
let siteWarnings = window.__NOVEL_DL__.siteWarnings || [];
let siteStats = window.__NOVEL_DL__.siteStats || [];
const siteWarningPanel = document.getElementById("siteWarningPanel");
let siteWarningHideTimer = 0;
let temporaryWarningDialogTimer = 0;

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
const sourceTagSet = new Set(allSources.flatMap((source) => (Array.isArray(source.tags) ? source.tags : []).map((tag) => `${tag || ""}`.trim()).filter(Boolean)));
const initialSearchSourceState = loadSearchSourceState();

const appState = {
  activeTab: "search",
  page: 1,
  pageSize: defaultPageSize,
  lastKeyword: "",
  results: [],
  total: 0,
  totalExact: true,
  hasPrev: false,
  hasNext: false,
  selectedSites: new Set(initialSearchSourceState.sites),
  selectedSourceTags: new Set(initialSearchSourceState.tags),
  tasks: new Map(),
  tasksLoaded: false,
  pollers: new Map(),
  inlineTaskViews: new Map(),
  bookshelf: {
    parentId: null,
    items: [],
    breadcrumb: [],
    loading: false,
    loaded: false,
    booksByKey: new Map(),
  },
  history: {
    items: [],
    loading: false,
    loaded: false,
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
    lastReportedChapterID: "",
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

  const icon = document.createElement("span");
  icon.className = "site-warning-icon";
  icon.textContent = warningLevelIcons[warning.level] || "ℹ️";

  const message = document.createElement("p");
  message.className = "site-warning-message";
  message.textContent = warning.message;

  const header = document.createElement("div");
  header.className = "site-warning-head";
  header.append(icon, message);
  card.appendChild(header);

  const stat = siteStats.find((stat) => stat.site_key === warning.site_key);
  if (stat && stat.enabled.length) {
    const detail = document.createElement("p");
    detail.className = "site-warning-detail";
    detail.textContent = `已自动配置字段：${stat.enabled.join("、")}`;
    card.appendChild(detail);
  }

  if (warning.action_label && warning.action_link) {
    const action = document.createElement("a");
    action.className = "site-warning-action";
    action.href = warning.action_link;
    if (warning.action_link === "#site-config") {
      action.addEventListener("click", (event) => {
        event.preventDefault();
        hideTemporaryWarningDialog();
        void openSiteConfig();
        if (siteConfigKeyNode) {
          siteConfigKeyNode.value = warning.site_key || "esjzone";
          populateSiteConfigForm(siteConfigKeyNode.value);
        }
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
  list.innerHTML = "";
  items.forEach((warning) => {
    const item = typeof warning === "string" ? { message: warning, level: "info" } : warning;
    list.appendChild(createSiteWarningCard(item));
  });
  dialog.hidden = false;
  if (temporaryWarningDialogTimer) window.clearTimeout(temporaryWarningDialogTimer);
  temporaryWarningDialogTimer = window.setTimeout(hideTemporaryWarningDialog, 5000);
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
  const title = document.createElement("h2");
  title.id = "temporaryWarningHeading";
  title.textContent = "临时提示";
  const close = document.createElement("button");
  close.type = "button";
  close.className = "temporary-warning-close";
  close.setAttribute("aria-label", "关闭临时提示");
  close.textContent = "×";
  close.addEventListener("click", hideTemporaryWarningDialog);
  head.append(title, close);

  const list = document.createElement("div");
  list.className = "temporary-warning-list";

  modal.append(head, list);
  dialog.appendChild(modal);
  document.body.appendChild(dialog);
  return dialog;
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
const searchForm = document.getElementById("searchForm");
const statusNode = document.getElementById("status");
const warningsNode = document.getElementById("warnings");
const resultsNode = document.getElementById("results");
const resultCountNode = document.getElementById("resultCount");
const resultMetaNode = document.getElementById("resultMeta");
const pageIndicatorNode = document.getElementById("pageIndicator");
const prevPageButton = document.getElementById("prevPage");
const nextPageButton = document.getElementById("nextPage");
const taskCountNode = document.getElementById("taskCount");
const taskTabCountNode = document.getElementById("taskTabCount");
const sourceSummaryNode = document.getElementById("sourceSummary");
const sourceSelectorNode = document.getElementById("sourceSelector");
const sourceTagFiltersNode = document.getElementById("sourceTagFilters");
const clearTagFiltersButton = document.getElementById("clearTagFilters");
const tasksNode = document.getElementById("tasks");
const tasksClearFinishedButton = document.getElementById("tasksClearFinished");
const searchTabButton = document.getElementById("searchTabButton");
const bookshelfTabButton = document.getElementById("bookshelfTabButton");
const bookshelfTabCountNode = document.getElementById("bookshelfTabCount");
const historyTabButton = document.getElementById("historyRailButton");
const historyTabCountNode = document.getElementById("historyTabCount");
const tasksTabButton = document.getElementById("tasksTabButton");
const searchTabPanel = document.getElementById("searchTabPanel");
const bookshelfTabPanel = document.getElementById("bookshelfTabPanel");
const historyTabPanel = document.getElementById("historyTabPanel");
const historyListNode = document.getElementById("historyList");
const historyStatusNode = document.getElementById("historyStatus");
const historyRefreshButton = document.getElementById("historyRefresh");
const tasksTabPanel = document.getElementById("tasksTabPanel");
const bookshelfNode = document.getElementById("bookshelf");
const bookshelfBreadcrumbNode = document.getElementById("bookshelfBreadcrumb");
const bookshelfStatusNode = document.getElementById("bookshelfStatus");
const bookshelfNewFolderButton = document.getElementById("bookshelfNewFolder");
const selectAllSourcesButton = document.getElementById("selectAllSources");
const clearSourcesButton = document.getElementById("clearSources");
const speedTestSourcesButton = document.getElementById("speedTestSources");
const speedTestStatusNode = document.getElementById("speedTestStatus");
const sourceFiltersNode = document.getElementById("sourceFilters");
const sourceFiltersToggleButton = document.getElementById("sourceFiltersToggle");
const sourceFiltersToggleSummaryNode = document.getElementById("sourceFiltersToggleSummary");
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
const generalBlurWebImagesLabelNode = generalBlurWebImagesNode ? generalBlurWebImagesNode.nextElementSibling : null;
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
  if (bookshelfTabButton) bookshelfTabButton.addEventListener("click", () => activateTab("bookshelf"));
  if (historyTabButton) historyTabButton.addEventListener("click", () => activateTab("history"));
  tasksTabButton.addEventListener("click", () => activateTab("tasks"));

  if (historyRefreshButton) historyRefreshButton.addEventListener("click", () => void loadHistory());

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

  clearTagFiltersButton.addEventListener("click", () => {
    if (appState.selectedSourceTags.size === 0) return;
    appState.selectedSourceTags = new Set();
    saveSearchSourceState();
    renderSourceTagFilters();
    renderSourceSelector();
    setStatus("已清空渠道标签筛选。");
  });

  setupSourceFiltersToggle();

  prevPageButton.addEventListener("click", async () => {
    if (!appState.hasPrev || !appState.lastKeyword) return;
    appState.page -= 1;
    await performSearch();
  });

  nextPageButton.addEventListener("click", async () => {
    if (!appState.hasNext || !appState.lastKeyword) return;
    appState.page += 1;
    await performSearch();
  });

  searchForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    appState.page = 1;
    await performSearch();
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
    try { await saveSiteConfig(); } catch (error) { setStatus(`保存站点配置失败：${error.message}`); }
  });
  
  generalConfigForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    try { await saveGeneralConfig(); } catch (error) { setStatus(`保存全局配置失败：${error.message}`); }
  });
  
  // 切换密码显示小眼睛图标
  toggleSitePasswordButton.addEventListener("click", () => {
    const reveal = sitePasswordNode.type === "password";
    sitePasswordNode.type = reveal ? "text" : "password";
    toggleSitePasswordButton.innerHTML = reveal 
      ? `<svg viewBox="0 0 24 24" width="20" height="20" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"></path><line x1="1" y1="1" x2="23" y2="23"></line></svg>` 
      : `<svg viewBox="0 0 24 24" width="20" height="20" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path><circle cx="12" cy="12" r="3"></circle></svg>`;
  });
  
  document.addEventListener("keydown", (event) => {
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
}

function activateTab(tabName) {
  if (tabName !== "search" && tabName !== "bookshelf" && tabName !== "history" && tabName !== "tasks") tabName = "search";
  appState.activeTab = tabName;
  searchTabButton.classList.toggle("is-active", tabName === "search");
  if (bookshelfTabButton) bookshelfTabButton.classList.toggle("is-active", tabName === "bookshelf");
  if (historyTabButton) historyTabButton.classList.toggle("is-active", tabName === "history");
  tasksTabButton.classList.toggle("is-active", tabName === "tasks");
  searchTabPanel.classList.toggle("is-active", tabName === "search");
  if (bookshelfTabPanel) bookshelfTabPanel.classList.toggle("is-active", tabName === "bookshelf");
  if (historyTabPanel) historyTabPanel.classList.toggle("is-active", tabName === "history");
  tasksTabPanel.classList.toggle("is-active", tabName === "tasks");
  if (tabName === "bookshelf" && !appState.bookshelf.loading) {
    // 每次切到书架都自动刷新，确保任务/外部修改后内容始终最新
    void loadBookshelf(appState.bookshelf.parentId);
  }
  if (tabName === "history" && !appState.history.loaded) {
    void loadHistory();
  }
}

async function performSearch() {
  const keyword = keywordInput.value.trim();
  if (!keyword) return setStatus("请输入关键词。");
  if (appState.selectedSites.size === 0) return setStatus("请至少选择一个渠道。");

  closeDetail();
  appState.lastKeyword = keyword;
  activateTab("search");
  renderWarnings([]);
  setStatus(`正在搜索“${keyword}”，第 ${appState.page} 页...`);

  try {
    const response = await fetch(`${root}/api/search`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        keyword, scope: "all", sites: Array.from(appState.selectedSites),
        page: appState.page, page_size: appState.pageSize,
      }),
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

    const warnings = payload.warnings || [];
    renderResults(appState.results);
    renderWarnings(warnings);
    renderPaging();
    renderResultMeta();

    if (!appState.results.length) {
      const suffix = warnings.length ? `；${temporaryWarningSummary(warnings)}` : "";
      return setStatus(`没有搜索到“${keyword}”，可以换关键词、减少渠道标签筛选，或直接粘贴小说链接${suffix}。`, "empty");
    }
    const warningSuffix = warnings.length ? `，${warnings.length} 个渠道临时跳过` : "";
    setStatus(`当前显示第 ${appState.page} 页，共 ${totalLabel(appState.total, appState.totalExact)} 条结果${warningSuffix}。`);
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

    const speedBadge = renderSourceSpeedBadge(source.key);
    if (speedBadge) tags.appendChild(speedBadge);

    button.appendChild(title);
    button.appendChild(key);
    button.appendChild(tags);
    button.addEventListener("click", () => toggleSource(source.key));
    sourceSelectorNode.appendChild(button);
  });
  sourceSummaryNode.textContent = sourceSummaryText(visibleSources.length);
  refreshSourceFiltersToggleSummary();
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

async function runVersionCheck() {
  if (!versionCheckButton) return;
  const mirror = versionMirrorNode ? versionMirrorNode.value : "";
  versionCheckButton.disabled = true;
  versionCheckButton.classList.add("is-loading");
  setVersionResult({ kind: "info", text: "正在检查最新版本..." });
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
      setVersionResult({ kind: "error", text: `检查失败：${payload.error}` });
      return;
    }
    renderVersionResult(payload);
  } catch (error) {
    setVersionResult({ kind: "error", text: `检查失败：${error.message}` });
  } finally {
    versionCheckButton.disabled = false;
    versionCheckButton.classList.remove("is-loading");
  }
}

function renderVersionResult(payload) {
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
  warnings.forEach((warning) => {
    const message = formatSearchWarning(warning);
    if (isTemporaryWarningText(message)) {
      temporaryWarnings.push(message);
      return;
    }
    const node = document.createElement("div");
    node.className = "warning-item";
    node.textContent = message;
    warningsNode.appendChild(node);
  });
  warningsNode.classList.toggle("is-empty", warningsNode.children.length === 0);
  if (temporaryWarnings.length) showTemporaryWarningDialog(temporaryWarnings);
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
    reason = "临时提示：站点响应超时，已跳过该渠道，不影响其它渠道结果。";
  } else if (lower.includes("http 403")) {
    reason = "临时提示：站点返回 403，可能触发访问限制/反爬，已跳过该渠道。";
  }
  if (warning.site === "n8novel" && !reason.includes("无限轻小说")) {
    reason += " 无限轻小说近期较容易出现 403，可稍后重试。";
  }
  return `${site}：${reason}`;
}

function temporaryWarningSummary(warnings) {
  const labels = warnings.slice(0, 3).map((warning) => sourceLabel(warning.site));
  const rest = warnings.length > labels.length ? `${labels.join("、")} 等 ${warnings.length} 个渠道` : labels.join("、");
  return `${rest}临时不可用，已显示其它可用渠道结果`;
}

function renderResults(results) {
  resultsNode.innerHTML = "";
  if (!results.length) return resultsNode.appendChild(createEmptyState("当前页没有可展示的结果。"));

  results.forEach((result) => {
    const card = document.createElement("article");
    card.className = "result-card";

    const coverButton = document.createElement("button");
    coverButton.type = "button";
    coverButton.className = "result-cover-button";
    coverButton.setAttribute("aria-label", `查看 ${displayResultTitle(result)} 的详情`);
    coverButton.appendChild(createCoverImage(result.cover_url, displayResultTitle(result), "result-cover"));

    const overlay = document.createElement("span");
    overlay.className = "result-cover-overlay";
    overlay.textContent = "查看详情";
    coverButton.appendChild(overlay);
    coverButton.addEventListener("click", () => openDetail(result, result.primary));

    const body = document.createElement("div");
    body.className = "result-body";

    const title = document.createElement("h3");
    title.className = "result-title";
    const sourceURL = displayResultURL(result);
    if (sourceURL) {
      const titleLink = document.createElement("a");
      titleLink.className = "result-title-link";
      titleLink.href = sourceURL;
      titleLink.target = "_blank";
      titleLink.rel = "noopener noreferrer";
      titleLink.textContent = displayResultTitle(result);
      title.appendChild(titleLink);
    } else {
      title.textContent = displayResultTitle(result);
    }

    const author = document.createElement("p");
    author.className = "result-author";
    author.textContent = `作者：${displayResultAuthor(result)}`;

    const source = document.createElement("p");
    source.className = "result-source";
    source.textContent = `源：${sourceLabel(result.preferred_site)}`;

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
  const detailPanel = detailContentNode.closest(".detail-panel");
  if (detailPanel && resetScroll) detailPanel.scrollTop = 0;

  const cached = appState.detailCache.get(cacheKey);
  if (cached) return renderDetail(result, activeVariant, cached, false, "");

  renderDetail(result, activeVariant, null, true, "");
  void loadDetail(result, activeVariant, cacheKey, chapterPage, pageSize);
}

function closeDetail() {
  detailOverlay.hidden = true; document.body.classList.remove("has-overlay");
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
  })).filter((item) => item.variant && item.variant.site && item.variant.book_id && shouldWarmupDetail(item.variant.site) && !appState.detailCache.has(detailRequestKey(item.variant, 1, normalizedChapterPageSize())));
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

  if (variants.length > 1) {
    const switchWrap = document.createElement("div");
    switchWrap.className = "detail-source-switch";
    variants.forEach((item) => {
      const button = document.createElement("button");
      button.type = "button"; button.className = "detail-source-button";
      if (detailKey(item) === detailKey(activeVariant)) button.classList.add("is-active");
      button.textContent = sourceLabel(item.site);
      button.addEventListener("click", () => openDetail(result, item));
      switchWrap.appendChild(button);
    });
    summary.appendChild(switchWrap);
  }

  const actions = document.createElement("div");
  actions.className = "detail-actions";
  const downloadButton = document.createElement("button");
  downloadButton.type = "button";
  downloadButton.className = "download-button";
  downloadButton.textContent = "下载到本地";
  downloadButton.addEventListener("click", () => void startDownloadTask({ site: activeVariant.site, book_id: activeVariant.book_id }, downloadButton));
  actions.appendChild(downloadButton);

  const exportButton = document.createElement("button");
  exportButton.type = "button";
  exportButton.className = "tool-button";
  exportButton.textContent = "导出";
  exportButton.title = "导出已下载到服务器本地的文件";
  exportButton.addEventListener("click", () => void startDownloadTask({ site: activeVariant.site, book_id: activeVariant.book_id }, exportButton, { target: "export" }));
  actions.appendChild(exportButton);

  const addShelfButton = document.createElement("button");
  addShelfButton.type = "button";
  addShelfButton.className = "tool-button";
  addShelfButton.textContent = "加入书架";
  addShelfButton.addEventListener("click", () => void addCurrentDetailToBookshelf(result, activeVariant, detail, addShelfButton));
  actions.appendChild(addShelfButton);

  summary.appendChild(actions);

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
    if (expectedTotal > fallbackChapters.length) setStatus("正在加载完整章节目录，用于阅读器显示全书章节...");
    const chapters = await loadReaderCatalog(activeVariant, chapterPage, fallbackChapters);
    const readerIndex = resolveReaderChapterIndex(chapters, clickedChapter, chapterIndex);
    openReader(chapters, readerIndex);
  } catch (error) {
    setStatus(`完整章节目录加载失败：${error.message}，先打开当前分页。`, "warning");
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
const readerCloseButton = document.getElementById("readerCloseButton");
const readerDetailButton = document.getElementById("readerDetailButton");
const readerCatalogButton = document.getElementById("readerCatalogButton");
const readerCatalogCloseButton = document.getElementById("readerCatalogCloseButton");
const readerCatalogPanel = document.getElementById("readerCatalogPanel");
const readerCatalogList = document.getElementById("readerCatalogList");
const readerTitle = document.getElementById("readerTitle");
const readerContent = document.getElementById("readerContent");
const readerBody = document.getElementById("readerBody");
const readerProgress = document.getElementById("readerProgress");

const readerState = { chapters: [], loadedMin: 0, loadedMax: -1, currentIndex: 0, loadingUp: false, loadingDown: false, canAutoLoad: false, token: 0, cache: new Map(), pending: new Map() };

function openReader(chapters, index) {
  readerState.chapters = chapters;
  readerState.loadedMin = index;
  readerState.loadedMax = index - 1;
  readerState.currentIndex = index;
  readerState.loadingUp = false;
  readerState.loadingDown = false;
  readerState.canAutoLoad = false;
  const token = ++readerState.token;
  readerOverlay.hidden = false;
  hideReaderCatalog();

  document.body.classList.add("has-overlay");
  readerContent.innerHTML = "";
  setReaderScrollLocked(true);
  setReaderScrollTop(0);
  applyReaderSettings(loadReaderSettings());
  updateReaderTitle(index);
  renderReaderWindow(index).then(() => {
    if (token !== readerState.token || readerOverlay.hidden) return;
    setReaderScrollTop(readerChapterTop(index));
    requestAnimationFrame(() => {
      if (token !== readerState.token || readerOverlay.hidden) return;
      setReaderScrollLocked(false);
      readerState.canAutoLoad = true;
    });
  });
}

async function renderReaderWindow(centerIndex) {
  const min = Math.max(centerIndex - 3, 0);
  const max = Math.min(centerIndex + 3, readerState.chapters.length - 1);
  const indices = [];
  for (let i = min; i <= max; i++) indices.push(i);

  readerContent.innerHTML = "";
  readerContent.appendChild(createReaderBoundaryHint(min > 0 ? "正在预加载上方章节" : "已经是第一章", min > 0));
  const loading = document.createElement("div");
  loading.className = "reader-loading";
  loading.textContent = "正在加载当前章节和前后 3 章...";
  readerContent.appendChild(loading);
  readerContent.appendChild(createReaderBoundaryHint(max < readerState.chapters.length - 1 ? "正在预加载下方章节" : "已经是最后一章", max < readerState.chapters.length - 1));

  await Promise.allSettled(indices.map((i) => fetchChapterContentForReader(readerState.chapters[i], i)));

  readerState.loadedMin = min;
  readerState.loadedMax = min - 1;
  readerContent.innerHTML = "";

  const topHint = createReaderBoundaryHint(min > 0 ? "正在预加载上方章节" : "已经是第一章", min > 0);
  readerContent.appendChild(topHint);

  const tasks = [];
  indices.forEach((i) => tasks.push(appendChapter(i)));

  const bottomHint = createReaderBoundaryHint(max < readerState.chapters.length - 1 ? "正在预加载下方章节" : "已经是最后一章", max < readerState.chapters.length - 1);
  readerContent.appendChild(bottomHint);

  await Promise.allSettled(tasks);
  topHint.textContent = min > 0 ? `已预加载上方 ${centerIndex - min} 章` : "已经是第一章";
  bottomHint.textContent = max < readerState.chapters.length - 1 ? `已预加载下方 ${max - centerIndex} 章` : "已经是最后一章";
  topHint.classList.remove("is-loading");
  bottomHint.classList.remove("is-loading");
  preloadCache(centerIndex, 3);
}

function createReaderBoundaryHint(text, loading) {
  const node = document.createElement("div");
  node.className = `reader-boundary${loading ? " is-loading" : ""}`;
  node.textContent = text;
  return node;
}

function readerChapterTop(index) {
  const node = readerContent.querySelector(`[data-chapter-index="${index}"]`);
  return node ? Math.max(0, node.offsetTop - 16) : 0;
}

function setReaderScrollTop(top) {
  const previous = readerBody.style.scrollBehavior;
  readerBody.style.scrollBehavior = "auto";
  readerBody.scrollTop = top;
  requestAnimationFrame(() => { readerBody.style.scrollBehavior = previous; });
}

function setReaderScrollLocked(locked) {
  readerBody.style.overflowY = locked ? "hidden" : "";
}

async function loadChaptersSequential(indices, direction) {
  for (const i of indices) {
    if (direction === "down") await appendChapter(i);
    else await prependChapter(i);
  }
}

function closeReader() {
  flushProgressReport();
  hideReaderCatalog();
  readerOverlay.hidden = true;
  document.body.classList.remove("has-overlay");
  setReaderScrollLocked(false);
  readerState.canAutoLoad = false;
  readerState.token += 1;
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
    appState.reader.lastReportedChapterID = "";
  }
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
  renderReaderCatalog();
  readerCatalogPanel.hidden = false;
  readerCatalogButton.setAttribute("aria-expanded", "true");
}

function hideReaderCatalog() {
  if (readerCatalogPanel) readerCatalogPanel.hidden = true;
  if (readerCatalogButton) readerCatalogButton.setAttribute("aria-expanded", "false");
}

function renderReaderCatalog() {
  if (!readerCatalogList) return;
  readerCatalogList.innerHTML = "";
  readerState.chapters.forEach((chapter, index) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "reader-catalog-item";
    if (index === readerState.currentIndex) button.classList.add("is-active");
    button.textContent = `${index + 1}. ${chapter.title || `第 ${index + 1} 章`}`;
    button.addEventListener("click", () => jumpReaderToChapter(index));
    readerCatalogList.appendChild(button);
  });
}

function renderReaderCatalogActive(index) {
  if (!readerCatalogList || !readerCatalogPanel || readerCatalogPanel.hidden) return;
  const nodes = readerCatalogList.querySelectorAll(".reader-catalog-item");
  nodes.forEach((node, idx) => node.classList.toggle("is-active", idx === index));
}

function jumpReaderToChapter(index) {
  if (!readerState.chapters.length) return;
  const next = Math.min(Math.max(0, index), readerState.chapters.length - 1);
  hideReaderCatalog();
  openReader(readerState.chapters, next);
}

async function appendChapter(idx) {
  if (idx >= readerState.chapters.length || idx <= readerState.loadedMax) return;
  readerState.loadedMax = idx;
  const ch = readerState.chapters[idx];
  const divider = document.createElement("div");
  divider.className = "reader-chapter-divider"; divider.dataset.chapterIndex = idx;
  divider.textContent = ch.title || `第 ${idx + 1} 章`;
  readerContent.appendChild(divider);
  const block = document.createElement("div");
  block.className = "reader-chapter-block"; block.dataset.chapterIndex = idx;
  readerContent.appendChild(block);
  await loadChapterContent(ch, idx, block);
}

async function prependChapter(idx) {
  if (idx < 0 || idx >= readerState.loadedMin) return;
  readerState.loadedMin = idx;
  const ch = readerState.chapters[idx];
  const block = document.createElement("div");
  block.className = "reader-chapter-block"; block.dataset.chapterIndex = idx;
  const divider = document.createElement("div");
  divider.className = "reader-chapter-divider"; divider.dataset.chapterIndex = idx;
  divider.textContent = ch.title || `第 ${idx + 1} 章`;
  const prevScrollHeight = readerBody.scrollHeight;
  readerContent.prepend(block);
  readerContent.prepend(divider);
  await loadChapterContent(ch, idx, block);
  // Maintain scroll position
  readerBody.scrollTop += readerBody.scrollHeight - prevScrollHeight;
}

async function loadChapterContent(ch, index, block) {
  const cached = readerState.cache.get(chapterReaderCacheKey(ch, index));
  if (cached) { renderChapterBlock(block, cached); return; }
  block.innerHTML = '<div class="reader-loading">正在加载...</div>';
  try {
    const content = await fetchChapterContentForReader(ch, index);
    renderChapterBlock(block, content);
  } catch (e) { block.innerHTML = `<div class="reader-error">加载失败：${e.message}</div>`; }
}

function renderChapterBlock(block, text) {
  block.innerHTML = "";
  if (!text.trim()) { block.innerHTML = '<div class="reader-error">章节内容为空</div>'; return; }
  text.split(/\n/).forEach(line => {
    if (!line.trim()) return;
    const p = document.createElement("p"); p.textContent = line; block.appendChild(p);
  });
}

async function fetchChapterContentForReader(ch, index) {
  const key = chapterReaderCacheKey(ch, index);
  const cached = readerState.cache.get(key);
  if (cached) return cached;
  const pending = readerState.pending.get(key);
  if (pending) return pending;
  const variant = appState.activeDetailVariant || (appState.detailResult && appState.detailResult.primary) || {};
  const site = variant.site || ""; const bookID = variant.book_id || "";
  if (!site || !bookID) throw new Error("缺少站点信息");
  const task = fetchChapterContentText(site, bookID, ch).then((content) => {
    readerState.cache.set(key, content);
    return content;
  }).finally(() => {
    readerState.pending.delete(key);
  });
  readerState.pending.set(key, task);
  return task;
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
  return (data.chapter && data.chapter.content) || "";
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
    if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
    return data;
  } catch (error) {
    if (error && error.name === "AbortError") throw new Error(timeoutMessage || "请求超时");
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

function chapterReaderCacheKey(ch, index) {
  return ch.id || ch.url || `${index}:${ch.title || ""}`;
}

function preloadCache(centerIndex, radius = 2) {
  const variant = appState.activeDetailVariant || (appState.detailResult && appState.detailResult.primary) || {};
  const site = variant.site || ""; const bookID = variant.book_id || "";
  if (!site || !bookID) return;
  for (let offset = -radius; offset <= radius; offset++) {
    const i = centerIndex + offset;
    if (i < 0 || i >= readerState.chapters.length) continue;
    const ch = readerState.chapters[i];
    const key = chapterReaderCacheKey(ch, i);
    if (readerState.cache.has(key) || readerState.pending.has(key)) continue;
    void fetchChapterContentForReader(ch, i).catch(() => {});
  }
}

readerCloseButton.addEventListener("click", closeReader);
if (readerDetailButton) readerDetailButton.addEventListener("click", openReaderDetail);
if (readerCatalogButton) readerCatalogButton.addEventListener("click", toggleReaderCatalog);
if (readerCatalogCloseButton) readerCatalogCloseButton.addEventListener("click", hideReaderCatalog);

readerBody.addEventListener("scroll", () => {
  if (!readerState.canAutoLoad) return;
  const blocks = readerContent.querySelectorAll("[data-chapter-index]");
  for (let i = blocks.length - 1; i >= 0; i--) {
    const rect = blocks[i].getBoundingClientRect();
    const bodyRect = readerBody.getBoundingClientRect();
    if (rect.top <= bodyRect.top + 60) {
      const visIdx = parseInt(blocks[i].dataset.chapterIndex, 10);
      updateReaderTitle(visIdx);
      preloadCache(visIdx, 3);
      break;
    }
  }
});

// Reader background color picker
const readerThemes = [
  { key: "paper", label: "米纸", bg: "#f8f2e6", text: "#4b3f35" },
  { key: "white", label: "白色", bg: "#ffffff", text: "#243044" },
  { key: "mint", label: "浅青", bg: "#eefaf5", text: "#243b3f" },
  { key: "blue", label: "浅蓝", bg: "#eef6ff", text: "#27364a" },
  { key: "rose", label: "浅粉", bg: "#fff1f2", text: "#4a2d35" },
  { key: "night", label: "夜间", bg: "#111827", text: "#d1d5db", night: true },
];

function initReaderSettings() {
  const picker = document.getElementById("readerBgPicker");
  if (!picker) return;
  picker.innerHTML = "";
  const settings = loadReaderSettings();

  readerThemes.forEach((theme) => {
    const dot = document.createElement("button");
    dot.type = "button";
    dot.className = "reader-bg-dot" + (theme.key === settings.theme ? " is-active" : "");
    dot.style.background = theme.bg;
    dot.title = theme.label;
    dot.setAttribute("aria-label", `阅读背景：${theme.label}`);
    dot.addEventListener("click", () => {
      const next = { ...loadReaderSettings(), theme: theme.key };
      saveReaderSettings(next);
      applyReaderSettings(next);
      renderReaderThemeActive();
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
    const next = { ...current, theme: current.theme === "night" ? "paper" : "night" };
    saveReaderSettings(next);
    applyReaderSettings(next);
    renderReaderThemeActive();
  });
  picker.appendChild(nightButton);
  applyReaderSettings(settings);
}

function renderReaderThemeActive() {
  const settings = loadReaderSettings();
  const picker = document.getElementById("readerBgPicker");
  if (!picker) return;
  picker.querySelectorAll(".reader-bg-dot").forEach((dot, index) => {
    dot.classList.toggle("is-active", readerThemes[index] && readerThemes[index].key === settings.theme);
  });
}

function loadReaderSettings() {
  const fallback = { theme: "paper", fontSize: 18 };
  try {
    const parsed = JSON.parse(localStorage.getItem("reader-settings") || "{}");
    const theme = readerThemes.some((item) => item.key === parsed.theme) ? parsed.theme : fallback.theme;
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
  const theme = readerThemes.find((item) => item.key === settings.theme) || readerThemes[0];
  readerOverlay.style.setProperty("--reader-bg", theme.bg);
  readerOverlay.style.setProperty("--reader-text", theme.text);
  readerContent.style.fontSize = `${settings.fontSize || 18}px`;
  readerOverlay.classList.toggle("is-night", Boolean(theme.night));
}

initReaderSettings();

function resultBadge(text) {
  const node = document.createElement("span"); node.className = "result-badge"; node.textContent = text; return node;
}

function renderPaging() {
  resultCountNode.textContent = appState.lastKeyword ? totalLabel(appState.total, appState.totalExact) : "0";
  pageIndicatorNode.textContent = `第 ${appState.page} 页 · 每页 ${appState.pageSize} 本`;
  prevPageButton.disabled = !appState.hasPrev; nextPageButton.disabled = !appState.hasNext;
}

function renderResultMeta() {
  if (!appState.lastKeyword) return resultMetaNode.textContent = "输入关键词后开始搜索。";
  if (!appState.results.length) return resultMetaNode.textContent = `关键词“${appState.lastKeyword}”暂无结果。`;
  const start = (appState.page - 1) * appState.pageSize + 1;
  const end = start + appState.results.length - 1;
  resultMetaNode.textContent = `关键词“${appState.lastKeyword}”当前显示 ${start}-${end}，共 ${totalLabel(appState.total, appState.totalExact)} 条。`;
}

async function startDownloadTask(target, button, options = {}) {
  const site = target.site || (target.primary && target.primary.site);
  const bookID = target.book_id || (target.primary && target.primary.book_id);
  if (!site || !bookID) return setStatus("下载目标缺失。");

  const taskTarget = options.target === "export" ? "export" : "local";
  const originalText = button ? button.textContent : "";
  if (button) { button.disabled = true; button.textContent = taskTarget === "export" ? "正在导出..." : "正在下载..."; }

  try {
    const body = { site, book_id: bookID, target: taskTarget };
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
    if (!options.noActivate) activateTab("tasks");
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

function renderTasks() {
  const tasks = Array.from(appState.tasks.values()).sort((l, r) => new Date(r.updated_at).getTime() - new Date(l.updated_at).getTime());
  if (taskCountNode) taskCountNode.textContent = String(tasks.length);
  if (taskTabCountNode) taskTabCountNode.textContent = String(tasks.length);
  tasksNode.innerHTML = "";

  if (!tasks.length) return tasksNode.appendChild(createEmptyState("还没有下载任务。", true));

  tasks.forEach((task) => {
    tasksNode.appendChild(buildTaskCard(task));
  });
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

  // Head: title (left) + status pill + action (right)
  const head = document.createElement("div");
  head.className = "task-head";
  const title = document.createElement("div");
  title.className = "task-title";
  title.textContent = task.title || `${sourceLabel(task.site)}/${task.book_id}`;
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
  card.appendChild(head);

  // Meta chips
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
  card.appendChild(chips);

  // Progress bar
  if (hasProgress) {
    const wrap = document.createElement("div");
    wrap.className = "task-progress-row";
    const bar = document.createElement("div");
    bar.className = "task-progress-bar";
    const fill = document.createElement("div");
    fill.className = "task-progress-fill";
    fill.style.width = `${percent}%`;
    bar.appendChild(fill);
    wrap.appendChild(bar);
    const count = document.createElement("span");
    count.className = "task-progress-count";
    count.textContent = `${task.completed_chapters || 0} / ${task.total_chapters}`;
    wrap.appendChild(count);
    card.appendChild(wrap);
  }

  // Error
  if (task.error) {
    const error = document.createElement("div");
    error.className = "task-error";
    error.textContent = task.error;
    card.appendChild(error);
  }

  // Exported files as chips
  if (Array.isArray(task.exported) && task.exported.length) {
    const files = document.createElement("div");
    files.className = "task-files";
    task.exported.forEach((p) => {
      const link = document.createElement("a");
      link.className = "task-file-chip";
      link.href = `${root}/api/download-file?path=${encodeURIComponent(p)}`;
      link.title = p;
      link.download = "";
      const basename = p.split(/[/\\]/).pop() || p;
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
    card.appendChild(files);
  }

  // Messages: last 2 only
  if (Array.isArray(task.messages) && task.messages.length) {
    const messages = document.createElement("div");
    messages.className = "task-messages";
    task.messages.slice(-2).forEach((msg) => {
      const item = document.createElement("div");
      item.className = `task-message level-${msg.level}`;
      item.textContent = msg.text;
      messages.appendChild(item);
    });
    card.appendChild(messages);
  }

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
    void startDownloadTask({ site: task.site, book_id: task.book_id }, confirm, {
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
  renderSiteConfigHelp(siteKey);
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
  siteConfigHintNode.textContent = "";
  siteCookieHelpNode.textContent = "";
}

async function openSiteConfig() {
  siteConfigOverlay.hidden = false;
  document.body.classList.add("has-overlay");
  try {
    await Promise.all([loadGeneralConfig(), loadSiteConfigs()]);
  } catch (error) {
    setStatus(`设置刷新失败：${error.message}`);
  }
}
function closeSiteConfig() { siteConfigOverlay.hidden = true; document.body.classList.remove("has-overlay"); }

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
  if (payload.item && payload.item.web_page_size) {
    appState.pageSize = Math.max(1, Number.parseInt(String(payload.item.web_page_size), 10) || defaultPageSize);
    renderPaging();
    renderResultMeta();
  }
  renderGeneralConfigForm(appState.generalConfig);
}

async function saveGeneralConfig() {
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
  appState.pageSize = Math.max(1, Number.parseInt(String(appState.generalConfig.web_page_size || payload.web_page_size || defaultPageSize), 10) || defaultPageSize);
  appState.page = 1;
  appState.detailCache.clear();
  appState.detailPending.clear();
  appState.detailTimings.clear();
  appState.readerCatalogCache.clear();
  appState.readerCatalogPending.clear();
  readerState.cache.clear();
  readerState.pending.clear();
  renderGeneralConfigForm(appState.generalConfig);
  renderPaging();
  renderResultMeta();
  applyImageBlurSetting(appState.generalConfig);
  setStatus("已保存全局配置。后续搜索、详情和下载将使用最新参数。");
}

function applyImageBlurSetting(item) {
  document.body.classList.toggle("web-images-blurred", Boolean(item && item.blur_web_images));
}

async function saveSiteConfig() {
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
  readerState.cache.clear();
  readerState.pending.clear();
  siteWarnings = Array.isArray(data.site_warnings) ? data.site_warnings : [];
  siteStats = Array.isArray(data.site_stats) ? data.site_stats : [];
  renderSiteWarnings();
  setStatus(`已保存 ${sourceLabel(siteKey)} 配置。`);
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

async function loadBookshelf(parentId) {
  if (!bookshelfNode) return;
  const shelf = appState.bookshelf;
  shelf.loading = true;
  setBookshelfStatus("正在加载书架...");
  const query = parentId ? `?parent_id=${encodeURIComponent(parentId)}` : "";
  try {
    const response = await fetch(`${root}/api/bookshelf/items${query}`);
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || "load bookshelf failed");
    shelf.parentId = parentId || null;
    shelf.items = Array.isArray(payload.items) ? payload.items : [];
    shelf.breadcrumb = Array.isArray(payload.breadcrumb) ? payload.breadcrumb : [];
    shelf.loaded = true;
    updateBookshelfFolderActions();
    rebuildBookshelfIndex();
    renderBookshelf();
    renderBookshelfBreadcrumb();
    const total = shelf.items.length;
    setBookshelfStatus(total === 0 ? "当前目录为空。" : `当前目录共 ${total} 项。`);
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
    const bookCount = Array.from(map.keys()).length;
    bookshelfTabCountNode.textContent = String(bookCount);
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
  bookshelfNode.innerHTML = "";
  const items = appState.bookshelf.items;
  if (!items.length) {
    bookshelfNode.appendChild(createEmptyState(appState.bookshelf.parentId ? "这个文件夹是空的。" : "书架还是空的。", true));
    return;
  }
  items.forEach((item) => {
    if (item.kind === "folder") {
      bookshelfNode.appendChild(renderBookshelfFolderCard(item));
    } else {
      bookshelfNode.appendChild(renderBookshelfBookCard(item));
    }
  });
}

function setHistoryStatus(text) {
  if (historyStatusNode) historyStatusNode.textContent = text;
}

async function loadHistory() {
  if (!historyListNode) return;
  const history = appState.history;
  if (history.loading) return;
  history.loading = true;
  setHistoryStatus("正在加载阅读历史...");
  try {
    const response = await fetch(`${root}/api/bookshelf/history?limit=100`);
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || "load history failed");
    history.items = Array.isArray(payload.items) ? payload.items : [];
    history.loaded = true;
    renderHistory();
    if (historyTabCountNode) historyTabCountNode.textContent = String(history.items.length);
    if (history.items.length === 0) {
      setHistoryStatus("尚未读过任何小说，先去书架点击书籍开始阅读吧。");
    } else {
      setHistoryStatus(`共 ${history.items.length} 条阅读记录。`);
    }
  } catch (error) {
    setHistoryStatus(`加载阅读历史失败：${error.message}`);
  } finally {
    history.loading = false;
  }
}

function renderHistory() {
  if (!historyListNode) return;
  historyListNode.innerHTML = "";
  const items = appState.history.items;
  if (!items.length) {
    historyListNode.appendChild(createEmptyState("还没有阅读记录。", true));
    return;
  }
  items.forEach((item) => historyListNode.appendChild(renderHistoryCard(item)));
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
  if (item.last_read_chapter_title) {
    meta.appendChild(resultBadge(`读到：${item.last_read_chapter_title}`));
  } else if (item.last_read_chapter_id) {
    meta.appendChild(resultBadge(`已读章节 ${item.last_read_chapter_index + 1 || ""}`.trim()));
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
  // History items have the same shape as bookshelf items, so reuse the bookshelf flow.
  await openBookshelfReader(item, null);
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
  if (chapterID === tracker.lastReportedChapterID) return;
  tracker.lastReportedChapterID = chapterID;
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
  downloadButton.addEventListener("click", () => void startDownloadTask({ site: item.site, book_id: item.book_id }, downloadButton));
  actions.appendChild(downloadButton);

  const exportButton = document.createElement("button");
  exportButton.type = "button";
  exportButton.className = "tool-button is-ghost";
  exportButton.textContent = "导出";
  exportButton.addEventListener("click", () => void startDownloadTask({ site: item.site, book_id: item.book_id }, exportButton, { target: "export" }));
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

async function addCurrentDetailToBookshelf(result, variant, detail, button, options = {}) {
  if (!result || !variant) return;
  const silent = Boolean(options && options.silent);
  const site = variant.site || (result.primary && result.primary.site);
  const bookID = variant.book_id || (result.primary && result.primary.book_id);
  if (!site || !bookID) {
    if (!silent) setStatus("加入书架失败：缺少站点或 book_id。");
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
  const progressView = !silent ? createInlineTaskView() : null;
  if (button) { button.disabled = true; button.textContent = "加入中..."; }
  try {
    const response = await fetch(`${root}/api/bookshelf/books`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || "add bookshelf failed");
    appState.bookshelf.loaded = false;
    if (progressView && button && button.parentElement && button.parentElement.parentElement) {
      button.parentElement.parentElement.insertBefore(progressView, button.parentElement.nextSibling);
    }
    if (!silent) setStatus(`已加入书架，开始下载到服务器本地：${data.item.title || data.item.book_id}`);
    await startDownloadTask({ site, book_id: bookID }, null, {
      inlineView: progressView,
      keepDetailOpen: true,
      noActivate: true,
    });
    if (button) { button.disabled = false; button.textContent = original; }
  } catch (error) {
    if (!silent) setStatus(`加入书架失败：${error.message}`);
    if (button) { button.disabled = false; button.textContent = original; }
  }
}

async function openBookshelfReader(item, button) {
  const original = button ? button.textContent : "";
  if (button) { button.disabled = true; button.textContent = "加载中..."; }
  try {
    if (!item.site || !item.book_id) throw new Error("缺少站点或 book_id");

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
    openReader(chapters, startIndex);
    const sourceTag = localOnly ? "本地缓存" : "在线";
    if (startIndex > 0) {
      const startChapter = chapters[startIndex];
      const label = (startChapter && startChapter.title) || `第 ${startIndex + 1} 章`;
      setStatus(`已跳到《${item.title || item.book_id}》上次阅读位置：${label}（${sourceTag}）`);
    } else {
      setStatus(`已打开《${item.title || item.book_id}》（${sourceTag}）`);
    }
  } catch (error) {
    setStatus(`加载阅读器失败：${error.message}`);
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
  tracker.lastReportedChapterID = "";
}
