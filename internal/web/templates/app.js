const root = window.__NOVEL_DL__.root;
const defaultSources = window.__NOVEL_DL__.defaultSources || [];
const allSources = window.__NOVEL_DL__.allSources || [];
const defaultPageSize = window.__NOVEL_DL__.pageSize || 50;
const sourceLabelMap = new Map(
  allSources.map((source) => [source.key, source.display_name || source.key]),
);

const DEFAULT_COVER_SRC = `data:image/svg+xml;charset=UTF-8,${encodeURIComponent(`
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 360 480">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%" stop-color="#fdfaef"/>
      <stop offset="100%" stop-color="#fef3c7"/>
    </linearGradient>
    <linearGradient id="circleBg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%" stop-color="#fde68a"/>
      <stop offset="100%" stop-color="#fcd34d"/>
    </linearGradient>
  </defs>
  <rect width="360" height="480" rx="28" fill="url(#bg)"/>
  <rect x="30" y="32" width="300" height="416" rx="24" fill="#ffffff" fill-opacity="0.85" stroke="#fcd34d" stroke-opacity="0.6"/>
  <circle cx="180" cy="164" r="58" fill="url(#circleBg)"/>
  <rect x="104" y="248" width="152" height="18" rx="9" fill="#fde68a"/>
  <rect x="90" y="282" width="180" height="18" rx="9" fill="#fef08a"/>
  <text x="180" y="356" text-anchor="middle" font-size="28" font-weight="bold" font-family="Arial, sans-serif" fill="#b45309">Novel DL</text>
  <text x="180" y="388" text-anchor="middle" font-size="16" font-family="Arial, sans-serif" fill="#d97706">No Cover</text>
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
  tasks: new Map(),
  pollers: new Map(),
  detailCache: new Map(),
  detailResult: null,
  activeDetailKey: "",
};

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

bootstrap();

function bootstrap() {
  renderSourceSelector();
  renderWarnings([]);
  renderResults([]);
  renderTasks();
  renderPaging();
  renderResultMeta();
  setStatus("选择渠道后输入关键词开始搜索。");

  searchTabButton.addEventListener("click", () => activateTab("search"));
  tasksTabButton.addEventListener("click", () => activateTab("tasks"));

  selectAllSourcesButton.addEventListener("click", () => {
    appState.selectedSites = new Set(allSources.map((source) => source.key));
    renderSourceSelector();
    setStatus(`已选中全部 ${appState.selectedSites.size} 个渠道。`);
  });

  clearSourcesButton.addEventListener("click", () => {
    appState.selectedSites = new Set();
    renderSourceSelector();
    setStatus("已清空渠道选择。");
  });

  prevPageButton.addEventListener("click", async () => {
    if (!appState.hasPrev || !appState.lastKeyword) {
      return;
    }
    appState.page -= 1;
    await performSearch();
  });

  nextPageButton.addEventListener("click", async () => {
    if (!appState.hasNext || !appState.lastKeyword) {
      return;
    }
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
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && !detailOverlay.hidden) {
      closeDetail();
    }
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
  if (!keyword) {
    setStatus("请输入关键词。");
    return;
  }
  if (appState.selectedSites.size === 0) {
    setStatus("请至少选择一个渠道。");
    return;
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
        keyword,
        scope: "all",
        sites: Array.from(appState.selectedSites),
        page: appState.page,
        page_size: appState.pageSize,
      }),
    });
    const payload = await response.json();
    if (!response.ok) {
      throw new Error(payload.error || "search failed");
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

    if (!appState.results.length) {
      setStatus("没有找到结果。");
      return;
    }

    setStatus(
      `当前显示第 ${appState.page} 页，共 ${totalLabel(appState.total, appState.totalExact)} 条结果。`,
    );
  } catch (error) {
    appState.results = [];
    appState.total = 0;
    appState.totalExact = true;
    appState.hasPrev = false;
    appState.hasNext = false;
    renderResults([]);
    renderWarnings([]);
    renderPaging();
    renderResultMeta();
    setStatus(`搜索失败：${error.message}`);
  }
}

function renderSourceSelector() {
  sourceSelectorNode.innerHTML = "";

  allSources.forEach((source) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "source-option";
    button.setAttribute("aria-pressed", String(appState.selectedSites.has(source.key)));
    if (appState.selectedSites.has(source.key)) {
      button.classList.add("is-selected");
    }

    const title = document.createElement("span");
    title.className = "source-option-title";
    title.textContent = source.display_name || source.key;

    const key = document.createElement("span");
    key.className = "source-option-key";
    key.textContent = source.key;

    button.appendChild(title);
    button.appendChild(key);
    button.addEventListener("click", () => toggleSource(source.key));
    sourceSelectorNode.appendChild(button);
  });

  sourceSummaryNode.textContent = `已选择 ${appState.selectedSites.size} / ${allSources.length} 个渠道，高亮即已选。`;
}

function toggleSource(siteKey) {
  if (appState.selectedSites.has(siteKey)) {
    appState.selectedSites.delete(siteKey);
  } else {
    appState.selectedSites.add(siteKey);
  }
  renderSourceSelector();
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
  if (!results.length) {
    resultsNode.appendChild(createEmptyState("当前页没有可展示的结果。"));
    return;
  }

  results.forEach((result) => {
    const card = document.createElement("article");
    card.className = "result-card";

    const coverButton = document.createElement("button");
    coverButton.type = "button";
    coverButton.className = "result-cover-button";
    coverButton.setAttribute(
      "aria-label",
      `查看 ${displayResultTitle(result)} 的详情`,
    );
    coverButton.appendChild(
      createCoverImage(result.cover_url, displayResultTitle(result), "result-cover"),
    );

    const overlay = document.createElement("span");
    overlay.className = "result-cover-overlay";
    overlay.textContent = "查看详情";
    coverButton.appendChild(overlay);
    coverButton.addEventListener("click", () => openDetail(result, result.primary));

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
    source.textContent = `源：${sourceLabel(result.preferred_site)}`;

    body.appendChild(title);
    body.appendChild(author);
    body.appendChild(source);

    if (result.latest_chapter) {
      const extra = document.createElement("p");
      extra.className = "result-extra";
      extra.textContent = `最新：${result.latest_chapter}`;
      body.appendChild(extra);
    }

    card.appendChild(coverButton);
    card.appendChild(body);
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
  if (cached) {
    renderDetail(result, activeVariant, cached, false, "");
    return;
  }

  renderDetail(result, activeVariant, null, true, "");
  void loadDetail(result, activeVariant, cacheKey);
}

function closeDetail() {
  detailOverlay.hidden = true;
  document.body.classList.remove("has-overlay");
}

async function loadDetail(result, variant, cacheKey) {
  try {
    const url = new URL(`${window.location.origin}${root}/api/books/detail`);
    url.searchParams.set("site", variant.site);
    url.searchParams.set("book_id", variant.book_id);

    const response = await fetch(url.toString());
    const payload = await response.json();
    if (!response.ok) {
      throw new Error(payload.error || "detail failed");
    }

    const book = payload.book || null;
    if (!book) {
      throw new Error("未返回详情数据");
    }

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
  const variants = Array.isArray(result.variants) && result.variants.length
    ? result.variants
    : [result.primary];
  const title = displayDetailTitle(result, activeVariant, book);
  const author = displayDetailAuthor(result, activeVariant, book);
  const description = displayDetailDescription(result, book);
  const chapters = Array.isArray(book && book.chapters) ? book.chapters : [];

  const hero = document.createElement("section");
  hero.className = "detail-hero";

  hero.appendChild(
    createCoverImage(
      book && book.cover_url ? book.cover_url : result.cover_url,
      title,
      "detail-cover",
    ),
  );

  const summary = document.createElement("div");
  summary.className = "detail-summary";

  const heading = document.createElement("h2");
  heading.id = "detailHeading";
  heading.className = "detail-title";
  heading.textContent = title;

  const authorNode = document.createElement("p");
  authorNode.className = "detail-author";
  authorNode.textContent = `作者：${author}`;

  const sourceNode = document.createElement("p");
  sourceNode.className = "detail-source";
  sourceNode.textContent = `当前源：${sourceLabel(activeVariant.site)}`;

  summary.appendChild(heading);
  summary.appendChild(authorNode);
  summary.appendChild(sourceNode);

  const meta = document.createElement("div");
  meta.className = "detail-meta";
  meta.appendChild(resultBadge(sourceLabel(activeVariant.site)));
  meta.appendChild(
    resultBadge(chapters.length ? `${chapters.length} 章` : loading ? "加载章节中" : "暂无章节"),
  );
  if (result.latest_chapter) {
    meta.appendChild(resultBadge(result.latest_chapter));
  }
  if (result.source_count > 1) {
    meta.appendChild(resultBadge(`${result.source_count} 个来源`));
  }
  summary.appendChild(meta);

  if (variants.length > 1) {
    const switchWrap = document.createElement("div");
    switchWrap.className = "detail-source-switch";
    variants.forEach((item) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "detail-source-button";
      if (detailKey(item) === detailKey(activeVariant)) {
        button.classList.add("is-active");
      }
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
  downloadButton.textContent = "下载并导出";
  downloadButton.addEventListener("click", () => {
    void startDownloadTask(
      {
        site: activeVariant.site,
        book_id: activeVariant.book_id,
      },
      downloadButton,
    );
  });
  actions.appendChild(downloadButton);
  summary.appendChild(actions);

  if (book && book.source_url) {
    const links = document.createElement("div");
    links.className = "detail-links";

    const sourceLink = document.createElement("a");
    sourceLink.className = "detail-link";
    sourceLink.href = book.source_url;
    sourceLink.target = "_blank";
    sourceLink.rel = "noopener noreferrer";
    sourceLink.textContent = "打开原站页面";

    links.appendChild(sourceLink);
    summary.appendChild(links);
  }

  hero.appendChild(summary);
  detailContentNode.appendChild(hero);

  const introSection = document.createElement("section");
  introSection.className = "detail-section";

  const introHead = document.createElement("div");
  introHead.className = "detail-section-head";
  const introTitle = document.createElement("h3");
  introTitle.textContent = "小说简介";
  introHead.appendChild(introTitle);
  introSection.appendChild(introHead);

  const introBody = document.createElement("p");
  introBody.className = "detail-description";
  if (errorMessage && !description.trim()) {
    introBody.textContent = `详情加载失败：${errorMessage}`;
  } else {
    introBody.textContent = description;
  }
  introSection.appendChild(introBody);
  detailContentNode.appendChild(introSection);

  const chapterSection = document.createElement("section");
  chapterSection.className = "detail-section";

  const chapterHead = document.createElement("div");
  chapterHead.className = "detail-section-head";
  const chapterTitle = document.createElement("h3");
  chapterTitle.textContent = "章节";
  chapterHead.appendChild(chapterTitle);
  chapterSection.appendChild(chapterHead);

  if (loading) {
    chapterSection.appendChild(createEmptyInline("正在加载章节列表..."));
  } else if (errorMessage) {
    chapterSection.appendChild(createEmptyInline(`详情加载失败：${errorMessage}`));
  } else if (!chapters.length) {
    chapterSection.appendChild(createEmptyInline("当前源没有返回章节列表。"));
  } else {
    chapterSection.appendChild(renderChapterList(chapters));
  }

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
      volume.className = "chapter-volume";
      volume.textContent = chapter.volume;
      list.appendChild(volume);
    }

    const item = document.createElement("div");
    item.className = "chapter-item";

    const number = document.createElement("span");
    number.className = "chapter-index";
    number.textContent = String(chapter.order || index + 1);

    const content = document.createElement("div");

    const title = document.createElement("span");
    title.className = "chapter-title";
    title.textContent = chapter.title || `第 ${index + 1} 章`;
    content.appendChild(title);

    if (chapter.url) {
      const link = document.createElement("a");
      link.className = "chapter-url";
      link.href = chapter.url;
      link.target = "_blank";
      link.rel = "noopener noreferrer";
      link.textContent = chapter.url;
      content.appendChild(link);
    }

    item.appendChild(number);
    item.appendChild(content);
    list.appendChild(item);
  });

  return list;
}

function resultBadge(text) {
  const node = document.createElement("span");
  node.className = "result-badge";
  node.textContent = text;
  return node;
}

function renderPaging() {
  resultCountNode.textContent = appState.lastKeyword
    ? totalLabel(appState.total, appState.totalExact)
    : "0";
  pageIndicatorNode.textContent = `第 ${appState.page} 页 · 每页 ${appState.pageSize} 本`;
  prevPageButton.disabled = !appState.hasPrev;
  nextPageButton.disabled = !appState.hasNext;
}

function renderResultMeta() {
  if (!appState.lastKeyword) {
    resultMetaNode.textContent = "输入关键词后开始搜索。";
    return;
  }
  if (!appState.results.length) {
    resultMetaNode.textContent = `关键词“${appState.lastKeyword}”暂无结果。`;
    return;
  }

  const start = (appState.page - 1) * appState.pageSize + 1;
  const end = start + appState.results.length - 1;
  resultMetaNode.textContent =
    `关键词“${appState.lastKeyword}”当前显示 ${start}-${end}，共 ${totalLabel(appState.total, appState.totalExact)} 条。`;
}

async function startDownloadTask(target, button) {
  const site = target.site || (target.primary && target.primary.site);
  const bookID = target.book_id || (target.primary && target.primary.book_id);
  if (!site || !bookID) {
    setStatus("下载目标缺失。");
    return;
  }

  const originalText = button ? button.textContent : "";
  if (button) {
    button.disabled = true;
    button.textContent = "正在创建...";
  }

  try {
    const response = await fetch(`${root}/api/download-tasks`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        site,
        book_id: bookID,
      }),
    });
    const payload = await response.json();
    if (!response.ok) {
      throw new Error(payload.error || "download failed");
    }

    upsertTask(payload.task);
    startPollingTask(payload.task.id);
    closeDetail();
    activateTab("tasks");
    setStatus(`已创建下载任务：${payload.task.site}/${payload.task.book_id}`);
  } catch (error) {
    setStatus(`创建下载任务失败：${error.message}`);
  } finally {
    if (button) {
      button.disabled = false;
      button.textContent = originalText;
    }
  }
}

function startPollingTask(taskId) {
  if (appState.pollers.has(taskId)) {
    return;
  }

  const poll = async () => {
    try {
      const response = await fetch(`${root}/api/download-tasks/${taskId}`);
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error || "task fetch failed");
      }

      const task = payload.task;
      upsertTask(task);
      if (task.status === "completed") {
        stopPollingTask(taskId);
        setStatus(`下载完成：${task.site}/${task.book_id}`);
      } else if (task.status === "failed") {
        stopPollingTask(taskId);
        setStatus(`下载失败：${task.error}`);
      }
    } catch (error) {
      stopPollingTask(taskId);
      setStatus(`读取任务状态失败：${error.message}`);
    }
  };

  void poll();
  const handle = window.setInterval(poll, 1000);
  appState.pollers.set(taskId, handle);
}

function stopPollingTask(taskId) {
  if (!appState.pollers.has(taskId)) {
    return;
  }
  window.clearInterval(appState.pollers.get(taskId));
  appState.pollers.delete(taskId);
}

function upsertTask(task) {
  appState.tasks.set(task.id, task);
  renderTasks();
}

function renderTasks() {
  const tasks = Array.from(appState.tasks.values()).sort((left, right) => {
    return new Date(right.updated_at).getTime() - new Date(left.updated_at).getTime();
  });

  taskCountNode.textContent = String(tasks.length);
  taskTabCountNode.textContent = String(tasks.length);
  tasksNode.innerHTML = "";

  if (!tasks.length) {
    tasksNode.appendChild(createEmptyState("还没有下载任务。", true));
    return;
  }

  tasks.forEach((task) => {
    const card = document.createElement("article");
    card.className = "task-card";

    const head = document.createElement("div");
    head.className = "task-head";

    const title = document.createElement("div");
    title.className = "task-title";
    title.textContent = task.title || `${sourceLabel(task.site)}/${task.book_id}`;

    const badgeNode = document.createElement("span");
    badgeNode.className = `task-status status-${task.status}`;
    badgeNode.textContent = formatTaskStatus(task);

    head.appendChild(title);
    head.appendChild(badgeNode);
    card.appendChild(head);

    const meta = document.createElement("div");
    meta.className = "task-meta";
    meta.textContent = `${task.site}/${task.book_id}`;
    card.appendChild(meta);

    if (task.total_chapters > 0) {
      const progressWrap = document.createElement("div");
      progressWrap.className = "task-progress";

      const progressBar = document.createElement("div");
      progressBar.className = "task-progress-bar";

      const progressFill = document.createElement("div");
      progressFill.className = "task-progress-fill";
      progressFill.style.width = `${taskProgressPercent(task)}%`;

      progressBar.appendChild(progressFill);
      progressWrap.appendChild(progressBar);

      const progressText = document.createElement("div");
      progressText.className = "task-progress-text";
      let progressMsg = `${task.completed_chapters}/${task.total_chapters}`;
      if (task.eta) {
        progressMsg += ` (ETA: ${task.eta})`;
      }
      progressText.textContent = progressMsg;
      progressWrap.appendChild(progressText);

      card.appendChild(progressWrap);
    }

    if (task.current_chapter) {
      const current = document.createElement("div");
      current.className = "task-current";
      current.textContent = `当前章节：${task.current_chapter}`;
      card.appendChild(current);
    }

    if (task.error) {
      const error = document.createElement("div");
      error.className = "task-error";
      error.textContent = task.error;
      card.appendChild(error);
    }

    if (Array.isArray(task.exported) && task.exported.length) {
      const exported = document.createElement("ul");
      exported.className = "file-list";
      task.exported.forEach((path) => {
        const item = document.createElement("li");
        const link = document.createElement("a");
        link.className = "file-download-link";
        link.href = `${root}/api/download-file?path=${encodeURIComponent(path)}`;
        link.textContent = path.split(/[/\\]/).pop();
        link.title = path;
        link.download = "";
        item.appendChild(link);
        exported.appendChild(item);
      });
      card.appendChild(exported);
    }

    if (Array.isArray(task.messages) && task.messages.length) {
      const messages = document.createElement("div");
      messages.className = "task-messages";
      task.messages.slice(-4).forEach((message) => {
        const item = document.createElement("div");
        item.className = `task-message level-${message.level}`;
        item.textContent = message.text;
        messages.appendChild(item);
      });
      card.appendChild(messages);
    }

    tasksNode.appendChild(card);
  });
}

function formatTaskStatus(task) {
  if (task.status === "completed") {
    return "已完成";
  }
  if (task.status === "failed") {
    return "失败";
  }
  if (task.phase === "exporting") {
    return "导出中";
  }
  if (task.phase === "loading_chapters") {
    return "加载章节中";
  }
  if (task.status === "running") {
    return "下载中";
  }
  return "排队中";
}

function taskProgressPercent(task) {
  if (!task.total_chapters) {
    return 0;
  }
  return Math.min(100, Math.round((task.completed_chapters / task.total_chapters) * 100));
}

function createCoverImage(src, alt, className) {
  const image = document.createElement("img");
  image.className = className;
  image.loading = "lazy";
  image.alt = alt || "Novel cover";
  image.src = src || DEFAULT_COVER_SRC;
  image.addEventListener("error", handleCoverError);
  return image;
}

function handleCoverError(event) {
  const image = event.currentTarget;
  if (image.dataset.fallbackApplied === "true") {
    return;
  }
  image.dataset.fallbackApplied = "true";
  image.src = DEFAULT_COVER_SRC;
}

function createEmptyState(text, compact) {
  const node = document.createElement("div");
  node.className = compact ? "empty-state empty-state-compact" : "empty-state";
  node.textContent = text;
  return node;
}

function createEmptyInline(text) {
  const node = document.createElement("div");
  node.className = "empty-inline";
  node.textContent = text;
  return node;
}

function displayResultTitle(result) {
  return result.title || (result.primary && result.primary.title) || (result.primary && result.primary.book_id) || "未命名小说";
}

function displayResultAuthor(result) {
  return result.author || (result.primary && result.primary.author) || "未知作者";
}

function displayDetailTitle(result, variant, book) {
  return (book && book.title)
    || result.title
    || (variant && variant.title)
    || (variant && variant.book_id)
    || "未命名小说";
}

function displayDetailAuthor(result, variant, book) {
  return (book && book.author)
    || result.author
    || (variant && variant.author)
    || "未知作者";
}

function displayDetailDescription(result, book) {
  return (book && book.description) || result.description || "暂无简介。";
}

function sourceLabel(siteKey) {
  return sourceLabelMap.get(siteKey) || siteKey || "未知来源";
}

function detailKey(variant) {
  return `${variant.site}/${variant.book_id}`;
}

function totalLabel(total, exact) {
  return exact ? `${total}` : `${total}+`;
}

function setStatus(text) {
  statusNode.textContent = text;
}
