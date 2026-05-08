const root = window.__NOVEL_DL__.root;
const defaultSources = window.__NOVEL_DL__.defaultSources || [];
const allSources = window.__NOVEL_DL__.allSources || [];
const configurableSiteKeys = ["novalpie", "esjzone"];
const defaultPageSize = window.__NOVEL_DL__.pageSize || 50;
const initialGeneralConfig = window.__NOVEL_DL__.generalConfig || {};
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
  selectedSites: new Set(defaultSources.map((source) => source.key)),
  selectedSourceTags: new Set(),
  tasks: new Map(),
  pollers: new Map(),
  detailCache: new Map(),
  detailPending: new Map(),
  detailTimings: new Map(),
  detailResult: null,
  activeDetailKey: "",
  activeDetailVariant: null,
  detailWarmupToken: 0,
  chapterPageSize: loadChapterPageSize(),
  chapterColumns: loadChapterColumns(),
  siteConfigs: new Map(),
  paramSupports: [],
  generalConfig: initialGeneralConfig,
};

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
  temporaryWarningDialogTimer = window.setTimeout(hideTemporaryWarningDialog, 9000);
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
const searchTabButton = document.getElementById("searchTabButton");
const tasksTabButton = document.getElementById("tasksTabButton");
const searchTabPanel = document.getElementById("searchTabPanel");
const tasksTabPanel = document.getElementById("tasksTabPanel");
const selectAllSourcesButton = document.getElementById("selectAllSources");
const clearSourcesButton = document.getElementById("clearSources");
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
const generalMaxConnectionsNode = document.getElementById("generalMaxConnections");
const generalMaxRPSNode = document.getElementById("generalMaxRPS");
const generalRetryTimesNode = document.getElementById("generalRetryTimes");
const generalBackoffFactorNode = document.getElementById("generalBackoffFactor");
const generalLocaleStyleNode = document.getElementById("generalLocaleStyle");
const generalFormatsNode = document.getElementById("generalFormats");
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
  tasksTabButton.addEventListener("click", () => activateTab("tasks"));

  selectAllSourcesButton.addEventListener("click", () => {
    const visibleSources = filteredSources();
    if (!visibleSources.length) return setStatus("当前标签筛选下没有可选择的渠道。");
    visibleSources.forEach((source) => appState.selectedSites.add(source.key));
    renderSourceSelector();
    setStatus(appState.selectedSourceTags.size > 0 ? `已选中当前筛选范围内的 ${visibleSources.length} 个渠道。` : `已选中全部 ${appState.selectedSites.size} 个渠道。`);
  });

  clearSourcesButton.addEventListener("click", () => {
    const visibleSources = filteredSources();
    if (!visibleSources.length) return setStatus("当前标签筛选下没有可清空的渠道。");
    visibleSources.forEach((source) => appState.selectedSites.delete(source.key));
    renderSourceSelector();
    setStatus(appState.selectedSourceTags.size > 0 ? `已清空当前筛选范围内的 ${visibleSources.length} 个渠道选择。` : "已清空渠道选择。");
  });

  clearTagFiltersButton.addEventListener("click", () => {
    if (appState.selectedSourceTags.size === 0) return;
    appState.selectedSourceTags = new Set();
    renderSourceTagFilters();
    renderSourceSelector();
    setStatus("已清空渠道标签筛选。");
  });

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
  appState.activeTab = tabName;
  const isSearch = tabName === "search";
  searchTabButton.classList.toggle("is-active", isSearch);
  tasksTabButton.classList.toggle("is-active", !isSearch);
  searchTabPanel.classList.toggle("is-active", isSearch);
  tasksTabPanel.classList.toggle("is-active", !isSearch);
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

    button.appendChild(title);
    button.appendChild(key);
    button.appendChild(tags);
    button.addEventListener("click", () => toggleSource(source.key));
    sourceSelectorNode.appendChild(button);
  });
  sourceSummaryNode.textContent = sourceSummaryText(visibleSources.length);
}

function toggleSource(siteKey) {
  if (appState.selectedSites.has(siteKey)) appState.selectedSites.delete(siteKey);
  else appState.selectedSites.add(siteKey);
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
  } else if (warning.site === "alicesw") {
    reason = `临时提示：爱丽丝书屋源近期不稳定，建议暂时取消该渠道或使用书籍直链。${error ? ` 原因：${error}` : ""}`;
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
  const activeVariant = variant || result.primary;
  const cacheKey = detailKey(activeVariant);
  appState.detailResult = result;
  appState.activeDetailVariant = activeVariant;
  appState.activeDetailKey = cacheKey;
  detailOverlay.hidden = false;
  document.body.classList.add("has-overlay");
  const detailPanel = detailContentNode.closest(".detail-panel");
  if (detailPanel) detailPanel.scrollTop = 0;

  const cached = appState.detailCache.get(cacheKey);
  if (cached) return renderDetail(result, activeVariant, cached, false, "");

  renderDetail(result, activeVariant, null, true, "");
  void loadDetail(result, activeVariant, cacheKey);
}

function closeDetail() {
  detailOverlay.hidden = true; document.body.classList.remove("has-overlay");
}

async function loadDetail(result, variant, cacheKey) {
  const startedAt = performance.now();
  try {
    let pending = appState.detailPending.get(cacheKey);
    if (!pending) {
      pending = fetchBookDetail(variant).finally(() => {
        appState.detailPending.delete(cacheKey);
      });
      appState.detailPending.set(cacheKey, pending);
    }
    const book = await pending;
    if (!book) throw new Error("未返回详情数据");

    appState.detailCache.set(cacheKey, book);
    appState.detailTimings.set(cacheKey, Math.max(1, Math.round(performance.now() - startedAt)));
    if (appState.detailResult === result && appState.activeDetailKey === cacheKey) {
      renderDetail(result, variant, book, false, "");
    }
  } catch (error) {
    if (appState.detailResult === result && appState.activeDetailKey === cacheKey) {
      renderDetail(result, variant, null, false, error.message);
    }
  }
}

async function fetchBookDetail(variant) {
  const url = new URL(`${window.location.origin}${root}/api/books/detail`);
  url.searchParams.set("site", variant.site);
  url.searchParams.set("book_id", variant.book_id);
  const payload = await fetchJSONWithTimeout(
    url.toString(),
    {},
    detailLoadTimeoutMs(variant.site),
    `${sourceLabel(variant.site)} 详情/章节目录加载超时，请稍后重试或暂时切换来源`,
  );
  return payload.book || null;
}

async function warmupDetailCache(results) {
  const token = ++appState.detailWarmupToken;
  const queue = results.slice(0, 6).map((result) => ({
    result,
    variant: result.primary,
  })).filter((item) => item.variant && item.variant.site && item.variant.book_id && !appState.detailCache.has(detailKey(item.variant)));
  const workers = Math.min(2, queue.length);
  let cursor = 0;

  async function worker() {
    while (token === appState.detailWarmupToken && cursor < queue.length) {
      const item = queue[cursor++];
      await loadDetail(item.result, item.variant, detailKey(item.variant));
    }
  }

  await Promise.all(Array.from({ length: workers }, worker));
}

function renderDetail(result, variant, book, loading, errorMessage) {
  detailContentNode.innerHTML = "";
  const activeVariant = variant || result.primary;
  const variants = Array.isArray(result.variants) && result.variants.length ? result.variants : [result.primary];
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
  meta.appendChild(resultBadge(chapters.length ? `${chapters.length} 章` : loading ? "加载章节中" : "暂无章节"));
  const timing = appState.detailTimings.get(detailKey(activeVariant));
  if (timing) meta.appendChild(resultBadge(`目录 ${timing}ms`));
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
  downloadButton.type = "button"; downloadButton.className = "download-button"; downloadButton.textContent = "下载并导出";
  downloadButton.addEventListener("click", () => {
    void startDownloadTask({ site: activeVariant.site, book_id: activeVariant.book_id }, downloadButton);
  });
  actions.appendChild(downloadButton);
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
      renderDetail(result, activeVariant, book, false, "");
    }));
    controlsGroup.appendChild(createChapterPageSizeControl(() => {
      renderDetail(result, activeVariant, book, false, "");
    }));
    chapterHead.appendChild(controlsGroup);
  }
  chapterSection.appendChild(chapterHead);

  if (loading) chapterSection.appendChild(createEmptyInline("正在加载章节列表..."));
  else if (errorMessage) chapterSection.appendChild(createEmptyInline(`详情加载失败：${errorMessage}`));
  else if (!chapters.length) chapterSection.appendChild(createEmptyInline("当前源没有返回章节列表。"));
  else chapterSection.appendChild(renderChapterList(chapters));

  detailContentNode.appendChild(chapterSection);
}

function renderChapterList(chapters) {
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
      number.className = "chapter-index"; number.textContent = String(chapter.order || i + 1);
      const content = document.createElement("div");
      const title = document.createElement("span");
      title.className = "chapter-title"; title.textContent = chapter.title || `第 ${i + 1} 章`;
      content.appendChild(title);
      item.appendChild(number); item.appendChild(content);
      item.addEventListener("click", () => openReader(chapters, i));
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

function createChapterPageSizeControl(onChange) {
  const wrap = document.createElement("label");
  wrap.className = "chapter-control-item";
  const text = document.createElement("span");
  text.textContent = "每批加载";
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

// ===== Chapter Reader =====
const readerOverlay = document.getElementById("readerOverlay");
const readerCloseButton = document.getElementById("readerCloseButton");
const readerTitle = document.getElementById("readerTitle");
const readerContent = document.getElementById("readerContent");
const readerBody = document.getElementById("readerBody");
const readerProgress = document.getElementById("readerProgress");

const readerState = { chapters: [], loadedMin: 0, loadedMax: -1, currentIndex: 0, loadingUp: false, loadingDown: false, cache: new Map(), pending: new Map() };

function openReader(chapters, index) {
  readerState.chapters = chapters;
  readerState.loadedMin = index;
  readerState.loadedMax = index - 1;
  readerState.currentIndex = index;
  readerState.loadingUp = false;
  readerState.loadingDown = false;
  readerOverlay.hidden = false;
  document.body.classList.add("has-overlay");
  readerContent.innerHTML = "";
  readerBody.scrollTop = 0;
  applyReaderSettings(loadReaderSettings());
  updateReaderTitle(index);
  preloadCache(index);
  // Load 5 chapters: current + 2 after, then prepend 2 before
  const loadDown = Math.min(index + 2, chapters.length - 1);
  const chain = [];
  for (let i = index; i <= loadDown; i++) chain.push(i);
  loadChaptersSequential(chain, "down").then(() => {
    const loadUp = Math.max(index - 2, 0);
    const upChain = [];
    for (let i = index - 1; i >= loadUp; i--) upChain.push(i);
    return loadChaptersSequential(upChain, "up");
  }).then(() => {
    preloadCache(index);
  });
}

async function loadChaptersSequential(indices, direction) {
  for (const i of indices) {
    if (direction === "down") await appendChapter(i);
    else await prependChapter(i);
  }
}

function closeReader() {
  readerOverlay.hidden = true;
  document.body.classList.remove("has-overlay");
}

function updateReaderTitle(index) {
  const ch = readerState.chapters[index];
  readerState.currentIndex = index;
  readerTitle.textContent = ch ? (ch.title || `第 ${index + 1} 章`) : "";
  readerProgress.textContent = `${index + 1} / ${readerState.chapters.length}`;
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
    case "n8novel":
    case "esjzone":
      return 30000;
    default:
      return 20000;
  }
}

function chapterReaderCacheKey(ch, index) {
  return ch.id || ch.url || `${index}:${ch.title || ""}`;
}

function preloadCache(centerIndex) {
  const variant = appState.activeDetailVariant || (appState.detailResult && appState.detailResult.primary) || {};
  const site = variant.site || ""; const bookID = variant.book_id || "";
  if (!site || !bookID) return;
  for (let offset = -8; offset <= 8; offset++) {
    const i = centerIndex + offset;
    if (i < 0 || i >= readerState.chapters.length) continue;
    const ch = readerState.chapters[i];
    const key = chapterReaderCacheKey(ch, i);
    if (readerState.cache.has(key) || readerState.pending.has(key)) continue;
    void fetchChapterContentForReader(ch, i).catch(() => {});
  }
}

readerCloseButton.addEventListener("click", closeReader);

// Scroll-based auto-load: up prepends, down appends
readerBody.addEventListener("scroll", () => {
  const { scrollTop, scrollHeight, clientHeight } = readerBody;
  // Update title based on visible chapter
  const blocks = readerContent.querySelectorAll("[data-chapter-index]");
  for (let i = blocks.length - 1; i >= 0; i--) {
    const rect = blocks[i].getBoundingClientRect();
    const bodyRect = readerBody.getBoundingClientRect();
    if (rect.top <= bodyRect.top + 60) {
      const visIdx = parseInt(blocks[i].dataset.chapterIndex, 10);
      updateReaderTitle(visIdx);
      preloadCache(visIdx);
      break;
    }
  }
  // Scroll near bottom → append next
  if (scrollTop + clientHeight >= scrollHeight - 900 && !readerState.loadingDown) {
    const next = readerState.loadedMax + 1;
    if (next < readerState.chapters.length) {
      readerState.loadingDown = true;
      preloadCache(next);
      loadChaptersSequential([next, next + 1, next + 2].filter((idx) => idx < readerState.chapters.length), "down").then(() => { readerState.loadingDown = false; });
    }
  }
  // Scroll near top → prepend previous
  if (scrollTop < 600 && !readerState.loadingUp) {
    const prev = readerState.loadedMin - 1;
    if (prev >= 0) {
      readerState.loadingUp = true;
      preloadCache(prev);
      loadChaptersSequential([prev, prev - 1].filter((idx) => idx >= 0), "up").then(() => { readerState.loadingUp = false; });
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

async function startDownloadTask(target, button) {
  const site = target.site || (target.primary && target.primary.site);
  const bookID = target.book_id || (target.primary && target.primary.book_id);
  if (!site || !bookID) return setStatus("下载目标缺失。");

  const originalText = button ? button.textContent : "";
  if (button) { button.disabled = true; button.textContent = "正在创建..."; }

  try {
    const response = await fetch(`${root}/api/download-tasks`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ site, book_id: bookID }),
    });
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || "download failed");

    upsertTask(payload.task); startPollingTask(payload.task.id); closeDetail(); activateTab("tasks");
    setStatus(`已创建下载任务：${payload.task.site}/${payload.task.book_id}`);
  } catch (error) { setStatus(`创建下载任务失败：${error.message}`); } 
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
      if (task.status === "completed") { stopPollingTask(taskId); setStatus(`下载完成：${task.site}/${task.book_id}`); } 
      else if (task.status === "failed") { stopPollingTask(taskId); setStatus(`下载失败：${task.error}`); }
    } catch (error) { stopPollingTask(taskId); setStatus(`读取任务状态失败：${error.message}`); }
  };
  void poll();
  appState.pollers.set(taskId, window.setInterval(poll, 1000));
}

function stopPollingTask(taskId) {
  if (!appState.pollers.has(taskId)) return;
  window.clearInterval(appState.pollers.get(taskId)); appState.pollers.delete(taskId);
}

function upsertTask(task) { appState.tasks.set(task.id, task); renderTasks(); }

function renderTasks() {
  const tasks = Array.from(appState.tasks.values()).sort((l, r) => new Date(r.updated_at).getTime() - new Date(l.updated_at).getTime());
  taskCountNode.textContent = String(tasks.length); taskTabCountNode.textContent = String(tasks.length);
  tasksNode.innerHTML = "";

  if (!tasks.length) return tasksNode.appendChild(createEmptyState("还没有下载任务。", true));

  tasks.forEach((task) => {
    const card = document.createElement("article"); card.className = `task-card is-${task.status}`;
    const head = document.createElement("div"); head.className = "task-head";
    const title = document.createElement("div"); title.className = "task-title"; title.textContent = task.title || `${sourceLabel(task.site)}/${task.book_id}`;
    const badgeNode = document.createElement("span"); badgeNode.className = `task-status status-${task.status}`; badgeNode.textContent = formatTaskStatus(task);
    head.appendChild(title); head.appendChild(badgeNode); card.appendChild(head);

    const meta = document.createElement("div"); meta.className = "task-meta"; meta.textContent = `${task.site}/${task.book_id}`; card.appendChild(meta);

    if (task.total_chapters > 0) {
      const progressWrap = document.createElement("div"); progressWrap.className = "task-progress";
      const progressBar = document.createElement("div"); progressBar.className = "task-progress-bar";
      const progressFill = document.createElement("div"); progressFill.className = "task-progress-fill";
      progressFill.style.width = `${taskProgressPercent(task)}%`;
      progressBar.appendChild(progressFill); progressWrap.appendChild(progressBar);

      const progressText = document.createElement("div"); progressText.className = "task-progress-text";
      progressText.textContent = `${task.completed_chapters}/${task.total_chapters}` + (task.eta ? ` (剩余时间: ${task.eta})` : "");
      progressWrap.appendChild(progressText); card.appendChild(progressWrap);
    }

    if (task.current_chapter) {
      const current = document.createElement("div"); current.className = "task-current"; current.textContent = `当前章节：${task.current_chapter}`; card.appendChild(current);
    }

    if (task.error) {
      const error = document.createElement("div"); error.className = "task-error"; error.textContent = task.error; card.appendChild(error);
    }

    if (Array.isArray(task.exported) && task.exported.length) {
      const exported = document.createElement("ul"); exported.className = "file-list";
      task.exported.forEach((path) => {
        const item = document.createElement("li"); const link = document.createElement("a");
        link.className = "file-download-link"; link.href = `${root}/api/download-file?path=${encodeURIComponent(path)}`;
        link.textContent = path.split(/[/\\]/).pop(); link.title = path; link.download = "";
        item.appendChild(link); exported.appendChild(item);
      });
      card.appendChild(exported);
    }

    if (Array.isArray(task.messages) && task.messages.length) {
      const messages = document.createElement("div"); messages.className = "task-messages";
      task.messages.slice(-4).forEach((msg) => {
        const item = document.createElement("div"); item.className = `task-message level-${msg.level}`; item.textContent = msg.text; messages.appendChild(item);
      });
      card.appendChild(messages);
    }
    tasksNode.appendChild(card);
  });
}

function formatTaskStatus(task) {
  if (task.status === "completed") return "已完成";
  if (task.status === "failed") return "失败";
  if (task.phase === "exporting") return "导出中";
  if (task.phase === "loading_chapters") return "加载章节中";
  if (task.status === "running") return "下载中";
  return "排队中";
}

function taskProgressPercent(task) {
  return !task.total_chapters ? 0 : Math.min(100, Math.round((task.completed_chapters / task.total_chapters) * 100));
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
  setRangeVal("generalMaxConnections", item.max_connections || 10);
  setRangeVal("generalMaxRPS", item.max_rps || 5.0);
  setRangeVal("generalRetryTimes", item.retry_times || 3);
  setRangeVal("generalBackoffFactor", item.backoff_factor || 2.0);
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
  renderGeneralConfigForm(appState.generalConfig);
}

async function saveGeneralConfig() {
  const current = appState.generalConfig || {};
  const payload = {
    workers: Math.max(1, Number.parseInt(generalWorkersNode.value || "4", 10) || 4),
    timeout: Math.max(1, Number.parseFloat(generalTimeoutNode.value || "10") || 10),
    request_interval: Math.max(0, Number.parseFloat(generalRequestIntervalNode.value || "0.5") || 0.5),
    max_connections: Math.max(1, Number.parseInt(String(current.max_connections || 10), 10) || 10),
    max_rps: Math.max(0.1, Number.parseFloat(String(current.max_rps || 5)) || 5),
    retry_times: Math.max(0, Number.parseInt(String(current.retry_times ?? 3), 10) || 0),
    backoff_factor: Math.max(1, Number.parseFloat(String(current.backoff_factor || 2)) || 2),
    locale_style: (generalLocaleStyleNode.value || "simplified").trim(),
    formats: (generalFormatsNode.value || "txt,epub").split(",").map((item) => item.trim()).filter(Boolean),
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
  renderGeneralConfigForm(appState.generalConfig);
  setStatus("已保存全局配置。新任务将使用最新参数。");
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
  siteWarnings = Array.isArray(data.site_warnings) ? data.site_warnings : [];
  siteStats = Array.isArray(data.site_stats) ? data.site_stats : [];
  renderSiteWarnings();
  setStatus(`已保存 ${sourceLabel(siteKey)} 配置。`);
}

function setStatus(text, tone) {
  statusNode.textContent = text;
  statusNode.dataset.tone = tone || "info";
}
