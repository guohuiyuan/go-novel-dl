const root = window.__NOVEL_DL__.root;
const defaultSources = window.__NOVEL_DL__.defaultSources || [];
const allSources = window.__NOVEL_DL__.allSources || [];

// 🔥 修复：直接提取 defaultSources 里的所有 key 作为默认选中集合
// 因为后端传过来的 defaultSources 已经是过滤过、推荐可用的源了
const initialSelectedSites = defaultSources.map(source => source.key);

const appState = {
  page: 1,
  pageSize: 12,
  lastKeyword: "",
  results: [],
  total: 0,
  totalExact: true,
  hasPrev: false,
  hasNext: false,
  selectedSites: new Set(initialSelectedSites),
  tasks: new Map(),
  pollers: new Map(),
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
const selectedCountNode = document.getElementById("selectedCount");
const sourceSummaryNode = document.getElementById("sourceSummary");
const defaultSourceCountNode = document.getElementById("defaultSourceCount");
const defaultSourcesNode = document.getElementById("defaultSources");
const sourceSelectorNode = document.getElementById("sourceSelector");
const tasksNode = document.getElementById("tasks");
const selectSearchableSourcesButton = document.getElementById("selectSearchableSources");
const selectAllSourcesButton = document.getElementById("selectAllSources");
const clearSourcesButton = document.getElementById("clearSources");

renderSourceChips(defaultSourcesNode, defaultSources);
renderSourceSelector();
renderTasks();
renderPaging();
defaultSourceCountNode.textContent = String(defaultSources.length);
setStatus("选择渠道后输入关键词开始搜索。");

// 恢复默认勾选
selectSearchableSourcesButton.addEventListener("click", () => {
  appState.selectedSites = new Set(defaultSources.map(source => source.key));
  renderSourceSelector();
  setStatus(`已恢复默认勾选，共 ${appState.selectedSites.size} 个推荐渠道。`);
});

// 全选所有渠道
selectAllSourcesButton.addEventListener("click", () => {
  appState.selectedSites = new Set(allSources.map(source => source.key));
  renderSourceSelector();
  const unsupported = selectedUnsupportedCount();
  if (unsupported > 0) {
    setStatus(`已选择所有 ${appState.selectedSites.size} 个渠道，其中 ${unsupported} 个仅支持直链下载。`);
    return;
  }
  setStatus(`已选择所有 ${appState.selectedSites.size} 个渠道。`);
});

// 清空选择
clearSourcesButton.addEventListener("click", () => {
  appState.selectedSites = new Set();
  renderSourceSelector();
  setStatus("已清空渠道选择。");
});

prevPageButton.addEventListener("click", () => {
  if (!appState.hasPrev || !appState.lastKeyword) {
    return;
  }
  appState.page -= 1;
  performSearch();
});

nextPageButton.addEventListener("click", () => {
  if (!appState.hasNext || !appState.lastKeyword) {
    return;
  }
  appState.page += 1;
  performSearch();
});

searchForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  appState.page = 1;
  await performSearch();
});

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

  appState.lastKeyword = keyword;
  renderWarnings([]);
  setStatus(`正在搜索“${keyword}”第 ${appState.page} 页...`);

  try {
    const response = await fetch(`${root}/api/search`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        keyword,
        scope: "all", // 后端逻辑兼容，直接传 all 即可
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
    setStatus(`找到 ${appState.totalExact ? appState.total : `${appState.total}+`} 条聚合结果，当前第 ${appState.page} 页。`);
  } catch (error) {
    appState.results = [];
    appState.total = 0;
    appState.totalExact = true;
    appState.hasPrev = false;
    appState.hasNext = false;
    renderResults([]);
    renderPaging();
    renderResultMeta();
    setStatus(`搜索失败：${error.message}`);
  }
}

function selectedUnsupportedCount() {
  const byKey = new Map(allSources.map((source) => [source.key, source]));
  let count = 0;
  appState.selectedSites.forEach((siteKey) => {
    const source = byKey.get(siteKey);
    if (source && !(source.capabilities && source.capabilities.search)) {
      count += 1;
    }
  });
  return count;
}

function renderSourceChips(container, sources) {
  container.innerHTML = "";
  if (!sources.length) {
    container.innerHTML = '<div class="empty-inline">暂无推荐渠道。</div>';
    return;
  }

  sources.forEach((source) => {
    const node = document.createElement("span");
    node.className = "source-chip";
    node.textContent = source.display_name || source.key;
    container.appendChild(node);
  });
}

function renderSourceSelector() {
  const sources = allSources;
  sourceSelectorNode.innerHTML = "";

  sources.forEach((source) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "source-option";
    
    // 如果当前渠道存在于选中集合中，则添加高亮样式
    if (appState.selectedSites.has(source.key)) {
      button.classList.add("is-selected");
    }
    
    if (!(source.capabilities && source.capabilities.search)) {
      button.classList.add("is-download-only");
    }

    const content = document.createElement("div");
    content.className = "source-option-main";

    const titleRow = document.createElement("div");
    titleRow.className = "source-option-head";

    const title = document.createElement("span");
    title.className = "source-option-title";
    title.textContent = source.display_name || source.key;

    const status = document.createElement("span");
    status.className = source.capabilities && source.capabilities.search ? "source-status source-status-search" : "source-status source-status-download";
    status.textContent = source.capabilities && source.capabilities.search ? "可搜索" : "仅下载";

    titleRow.appendChild(title);
    titleRow.appendChild(status);

    const subtitle = document.createElement("span");
    subtitle.className = "source-option-subtitle";
    subtitle.textContent = `${source.key} · ${source.default_available ? "默认渠道" : "扩展渠道"}`;

    const note = document.createElement("span");
    note.className = "source-option-note";
    note.textContent = source.capabilities && source.capabilities.search
      ? "可参与 Web 搜索与聚合结果排序。"
      : "当前站点保留为可下载源，搜索会提示未实现。";

    content.appendChild(titleRow);
    content.appendChild(subtitle);
    content.appendChild(note);

    const marker = document.createElement("span");
    marker.className = "source-option-marker";
    marker.textContent = appState.selectedSites.has(source.key) ? "已选" : "选择";

    button.appendChild(content);
    button.appendChild(marker);
    button.addEventListener("click", () => toggleSource(source.key));
    sourceSelectorNode.appendChild(button);
  });

  selectedCountNode.textContent = String(appState.selectedSites.size);
  sourceSummaryNode.textContent = buildSourceSummary(sources);
}

function buildSourceSummary(sources) {
  const searchable = sources.filter((source) => source.capabilities && source.capabilities.search).length;
  const selectedUnsupported = selectedUnsupportedCount();
  if (selectedUnsupported > 0) {
    return `共 ${sources.length} 个渠道，其中 ${searchable} 个可搜索；已选择的渠道里有 ${selectedUnsupported} 个仅支持直链下载。`;
  }
  return `共 ${sources.length} 个渠道，其中 ${searchable} 个可搜索。`;
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
  if (!warnings.length) {
    warningsNode.innerHTML = '<div class="warning-item warning-item-success">本次搜索没有渠道警告。</div>';
    return;
  }

  warnings.forEach((warning) => {
    const node = document.createElement("div");
    node.className = "warning-item";
    node.textContent = `${warning.site}: ${warning.error}`;
    warningsNode.appendChild(node);
  });
}

function renderResults(results) {
  resultsNode.innerHTML = "";
  if (!results.length) {
    resultsNode.innerHTML = '<div class="empty-state">当前页没有可展示的结果。</div>';
    return;
  }

  results.forEach((result) => {
    const card = document.createElement("article");
    card.className = "result-card";

    const media = document.createElement("div");
    media.className = "result-media";
    if (result.cover_url) {
      const image = document.createElement("img");
      image.className = "result-cover";
      image.loading = "lazy";
      image.src = result.cover_url;
      image.alt = result.title || result.primary.title || result.primary.book_id;
      media.appendChild(image);
    } else {
      const placeholder = document.createElement("div");
      placeholder.className = "result-cover result-cover-placeholder";
      placeholder.textContent = (result.title || result.primary.title || "书").slice(0, 1);
      media.appendChild(placeholder);
    }

    const body = document.createElement("div");
    body.className = "result-body";

    const head = document.createElement("div");
    head.className = "result-head";

    const titleWrap = document.createElement("div");

    const title = document.createElement("h3");
    title.textContent = result.title || result.primary.title || result.primary.book_id;

    const author = document.createElement("p");
    author.className = "result-author";
    author.textContent = result.author || "未知作者";

    titleWrap.appendChild(title);
    titleWrap.appendChild(author);

    const action = document.createElement("button");
    action.className = "download-button";
    action.type = "button";
    action.textContent = "下载并导出";
    action.addEventListener("click", () => startDownloadTask(result, action));

    head.appendChild(titleWrap);
    head.appendChild(action);

    const meta = document.createElement("div");
    meta.className = "result-meta";
    meta.appendChild(badge(`首选 ${result.preferred_site}`));
    meta.appendChild(badge(`${result.source_count} 个来源`));
    if (result.latest_chapter) {
      meta.appendChild(badge(`最新 ${result.latest_chapter}`));
    }

    const desc = document.createElement("p");
    desc.className = "desc";
    desc.textContent = result.description || "暂无简介。";

    const sourceTitle = document.createElement("p");
    sourceTitle.className = "result-section-title";
    sourceTitle.textContent = "聚合来源";

    const sources = document.createElement("div");
    sources.className = "result-meta";
    (result.variants || []).forEach((variant) => {
      sources.appendChild(badge(`${variant.site}${variant.book_id ? `/${variant.book_id}` : ""}`));
    });

    const foot = document.createElement("div");
    foot.className = "card-foot";

    const info = document.createElement("div");
    info.className = "result-target";
    info.textContent = `下载目标：${result.primary.site}/${result.primary.book_id}`;

    foot.appendChild(info);

    body.appendChild(head);
    body.appendChild(meta);
    body.appendChild(desc);
    body.appendChild(sourceTitle);
    body.appendChild(sources);
    body.appendChild(foot);

    card.appendChild(media);
    card.appendChild(body);
    resultsNode.appendChild(card);
  });
}

function renderPaging() {
  resultCountNode.textContent = String(appState.results.length);
  pageIndicatorNode.textContent = `第 ${appState.page} 页`;
  prevPageButton.disabled = !appState.hasPrev;
  nextPageButton.disabled = !appState.hasNext;
}

function renderResultMeta() {
  if (!appState.lastKeyword) {
    resultMetaNode.textContent = "输入关键词后开始搜索。";
    return;
  }
  const totalLabel = appState.totalExact ? `${appState.total}` : `${appState.total}+`;
  resultMetaNode.textContent = `关键词“${appState.lastKeyword}”共返回 ${totalLabel} 条聚合结果，当前显示第 ${appState.page} 页。`;
}

async function startDownloadTask(result, button) {
  button.disabled = true;
  button.textContent = "正在创建任务...";

  try {
    const response = await fetch(`${root}/api/download-tasks`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        site: result.primary.site,
        book_id: result.primary.book_id,
      }),
    });
    const payload = await response.json();
    if (!response.ok) {
      throw new Error(payload.error || "download failed");
    }

    upsertTask(payload.task);
    startPollingTask(payload.task.id);
    setStatus(`已创建下载任务：${payload.task.site}/${payload.task.book_id}`);
  } catch (error) {
    setStatus(`创建下载任务失败：${error.message}`);
  } finally {
    button.disabled = false;
    button.textContent = "下载并导出";
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
      }
      if (task.status === "failed") {
        stopPollingTask(taskId);
        setStatus(`下载失败：${task.error}`);
      }
    } catch (error) {
      stopPollingTask(taskId);
      setStatus(`读取任务状态失败：${error.message}`);
    }
  };

  poll();
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
  tasksNode.innerHTML = "";

  if (!tasks.length) {
    tasksNode.innerHTML = '<div class="empty-state empty-state-compact">还没有下载任务。</div>';
    return;
  }

  tasks.forEach((task) => {
    const card = document.createElement("article");
    card.className = "task-card";

    const head = document.createElement("div");
    head.className = "task-head";

    const title = document.createElement("div");
    title.className = "task-title";
    title.textContent = task.title || `${task.site}/${task.book_id}`;

    const badgeNode = document.createElement("span");
    badgeNode.className = `task-status status-${task.status}`;
    badgeNode.textContent = formatTaskStatus(task);

    head.appendChild(title);
    head.appendChild(badgeNode);

    const meta = document.createElement("div");
    meta.className = "task-meta";
    meta.textContent = `${task.site}/${task.book_id}`;

    card.appendChild(head);
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
      progressText.textContent = `${task.completed_chapters}/${task.total_chapters}`;
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

    if (task.exported && task.exported.length) {
      const exported = document.createElement("ul");
      exported.className = "file-list";
      task.exported.forEach((path) => {
        const item = document.createElement("li");
        item.textContent = path;
        exported.appendChild(item);
      });
      card.appendChild(exported);
    }

    if (task.messages && task.messages.length) {
      const messages = document.createElement("div");
      messages.className = "task-messages";
      task.messages.slice(-6).forEach((message) => {
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

function badge(text) {
  const node = document.createElement("span");
  node.className = "source-badge";
  node.textContent = text;
  return node;
}

function setStatus(text) {
  statusNode.textContent = text;
}