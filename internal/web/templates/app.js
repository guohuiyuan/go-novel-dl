const root = window.__NOVEL_DL__.root;
const defaultSources = window.__NOVEL_DL__.defaultSources || [];
const allSources = window.__NOVEL_DL__.allSources || [];
const defaultPageSize = window.__NOVEL_DL__.pageSize || 50;
const initialGeneralConfig = window.__NOVEL_DL__.generalConfig || {};
const sourceLabelMap = new Map(
  allSources.map((source) => [source.key, source.display_name || source.key]),
);
let siteWarnings = window.__NOVEL_DL__.siteWarnings || [];
let siteStats = window.__NOVEL_DL__.siteStats || [];
const siteWarningPanel = document.getElementById("siteWarningPanel");

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
  detailResult: null,
  activeDetailKey: "",
  siteConfigs: new Map(),
  paramSupports: [],
  generalConfig: initialGeneralConfig,
};

function renderSiteWarnings() {
  if (!siteWarningPanel) return;
  if (siteWarnings.length === 0) {
    siteWarningPanel.hidden = true;
    siteWarningPanel.innerHTML = "";
    return;
  }
  siteWarningPanel.hidden = false;
  siteWarningPanel.innerHTML = "";
  siteWarnings.forEach((warning) => {
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
    siteWarningPanel.appendChild(card);
  });
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
const siteLoginRequiredNode = document.getElementById("siteLoginRequired");
const siteWorkerLimitNode = document.getElementById("siteWorkerLimit");
const siteFetchImagesNode = document.getElementById("siteFetchImages");
const siteLocaleStyleNode = document.getElementById("siteLocaleStyle");
const siteUsernameNode = document.getElementById("siteUsername");
const sitePasswordNode = document.getElementById("sitePassword");
const toggleSitePasswordButton = document.getElementById("toggleSitePassword");
const siteCookieNode = document.getElementById("siteCookie");
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
  renderSourceTagFilters();
  renderSourceSelector();
  renderWarnings([]);
  renderResults([]);
  renderTasks();
  renderPaging();
  renderResultMeta();
  setStatus("选择渠道后输入关键词开始搜索。");

  // 绑定滑动条联动显示
  ['generalWorkers', 'generalTimeout', 'generalRequestInterval', 'generalMaxConnections', 'generalMaxRPS', 'generalRetryTimes', 'generalBackoffFactor', 'generalWebPageSize', 'generalCLIPageSize', 'siteWorkerLimit'].forEach(bindRangeValue);

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
  if (appState.selectedSites.has("esjzone") && !isESJConfigured()) {
    showESJConfigPrompt();
    return setStatus("ESJ Zone 需要先配置 Cookie 或密码。已为你打开配置入口提示。");
  }

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

    renderResults(appState.results);
    renderWarnings(payload.warnings || []);
    renderPaging();
    renderResultMeta();

    if (!appState.results.length) return setStatus("没有找到结果。");
    setStatus(`当前显示第 ${appState.page} 页，共 ${totalLabel(appState.total, appState.totalExact)} 条结果。`);
  } catch (error) {
    if (error && error.payload && error.payload.error_code === "esjzone_config_required") {
      showESJConfigPrompt();
    }
    appState.results = []; appState.total = 0; appState.totalExact = true;
    appState.hasPrev = false; appState.hasNext = false;
    renderResults([]); renderWarnings([]); renderPaging(); renderResultMeta();
    setStatus(`搜索失败：${error.message}`);
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
  renderSourceTagFilters();
  renderSourceSelector();

  const visibleCount = filteredSources().length;
  if (appState.selectedSourceTags.size === 0) {
    setStatus("已清空渠道标签筛选。");
    return;
  }
  setStatus(`已按标签 ${Array.from(appState.selectedSourceTags).join("、")} 筛选，当前显示 ${visibleCount} 个渠道。`);
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

function sourceSummaryText(visibleCount) {
  const total = allSources.length;
  const selectedCount = appState.selectedSites.size;
  if (appState.selectedSourceTags.size === 0) {
    return `已选择 ${selectedCount} / ${total} 个渠道，高亮即已选。`;
  }

  const visibleKeys = new Set(filteredSources().map((source) => source.key));
  let hiddenSelected = 0;
  appState.selectedSites.forEach((siteKey) => {
    if (!visibleKeys.has(siteKey)) hiddenSelected += 1;
  });

  let summary = `标签筛选：${Array.from(appState.selectedSourceTags).join("、")}；当前显示 ${visibleCount} / ${total} 个渠道，已选择 ${selectedCount} 个。`;
  if (hiddenSelected > 0) summary += ` 其中 ${hiddenSelected} 个已选渠道当前被隐藏。`;
  return summary;
}

function renderWarnings(warnings) {
  warningsNode.innerHTML = "";
  warningsNode.classList.toggle("is-empty", warnings.length === 0);
  warnings.forEach((warning) => {
    const node = document.createElement("div");
    node.className = "warning-item";
    node.textContent = `${sourceLabel(warning.site)}：${warning.error}`;
    warningsNode.appendChild(node);
  });
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
  appState.activeDetailKey = cacheKey;
  detailOverlay.hidden = false;
  document.body.classList.add("has-overlay");

  const cached = appState.detailCache.get(cacheKey);
  if (cached) return renderDetail(result, activeVariant, cached, false, "");

  renderDetail(result, activeVariant, null, true, "");
  void loadDetail(result, activeVariant, cacheKey);
}

function closeDetail() {
  detailOverlay.hidden = true; document.body.classList.remove("has-overlay");
}

async function loadDetail(result, variant, cacheKey) {
  try {
    const url = new URL(`${window.location.origin}${root}/api/books/detail`);
    url.searchParams.set("site", variant.site); url.searchParams.set("book_id", variant.book_id);
    const response = await fetch(url.toString());
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || "detail failed");
    const book = payload.book || null;
    if (!book) throw new Error("未返回详情数据");

    appState.detailCache.set(cacheKey, book);
    if (appState.detailResult === result && appState.activeDetailKey === cacheKey) {
      renderDetail(result, variant, book, false, "");
    }
  } catch (error) {
    if (appState.detailResult === result && appState.activeDetailKey === cacheKey) {
      renderDetail(result, variant, null, false, error.message);
    }
  }
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
  if (book && book.source_url) {
    const titleLink = document.createElement("a");
    titleLink.className = "detail-title-link"; titleLink.href = book.source_url;
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
  chapterHead.appendChild(chapterTitle); chapterSection.appendChild(chapterHead);

  if (loading) chapterSection.appendChild(createEmptyInline("正在加载章节列表..."));
  else if (errorMessage) chapterSection.appendChild(createEmptyInline(`详情加载失败：${errorMessage}`));
  else if (!chapters.length) chapterSection.appendChild(createEmptyInline("当前源没有返回章节列表。"));
  else chapterSection.appendChild(renderChapterList(chapters));

  detailContentNode.appendChild(chapterSection);
}

function renderChapterList(chapters) {
  const list = document.createElement("div");
  list.className = "chapter-list";
  let lastVolume = "";
  chapters.forEach((chapter, index) => {
    if (chapter.volume && chapter.volume !== lastVolume) {
      lastVolume = chapter.volume;
      const volume = document.createElement("div");
      volume.className = "chapter-volume"; volume.textContent = chapter.volume;
      list.appendChild(volume);
    }
    const item = document.createElement("div"); item.className = "chapter-item";
    const number = document.createElement("span"); number.className = "chapter-index"; number.textContent = String(chapter.order || index + 1);
    const content = document.createElement("div");
    const chapterTitle = chapter.title || `第 ${index + 1} 章`;
    if (chapter.url) {
      const titleLink = document.createElement("a");
      titleLink.className = "chapter-title chapter-title-link"; titleLink.href = chapter.url;
      titleLink.target = "_blank"; titleLink.rel = "noopener noreferrer"; titleLink.textContent = chapterTitle;
      content.appendChild(titleLink);
    } else {
      const title = document.createElement("span"); title.className = "chapter-title"; title.textContent = chapterTitle;
      content.appendChild(title);
    }
    item.appendChild(number); item.appendChild(content); list.appendChild(item);
  });
  return list;
}

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
function displayDetailTitle(result, variant, book) { return (book && book.title) || result.title || (variant && variant.title) || (variant && variant.book_id) || "未命名小说"; }
function displayDetailAuthor(result, variant, book) { return (book && book.author) || result.author || (variant && variant.author) || "未知作者"; }
function displayDetailDescription(result, book) { return (book && book.description) || result.description || "暂无简介。"; }
function sourceLabel(siteKey) { return sourceLabelMap.get(siteKey) || siteKey || "未知来源"; }
function detailKey(variant) { return `${variant.site}/${variant.book_id}`; }
function totalLabel(total, exact) { return exact ? `${total}` : `${total}+`; }

function isESJConfigured() {
  const item = appState.siteConfigs.get("esjzone");
  if (!item) return false;
  const hasCookie = Boolean((item.cookie || "").trim());
  const hasCredentials = Boolean((item.username || "").trim() && (item.password || "").trim());
  return hasCookie || hasCredentials;
}

function showESJConfigPrompt() {
  siteWarnings = [{ site_key: "esjzone", level: "config", message: "ESJ Zone 尚未配置 Cookie 或密码，搜索前请先完成站点配置。", action_label: "打开站点配置", action_link: "#site-config" }];
  renderSiteWarnings();
}

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
  items.forEach((item) => {
    const option = document.createElement("option");
    option.value = item.key; option.textContent = sourceLabel(item.key);
    siteConfigKeyNode.appendChild(option);
  });
  if (items.length > 0) {
    siteConfigKeyNode.value = items[0].key; populateSiteConfigForm(items[0].key);
  }
}

function setRangeVal(id, val) {
  const el = document.getElementById(id);
  if (el) { el.value = val; document.getElementById(id + "Val").textContent = val; }
}

function populateSiteConfigForm(siteKey) {
  const item = appState.siteConfigs.get(siteKey);
  if (!item) return;
  siteLoginRequiredNode.checked = Boolean(item.login_required);
  setRangeVal("siteWorkerLimit", item.worker_limit || 0);
  siteFetchImagesNode.checked = item.fetch_images !== false;
  siteLocaleStyleNode.value = (item.locale_style || "").trim();
  siteUsernameNode.value = item.username || "";
  sitePasswordNode.value = item.password || "";
  sitePasswordNode.type = "password";
  
  // 恢复密码小眼睛状态为隐藏
  toggleSitePasswordButton.innerHTML = `<svg viewBox="0 0 24 24" width="20" height="20" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path><circle cx="12" cy="12" r="3"></circle></svg>`;
  
  siteCookieNode.value = item.cookie || "";
  siteMirrorHostsNode.value = Array.isArray(item.mirror_hosts) ? item.mirror_hosts.join("\n") : "";
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
  setRangeVal("generalWebPageSize", item.web_page_size || 50);
  setRangeVal("generalCLIPageSize", item.cli_page_size || 30);
  generalRawDataDirNode.value = item.raw_data_dir || "./data/raw_data";
  generalCacheDirNode.value = item.cache_dir || "./data/novel_cache";
  generalOutputDirNode.value = item.output_dir || "./data/downloads";
}

async function loadGeneralConfig() {
  const response = await fetch(`${root}/api/general-config`);
  const payload = await response.json();
  if (!response.ok) throw new Error(payload.error || "general config load failed");
  appState.generalConfig = payload.item || appState.generalConfig;
  renderGeneralConfigForm(appState.generalConfig);
}

async function saveGeneralConfig() {
  const payload = {
    workers: Math.max(1, Number.parseInt(generalWorkersNode.value || "4", 10) || 4),
    timeout: Math.max(1, Number.parseFloat(generalTimeoutNode.value || "10") || 10),
    request_interval: Math.max(0, Number.parseFloat(generalRequestIntervalNode.value || "0.5") || 0.5),
    max_connections: Math.max(1, Number.parseInt(generalMaxConnectionsNode.value || "10", 10) || 10),
    max_rps: Math.max(0.1, Number.parseFloat(generalMaxRPSNode.value || "5") || 5),
    retry_times: Math.max(0, Number.parseInt(generalRetryTimesNode.value || "3", 10) || 3),
    backoff_factor: Math.max(1, Number.parseFloat(generalBackoffFactorNode.value || "2") || 2),
    locale_style: (generalLocaleStyleNode.value || "simplified").trim(),
    formats: (generalFormatsNode.value || "txt,epub").split(",").map((item) => item.trim()).filter(Boolean),
    append_timestamp: generalAppendTimestampNode.checked,
    include_picture: generalIncludePictureNode.checked,
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

async function saveSiteConfig() {
  const siteKey = siteConfigKeyNode.value;
  if (!siteKey) return;
  const payload = {
    login_required: siteLoginRequiredNode.checked,
    worker_limit: Math.max(0, Number.parseInt(siteWorkerLimitNode.value || "0", 10) || 0),
    fetch_images: siteFetchImagesNode.checked,
    locale_style: siteLocaleStyleNode.value.trim(),
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

function setStatus(text) { statusNode.textContent = text; }
