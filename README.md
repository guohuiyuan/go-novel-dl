# go-novel-dl

> ⭐ 如果这个项目正在帮你省时间，欢迎顺手点一个 Star。Star 越多，作者越能确认这个工具确实有人在用，也会更有动力优先修复失效站点、适配新站点和更新版本。

<p align="center">
  <img src="./internal/web/templates/icon-256.png" alt="Novel Downloader Icon" width="220" />
</p>

`go-novel-dl` 是一个基于 Go 的多源小说下载工具，当前同时提供：

- CLI 交互式搜索与下载
- Web UI 搜索、详情、任务队列与配置中心
- 可选桌面壳层，底层仍复用同一套 Web 服务

项目当前的核心形态不是“单个下载脚本”，而是一个统一运行时：

- `internal/app.Runtime` 负责组装 `Config / Registry / Library / Pipeline / Exporter / State / Progress`
- `internal/site` 负责站点适配与 URL 解析
- `internal/store` 负责分阶段本地落盘
- `internal/web` 负责 Gin Web 服务与前端资源
- `internal/config` 负责 SQLite 配置中心

## 当前能力

- **聚合搜索**：并发请求多个站点，按书名/作者归并同作品变体，达量提前返回并取消剩余慢源
- **混合结果排序**：综合关键词匹配、站点优先级、简介完整度、封面可用性挑主结果
- **URL 直达**：CLI 与 Web 都支持直接输入小说链接解析
- **Web 阅读器**：按需拉章节正文，支持滚动续读、上下文预加载、主题/背景/字号/排版
- **阅读历史**：在线读过但未入书架的书也会出现在「阅读历史」；从书架移除有进度的书会降级为隐式记录而非丢失
- **异步下载任务**：前端轮询进度；下载速度走 30s 滑动窗口斜率估算，加减速/卡住/重试回退都能正确反映；非进行中任务可一键「📤 重新导出」换 txt/epub/html
- **多格式导出**：`txt` / `html` / `epub`，文件名模板支持 `{title}` `{author}` `{site}` `{book_id}` 占位符
- **分阶段存储**：原始 / 处理后 / 导出文件分层落盘
- **图片处理**：章节图片保留、EPUB 图片抓取与压缩；Web 全局可开启图片模糊
- **统一配置**：CLI 与 Web 共用 `data/site_catalog.db`；站点级支持可选登录/Cookie、镜像、并发、抓图、文字转换、缓存开关
- **站点兼容**：Alice Book House 加密章节、Linovelib 多页目录、ESJ Zone 免登录搜索、轻之文库/轻小说百科/神凑轻小说 差异处理
- **版本检查**：设置中心通过 GitHub 镜像（ghproxy/gh-proxy/mirror.ghproxy，或并发自动探测）查询最新 Release

![Web UI](./screenshots/web.png)

## 版本与变更

完整 changelog 见 [GitHub Releases](https://github.com/guohuiyuan/go-novel-dl/releases)。各版本的能力差异已并入下方对应章节，本节不再单独罗列。

## 架构概览

### 运行入口

- `cmd/novel-dl/main.go`
- `internal/cli/root.go`

CLI、Web、桌面壳层都复用同一套后端能力：

```text
CLI / Web / Desktop
    -> config.Load / LoadOrInitConfig
    -> app.NewRuntime
    -> site.Registry 选择站点适配器
    -> Search / DownloadPlan / FetchChapter
    -> store.Library 分阶段持久化
    -> pipeline.Runner 处理正文
    -> exporter.Service 导出文件
```

### 主要目录

```text
cmd/novel-dl/        CLI 入口
internal/app/        运行时组装、聚合搜索、下载编排
internal/cli/        Cobra 命令与终端交互
internal/config/     配置模型、SQLite 存储、配置合并
internal/exporter/   TXT / HTML / EPUB 导出
internal/model/      书籍、章节、搜索结果等模型
internal/pipeline/   文本处理流水线
internal/progress/   下载进度上报抽象
internal/site/       各站点适配器、能力声明、URL 解析
internal/state/      轻量运行状态，如界面语言
internal/store/      本地书籍分阶段存储
internal/textconv/   简繁转换与文本规范化
internal/ui/         CLI 控件与控制台交互
internal/web/        Gin 服务、Web API、前端模板
desktop/             桌面壳层，启动内嵌 Web 后端
docs/architecture.md 附加架构说明
```

### 聚合搜索

`internal/app/discovery.go` 中的 `HybridSearch` 会：

- 并发调用多个站点的 `Search`
- 当调用方设置 `OverallLimit` 时，聚合结果达到上限即返回，并通过 context 取消剩余慢源请求
- 单站点失败会被记录为 warning，不会让已收集到的其它站点结果失效
- 用归一化后的书名/作者进行分组
- 为每组结果选择一个主结果
- 保留全部来源变体，供 Web 详情和下载时切换

主结果优先级不只看命中度，还会参考：

- 默认站点优先顺序
- 是否有简介
- 是否有封面
- 是否有最新章节信息

### 配置模型

当前配置不是 TOML 文件，而是 SQLite：

- 配置文件路径：`data/site_catalog.db`
- 全局配置：保存在 `config_kvs.general_config`
- 站点配置：保存在 `site_catalog`
- 首次运行会自动初始化数据库

当前默认全局值来自 `internal/config/defaults.go`：

| 项目 | 默认值 |
| --- | --- |
| 原始数据目录 | `./data/raw_data` |
| 导出目录 | `./data/downloads` |
| 缓存目录 | `./data/novel_cache` |
| 禁用缓存 | `false` |
| 全局并发 | `4` |
| 最大连接数 | `10` |
| 超时 | `10s` |
| Web 每页 | `50` |
| CLI 每页 | `30` |
| Web 图片模糊化 | `false` |
| 默认导出格式 | `txt`, `epub` |
| 默认保留图片 | `true` |

也可以通过环境变量覆盖数据库位置：

```bash
NOVEL_DL_SITE_DB=/path/to/site_catalog.db
```

## 快速开始

### 1. 环境要求

- Go `1.25.1` 或更高

### 2. 首次运行

首次执行命令时会自动创建 `data/site_catalog.db`，也可以手动初始化：

```bash
go run ./cmd/novel-dl config init
```

### 3. 启动 Web

```bash
go run ./cmd/novel-dl web
```

默认访问：

```text
http://localhost:8080/novel
```

常用参数：

```bash
go run ./cmd/novel-dl web --port 18089 --no-browser
go run ./cmd/novel-dl web --page-size 30
```

### 4. CLI 搜索与下载

```bash
# 根命令支持直接传关键词进入交互式搜索
go run ./cmd/novel-dl 三体

# 显式调用 search
go run ./cmd/novel-dl search 三体

# 指定站点搜索
go run ./cmd/novel-dl search 三体 --site sfacg --site linovelib

# 直接通过 URL 下载
go run ./cmd/novel-dl download https://www.linovelib.com/novel/8.html

# 指定站点和书籍 ID 下载
go run ./cmd/novel-dl download --site sfacg 456123

# 按章节范围下载
go run ./cmd/novel-dl download --site esjzone 1660702902 --start 294593 --end 305803

# 下载但暂不导出
go run ./cmd/novel-dl download --site sfacg 456123 --no-export
```

### 5. 导出已下载内容

```bash
go run ./cmd/novel-dl export --site sfacg 456123 --format epub
go run ./cmd/novel-dl export --site sfacg 456123 --stage raw --format txt
```

## 命令概览

```text
novel-dl [keyword]               交互式搜索入口
novel-dl search <keyword>        聚合搜索并可继续下载
novel-dl download [book_id|url]  直接下载
novel-dl export [book_id]        导出本地已下载内容
novel-dl web                     启动 Web 服务
novel-dl config init             初始化 SQLite 配置库
novel-dl config sites            查看可管理站点配置
novel-dl config site-set ...     更新单个站点配置
novel-dl config set-lang ...     设置界面语言状态
novel-dl clean state             清理状态文件
novel-dl clean logs              清理日志
novel-dl clean cache             清理缓存
novel-dl clean book              清理书籍原始数据
```

几个当前最常用的参数：

### `search`

- `--site, -s`：指定搜索站点，可重复传入
- `--limit, -l`：总结果数上限；达到上限后会提前返回，不等待其它慢源超时
- `--site-limit`：单站点结果上限
- `--page-size`：CLI 每页显示数量
- `--timeout`：搜索超时秒数
- `--format`：选中结果后下载时的导出格式

### `download`

- `--site`：站点 key；如果传入的是 URL，可以省略并自动识别
- `--start`：起始章节 ID
- `--end`：结束章节 ID
- `--format`：导出格式
- `--no-export`：只下载，不导出

### `export`

- `--site`：站点 key
- `--stage`：导出阶段，留空时导出最新阶段
- `--format`：导出格式

### `config site-set`

- `--login-required`
- `--workers`
- `--fetch-images`
- `--locale-style`
- `--username`
- `--password`
- `--cookie`
- `--mirror`

示例：

```bash
# ESJ Zone 搜索无需配置；只有需要账号章节或希望复用登录态时再配置 Cookie/账号
go run ./cmd/novel-dl config site-set esjzone ^
  --cookie "your_cookie" ^
  --workers 8 ^
  --fetch-images=true ^
  --mirror https://www.esjzone.one
```

## Web UI 与 API

### Web 特性

- **搜索**：仅展示同时支持搜索和下载的站点；支持关键词与直接粘贴书籍 URL；按站点标签多选筛选取交集；命中当前页所需结果即返回，慢源/坏源不阻塞首屏
- **渠道测速**：一键并发探测可见渠道，每站最多采两次取最优（best-of-2），前端用耗时徽章展示
- **状态持久化**：渠道选择与标签筛选写入 `localStorage`，换浏览器/重启后保留
- **详情与阅读器**：`DownloadPlan` 拉目录与元数据并 TTL 缓存；章节正文按需 + 上下文预加载；阅读器支持主题/背景/字号/排版
- **下载任务**：异步执行 + 前端轮询；任务卡片显示进度/ETA；非进行中任务可点「📤 重新导出」弹 popover 重选格式重新生成（复用已下载章节）
- **书架**：支持文件夹分组（仅根目录可建）；进入 tab 自动刷新；「加入书架」与「下载」语义分离——前者只登记本地引用 + 触发缓存，不会触发完整导出
- **设置中心**：右侧垂直侧边栏入口，「阅读历史」与「设置」并列；顶部 Tab 精简为「搜索 / 书架 / 下载任务」
- **临时提示**：站点状态用顶部 toast 展示，不阻塞搜索、详情和阅读

### 路由前缀

```text
/novel
```

### 主要接口

```http
GET  /novel/api/meta
GET  /novel/api/version
POST /novel/api/version/check
GET  /novel/api/general-config
PUT  /novel/api/general-config
GET  /novel/api/site-configs
PUT  /novel/api/site-configs/:site
POST /novel/api/search
POST /novel/api/sources/speedtest
GET  /novel/api/books/detail
GET  /novel/api/chapter-content
POST /novel/api/download-tasks
GET  /novel/api/download-tasks/:id
GET  /novel/api/download-file
GET  /novel/healthz
```

搜索示例：

```http
POST /novel/api/search
Content-Type: application/json

{
  "keyword": "回到过去爱上你",
  "sites": ["alicesw", "sfacg"],
  "page": 1,
  "page_size": 20
}
```

详情示例：

```http
GET /novel/api/books/detail?site=alicesw&book_id=50427
```

章节正文示例：

```http
GET /novel/api/chapter-content?site=alicesw&book_id=38694&chapter_id=40207-1e77f397a3411
```

下载任务示例：

```http
POST /novel/api/download-tasks
Content-Type: application/json

{
  "site": "sfacg",
  "book_id": "456123",
  "formats": ["epub"]
}
```

全局配置中和 Web 相关的关键字段：

- `web_page_size`
- `cli_page_size`
- `blur_web_images`
- `disable_cache`
- `formats`
- `include_picture`
- `filename_template`（默认 `{title}_{author}`，支持 `{title}/{author}/{site}/{book_id}` 占位符）

## 数据目录

```text
data/
├─ site_catalog.db                 SQLite 配置中心
├─ raw_data/
│  └─ <site>/<book_id>/
│     ├─ book_info.raw.json
│     ├─ chapters.raw.sqlite
│     ├─ pipeline.json
│     └─ book_info.<stage>.json / chapters.<stage>.sqlite
├─ downloads/<site>/               导出文件
├─ novel_cache/                    运行缓存
├─ logs/                           调试日志
└─ go-novel-dl/state.json          轻量状态文件
```

`internal/store` 当前使用“按书分目录 + 按阶段落盘”的方式保存内容：

- 元数据：`book_info.<stage>.json`
- 章节正文：`chapters.<stage>.sqlite`
- 流水线状态：`pipeline.json`

## 当前内置站点

### 已注册站点

当前默认 `Registry` 中已注册、可由 URL 解析和下载流程使用的站点：

```text
aaatxt
alicesw
alphapolis
biquge345
biquge5
ciweimao
ciyuanji
esjzone
faloo
fanqienovel
fsshu
haiwaishubao
hongxiuzhao
ixdzs8
kadokado
linovel
linovelib
lnovel
mjyhb
n17k
n69shuba
n8novel
novalpie
novelpia
ruochu
sfacg
shencou
shuhaige
syosetu
syosetu18
syosetu_org
tianyabooks
tongrenshe
wenku8
yibige
```

### 默认启用渠道

当前 `defaultAvailableSiteKeys` 覆盖以下站点：

```text
aaatxt
esjzone
linovelib
linovel
lnovel
shencou
biquge345
biquge5
ixdzs8
ruochu
fanqienovel
n17k
faloo
sfacg
ciyuanji
ciweimao
n8novel
shuhaige
tianyabooks
alicesw
kadokado
haiwaishubao
mjyhb
novelpia
```

代码中保留但当前未注册启用的站点包括：

```text
westnovel
yodu
piaotia
n23qb
n37yq
akatsuki_novels
czbooks
xiguashuwu
qbtr
uaa
```

这些站点的适配器或测试仍在代码中，主要因站点稳定性、403/Cloudflare、浏览器指纹、登录或网络连通性问题暂不作为当前 active 渠道。

### 能力分组

同时支持搜索和下载：

```text
aaatxt
alicesw
biquge345
biquge5
ciyuanji
ciweimao
esjzone
faloo
fsshu
haiwaishubao
ixdzs8
kadokado
linovel
linovelib
n17k
n8novel
novalpie
novelpia
ruochu
sfacg
shuhaige
tianyabooks
tongrenshe
```

仅支持下载：

```text
alphapolis
fanqienovel
hongxiuzhao
lnovel
mjyhb
n69shuba
shencou
syosetu
syosetu18
syosetu_org
wenku8
yibige
```

需要登录或鉴权：

```text
esjzone
novalpie
```

ESJ Zone 搜索和详情无需预先登录；只有遇到需要认证的章节时才可能需要 Cookie/账号。

### 可在配置中心管理的站点

当前 SQLite `site_catalog` 覆盖以下站点：

```text
aaatxt
alicesw
alphapolis
esjzone
faloo
fsshu
biquge5
ixdzs8
linovelib
linovel
lnovel
shencou
ruochu
fanqienovel
sfacg
ciyuanji
ciweimao
kadokado
novalpie
novelpia
haiwaishubao
mjyhb
n17k
n8novel
shuhaige
syosetu
syosetu18
syosetu_org
tianyabooks
tongrenshe
```

这意味着：

- 上面这些站点可以通过 Web 设置中心或 `config site-set` 直接改配置
- 其它已注册站点仍可使用，但当前不在 SQLite 站点配置中心里暴露

## 构建、测试与部署

### 本地构建

```bash
go build ./...
go build -o novel-dl ./cmd/novel-dl
```

### 测试

```bash
go test ./...
```

### Docker

```bash
docker compose build
docker compose up -d
```

启动后访问：

```text
http://localhost:8080/novel
```

镜像默认命令等价于：

```bash
./novel-dl web --port 8080 --no-browser
```

## 许可证

本项目遵循 GNU Affero General Public License v3.0（AGPL-3.0）。详情见 [LICENSE](LICENSE)。

## 使用说明

- 本项目仅供学习、研究与个人技术验证使用
- 请遵守目标站点服务条款、版权要求与当地法律法规
- 部分站点可能受限流、Cloudflare、登录态、反爬或网络连通性影响
- 站点能力表表示“代码已实现”，不代表目标站点长期稳定可用


## Star History

[![Star History Chart](https://api.star-history.com/image?repos=guohuiyuan/go-novel-dl&type=date&legend=top-left)](https://www.star-history.com/?repos=guohuiyuan%2Fgo-novel-dl&type=date&legend=top-left)
