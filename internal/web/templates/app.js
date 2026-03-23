const root = window.__NOVEL_DL__.root;
const defaultSources = window.__NOVEL_DL__.defaultSources || [];
const allSources = window.__NOVEL_DL__.allSources || [];

const appState = {
  scope: "default",
  results: [],
  selectedSites: new Set(defaultSources.map((source) => source.key)),
  tasks: new Map(),
  pollers: new Map(),
};

const keywordInput = document.getElementById("keyword");
const searchForm = document.getElementById("searchForm");
const statusNode = document.getElementById("status");
const warningsNode = document.getElementById("warnings");
const resultsNode = document.getElementById("results");
const resultCountNode = document.getElementById("resultCount");
const taskCountNode = document.getElementById("taskCount");
const selectedCountNode = document.getElementById("selectedCount");
const defaultSourcesNode = document.getElementById("defaultSources");
const sourceSelectorNode = document.getElementById("sourceSelector");
const tasksNode = document.getElementById("tasks");
const selectAllSourcesButton = document.getElementById("selectAllSources");
const clearSourcesButton = document.getElementById("clearSources");

renderSourceChips(defaultSourcesNode, defaultSources);
renderSourceSelector();
renderTasks();
setStatus("选择搜索源后输入关键字开始搜索。");

document.querySelectorAll(".scope-pill").forEach((button) => {
  button.addEventListener("click", () => {
    appState.scope = button.dataset.scope || "default";
    document.querySelectorAll(".scope-pill").forEach((node) => {
      node.classList.toggle("is-active", node === button);
    });
    appState.selectedSites = new Set(currentScopeSources().map((source) => source.key));
    renderSourceSelector();
    setStatus(appState.scope === "all" ? "当前范围：全部源" : "当前范围：默认可用源");
  });
});

selectAllSourcesButton.addEventListener("click", () => {
  appState.selectedSites = new Set(currentScopeSources().map((source) => source.key));
  renderSourceSelector();
  setStatus(`已选择 ${appState.selectedSites.size} 个搜索源`);
});

clearSourcesButton.addEventListener("click", () => {
  appState.selectedSites = new Set();
  renderSourceSelector();
  setStatus("已清空搜索源选择");
});

searchForm.addEventListener("submit", async (event) => {
  event.preventDefault();

  const keyword = keywordInput.value.trim();
  if (!keyword) {
    setStatus("请输入关键字");
    return;
  }
  if (appState.selectedSites.size === 0) {
    setStatus("请至少选择一个搜索源");
    return;
  }

  setStatus(`正在搜索 “${keyword}”`);
  renderWarnings([]);

  try {
    const response = await fetch(`${root}/api/search`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        keyword,
        scope: appState.scope,
        sites: Array.from(appState.selectedSites),
        limit: 20,
        site_limit: 6,
      }),
    });
    const payload = await response.json();
    if (!response.ok) {
      throw new Error(payload.error || "search failed");
    }

    appState.results = payload.results || [];
    renderResults(appState.results);
    renderWarnings(payload.warnings || []);
    resultCountNode.textContent = String(appState.results.length);
    setStatus(appState.results.length === 0 ? "没有找到结果" : `找到 ${appState.results.length} 个聚合结果`);
  } catch (error) {
    renderResults([]);
    resultCountNode.textContent = "0";
    setStatus(`搜索失败: ${error.message}`);
  }
});

function currentScopeSources() {
  return appState.scope === "all" ? allSources : defaultSources;
}

function renderSourceChips(container, sources) {
  container.innerHTML = "";
  sources.forEach((source) => {
    const node = document.createElement("span");
    node.className = "source-chip";
    node.textContent = source.display_name || source.key;
    container.appendChild(node);
  });
}

function renderSourceSelector() {
  const sources = currentScopeSources();
  sourceSelectorNode.innerHTML = "";

  sources.forEach((source) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "source-option";
    if (appState.selectedSites.has(source.key)) {
      button.classList.add("is-selected");
    }

    const title = document.createElement("span");
    title.className = "source-option-title";
    title.textContent = source.display_name || source.key;

    const subtitle = document.createElement("span");
    subtitle.className = "source-option-subtitle";
    subtitle.textContent = source.key;

    button.appendChild(title);
    button.appendChild(subtitle);
    button.addEventListener("click", () => toggleSource(source.key));
    sourceSelectorNode.appendChild(button);
  });

  selectedCountNode.textContent = String(appState.selectedSites.size);
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
  warnings.forEach((warning) => {
    const node = document.createElement("div");
    node.className = "warning-item";
    node.textContent = `${warning.site} 搜索失败: ${warning.error}`;
    warningsNode.appendChild(node);
  });
}

function renderResults(results) {
  resultsNode.innerHTML = "";
  if (!results.length) {
    resultsNode.innerHTML = '<div class="empty-state">没有可展示的结果。</div>';
    return;
  }

  results.forEach((result) => {
    const card = document.createElement("article");
    card.className = "result-card";

    const title = document.createElement("h3");
    title.textContent = result.title || result.primary.title || result.primary.book_id;

    const meta = document.createElement("div");
    meta.className = "result-meta";
    meta.appendChild(badge(result.author || "未知作者"));
    meta.appendChild(badge(`首选源 ${result.preferred_site}`));
    meta.appendChild(badge(`${result.source_count} 个来源`));

    const sources = document.createElement("div");
    sources.className = "result-meta";
    (result.variants || []).forEach((variant) => {
      sources.appendChild(badge(variant.site));
    });

    const desc = document.createElement("p");
    desc.className = "desc";
    desc.textContent = result.description || "没有简介。";

    const foot = document.createElement("div");
    foot.className = "card-foot";

    const info = document.createElement("div");
    info.className = "desc";
    info.textContent = `下载目标: ${result.primary.site}/${result.primary.book_id}`;

    const action = document.createElement("button");
    action.className = "download-button";
    action.type = "button";
    action.textContent = "下载并导出";
    action.addEventListener("click", () => startDownloadTask(result, action));

    foot.appendChild(info);
    foot.appendChild(action);

    card.appendChild(title);
    card.appendChild(meta);
    card.appendChild(sources);
    card.appendChild(desc);
    card.appendChild(foot);
    resultsNode.appendChild(card);
  });
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
    setStatus(`已创建下载任务 ${payload.task.site}/${payload.task.book_id}`);
  } catch (error) {
    setStatus(`创建下载任务失败: ${error.message}`);
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
        setStatus(`下载完成: ${task.site}/${task.book_id}`);
      }
      if (task.status === "failed") {
        stopPollingTask(taskId);
        setStatus(`下载失败: ${task.error}`);
      }
    } catch (error) {
      stopPollingTask(taskId);
      setStatus(`读取任务状态失败: ${error.message}`);
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
      current.textContent = `当前章节: ${task.current_chapter}`;
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
