const appState = {
  scope: "default",
  results: [],
};

const root = window.__NOVEL_DL__.root;
const defaultSources = window.__NOVEL_DL__.defaultSources || [];
const allSources = window.__NOVEL_DL__.allSources || [];

const keywordInput = document.getElementById("keyword");
const searchForm = document.getElementById("searchForm");
const statusNode = document.getElementById("status");
const warningsNode = document.getElementById("warnings");
const resultsNode = document.getElementById("results");
const resultCountNode = document.getElementById("resultCount");
const defaultSourcesNode = document.getElementById("defaultSources");

renderSourceChips(defaultSourcesNode, defaultSources);

document.querySelectorAll(".scope-pill").forEach((button) => {
  button.addEventListener("click", () => {
    appState.scope = button.dataset.scope || "default";
    document.querySelectorAll(".scope-pill").forEach((node) => {
      node.classList.toggle("is-active", node === button);
    });
    statusNode.textContent = appState.scope === "all"
      ? "当前搜索范围：全部搜索源"
      : "当前搜索范围：默认可用源";
  });
});

searchForm.addEventListener("submit", async (event) => {
  event.preventDefault();

  const keyword = keywordInput.value.trim();
  if (!keyword) {
    setStatus("请输入关键字");
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
        limit: 20,
        site_limit: 6
      })
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

function renderSourceChips(container, sources) {
  container.innerHTML = "";
  sources.forEach((source) => {
    const node = document.createElement("span");
    node.className = "source-chip";
    node.textContent = source.display_name || source.key;
    container.appendChild(node);
  });
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
    action.addEventListener("click", async () => {
      action.disabled = true;
      action.textContent = "下载中...";
      try {
        const response = await fetch(`${root}/api/download`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            site: result.primary.site,
            book_id: result.primary.book_id
          })
        });
        const payload = await response.json();
        if (!response.ok) {
          throw new Error(payload.error || "download failed");
        }

        const files = document.createElement("ul");
        files.className = "file-list";
        (payload.exported || []).forEach((path) => {
          const item = document.createElement("li");
          item.textContent = path;
          files.appendChild(item);
        });
        card.appendChild(files);
        setStatus(`已导出 ${payload.site}/${payload.book_id}`);
      } catch (error) {
        setStatus(`下载失败: ${error.message}`);
      } finally {
        action.disabled = false;
        action.textContent = "下载并导出";
      }
    });

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

function badge(text) {
  const node = document.createElement("span");
  node.className = "source-badge";
  node.textContent = text;
  return node;
}

function setStatus(text) {
  statusNode.textContent = text;
}
