# go-novel-dl

<p align="center">
  <img src="./internal/web/templates/icon-256.png" alt="Novel Downloader Icon" width="220" />
</p>

`go-novel-dl` 是一个以 Go 实现的多源聚合小说下载器，提供命令行和 Web 两种界面。

项目当前以 SQLite 配置中心为主：全局参数与站点参数统一存储，Web 与 CLI 共用同一套配置数据。

## 功能特性

- **多源聚合搜索**：并发搜索多个小说站点，结果自动聚合去重
- **交互式 TUI**：基于 Bubble Tea 的终端交互界面，支持翻页、选择、批量下载
- **Web UI**：现代化 Web 界面，支持搜索、详情查看、任务队列、浏览器下载
- **统一设置中心**：全局配置与站点配置在同一页面编辑，无需切换
- **多格式导出**：支持 TXT / EPUB 格式
- **章节范围下载**：可指定起止章节
- **可配置分页**：CLI 和 Web 分页大小可通过配置或命令行调整
- **ESJ 优化链路**：支持 Cookie 优先登录、并发章节抓取、图片抓取开关

## 快速开始

### 1. 初始化配置

```bash
go run ./cmd/novel-dl config init
```

默认会初始化并使用 SQLite 配置库：`data/site_catalog.db`。

### 2. 交互式搜索下载

```bash
# 直接进入交互式搜索
go run ./cmd/novel-dl 三体

# 或显式调用 search 命令
go run ./cmd/novel-dl search 斗罗

# 指定渠道搜索
go run ./cmd/novel-dl search 斗罗 --site sfacg --site n17k
```

### 3. 启动 Web UI

```bash
go run ./cmd/novel-dl web
```

访问 http://localhost:8080/novel

```bash
# 自定义端口，不自动打开浏览器
go run ./cmd/novel-dl web --port 18080 --no-browser

# 指定每页显示数量
go run ./cmd/novel-dl web --page-size 30
```

### 4. 直接下载

```bash
# 通过 URL 下载
go run ./cmd/novel-dl download https://www.esjzone.cc/detail/1660702902.html

# 指定站点和书号
go run ./cmd/novel-dl download --site linovelib 8

# 按章节范围下载
go run ./cmd/novel-dl download --site esjzone 1660702902 --start 294593 --end 305803

# 仅下载不导出
go run ./cmd/novel-dl download --site sfacg 248014 --no-export
```

### 5. 导出已下载内容

```bash
go run ./cmd/novel-dl export --site esjzone 1660702902 --format epub
```

## 命令概览

```text
novel-dl [keyword]              # 交互式聚合搜索
novel-dl search <keyword>       # 搜索并选择下载
novel-dl download [url|book_id] # 直接下载
novel-dl export [book_id]       # 导出已下载内容
novel-dl web                    # 启动 Web 服务
novel-dl config init            # 初始化配置
novel-dl config sites           # 查看站点配置与参数实现状态
novel-dl config site-set ...    # 修改站点配置
novel-dl config set-lang zh_CN  # 设置语言
novel-dl clean [state|logs|cache|book]  # 清理数据
```

### 命令行参数

#### search

| 参数             | 说明             | 默认值       |
| ---------------- | ---------------- | ------------ |
| `-s, --site`   | 指定搜索渠道     | 所有可用渠道 |
| `-l, --limit`  | 总结果数上限     | 150          |
| `--site-limit` | 单渠道结果数上限 | 30           |
| `--page-size`  | 每页显示数量     | 读取配置     |
| `--timeout`    | 请求超时秒数     | 5            |
| `--format`     | 导出格式         | 读取配置     |

#### web

| 参数             | 说明             | 默认值   |
| ---------------- | ---------------- | -------- |
| `-p, --port`   | 服务端口         | 8080     |
| `--no-browser` | 不自动打开浏览器 | false    |
| `--page-size`  | 每页显示数量     | 读取配置 |

#### download

| 参数            | 说明         |
| --------------- | ------------ |
| `--site`      | 渠道 key     |
| `--start`     | 起始章节 ID  |
| `--end`       | 结束章节 ID  |
| `--format`    | 导出格式     |
| `--no-export` | 仅下载不导出 |

## Web UI

### 功能

- **多渠道搜索**：支持勾选多个渠道并发搜索
- **分页浏览**：搜索结果分页显示，页码可配置
- **详情查看**：点击封面查看小说简介、章节目录
- **任务队列**：下载任务实时显示进度
- **浏览器下载**：已完成的任务可直接点击文件名下载
- **统一设置中心**：右上角设置按钮或左侧“设置中心”按钮，均打开同一设置弹窗
- **同页编辑**：全局参数与站点参数同屏展示、分别保存

### Web API

```http
# 获取渠道元数据
GET /novel/api/meta

# 搜索
POST /novel/api/search
Content-Type: application/json
{
  "keyword": "斗罗",
  "sites": ["n17k", "sfacg"],
  "page": 1,
  "page_size": 50
}

# 获取书籍详情
GET /novel/api/books/detail?site=sfacg&book_id=248014

# 创建下载任务
POST /novel/api/download-tasks
Content-Type: application/json
{
  "site": "sfacg",
  "book_id": "248014"
}

# 查询任务状态
GET /novel/api/download-tasks/:id

# 下载文件
GET /novel/api/download-file?path=/path/to/file
```

## 配置说明

### 运行时配置来源

- 运行时读取：`data/site_catalog.db`
- 全局参数：SQLite `config_kvs`（键 `general_config`）
- 站点参数：SQLite `site_catalog` 表
- 首次检测到 `data/site_catalog.db` 不存在时，会用内嵌 `internal/config/resources/settings.sample.toml` 初始化数据库；后续运行只从 SQLite 读取和修改配置

### 全局参数（示例）

```toml
[general]
# 分页配置
web_page_size = 50    # Web 界面每页显示数量
cli_page_size = 30    # CLI 界面每页显示数量

# 下载配置
request_interval = 0.5
workers = 4
max_connections = 10
timeout = 10.0

[general.output]
formats = ["txt", "epub"]
append_timestamp = true

# 站点配置
[sites.esjzone]
login_required = true
username = "your_username"
password = "your_password"
```

完整字段示例见 `internal/config/resources/settings.sample.toml`。

### 站点参数命令示例

```bash
# 查看站点配置
go run ./cmd/novel-dl config sites

# 修改 ESJ 站点配置
go run ./cmd/novel-dl config site-set esjzone \
  --login-required \
  --username your_name \
  --password your_password \
  --workers 8 \
  --fetch-images=true
```

## 站点能力矩阵

| Key             | 下载 | 搜索 | 登录 |
| --------------- | :--: | :--: | :--: |
| `biquge345`   |  ✓  |  ✓  |  -  |
| `biquge5`     |  ✓  |  ✓  |  -  |
| `ciweimao`    |  ✓  |  ✓  |  -  |
| `ciyuanji`    |  ✓  |  ✓  |  -  |
| `esjzone`     |  ✓  |  ✓  |  ✓  |
| `faloo`       |  ✓  |  ✓  |  -  |
| `fanqienovel` |  ✓  |  -  |  -  |
| `fsshu`       |  ✓  |  ✓  |  -  |
| `hongxiuzhao` |  ✓  |  -  |  -  |
| `ixdzs8`      |  ✓  |  ✓  |  -  |
| `linovelib`   |  ✓  |  ✓  |  -  |
| `n17k`        |  ✓  |  ✓  |  -  |
| `n23qb`       |  ✓  |  ✓  |  -  |
| `n69shuba`    |  ✓  |  -  |  -  |
| `novalpie`    |  ✓  |  -  |  ✓  |
| `piaotia`     |  ✓  |  ✓  |  -  |
| `qbtr`        |  ✓  |  ✓  |  -  |
| `ruochu`      |  ✓  |  ✓  |  -  |
| `sfacg`       |  ✓  |  ✓  |  -  |
| `wenku8`      |  ✓  |  -  |  -  |
| `westnovel`   |  ✓  |  ✓  |  -  |
| `yibige`      |  ✓  |  -  |  -  |
| `yodu`        |  ✓  |  ✓  |  -  |

## 数据目录

```text
data/
├── site_catalog.db            # SQLite 配置数据库（全局+站点）
├── raw_data/<site>/<book>/    # 原始数据
├── downloads/                 # 导出文件
├── novel_cache/               # 缓存
├── logs/                      # 日志
└── go-novel-dl/state.json    # 状态
```

## 架构概览

```text
cmd/novel-dl/          # CLI 入口
internal/app/          # Runtime、任务编排、下载流程
internal/site/         # 各站点抓取器与解析器（含 ESJ）
internal/config/       # 配置模型、SQLite 存储、加载逻辑
internal/exporter/     # TXT/HTML/EPUB 导出
internal/web/          # Web API 与前端模板
internal/store/        # 本地书籍存储
internal/pipeline/     # 处理链
internal/textconv/     # 文本规范化
```

下载主流程：

1. 聚合搜索或直接解析 URL 得到 `site/book_id`
2. Runtime 按站点配置建立抓取客户端
3. 站点层抓取目录并并发下载章节
4. Pipeline 处理正文
5. Exporter 导出 TXT/EPUB（含图片资源处理）

## Docker 部署

```bash
# 构建
docker compose build

# 启动
docker compose up -d

# 访问
http://localhost:8080/novel
```

## 构建与测试

```bash
# 构建
go build ./...

# 测试
go test ./...

# 构建可执行文件
go build -o novel-dl ./cmd/novel-dl
```

## 致敬与参考

本项目在设计和演进过程中，参考并致敬以下优秀开源项目：

- [saudadez21/novel-downloader](https://github.com/saudadez21/novel-downloader)
- [mikoto710/esj-novel-downloader](https://github.com/mikoto710/esj-novel-downloader)

项目定位：

- `saudadez21/novel-downloader`：偏通用多站点下载框架，强调插件化站点支持、配置体系与端到端下载导出流程。
- `mikoto710/esj-novel-downloader`：偏 ESJ 垂直专项工具，重点在 ESJ 登录态处理、章节解析与导出链路优化。

感谢两位作者对小说下载生态与工程实现思路的贡献。

## 注意事项

- 站点能力基于代码实现，非实时健康检查
- 部分站点可能受 Cloudflare、限流、登录态或反爬策略影响
- `esjzone` 和 `novalpie` 需要登录才能使用
