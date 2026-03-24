# go-novel-dl

`go-novel-dl` 是一个以 Go 实现的小说下载器，当前同时提供：

- 交互式多源并发搜索 CLI
- 单书下载 / 导出 CLI
- Web UI

当前主入口是 `novel-dl`。`novel-cli` 仍保留为兼容入口，但底层调用的是同一套命令。

## 当前能力

- 支持按一个或多个站点做多源并发搜索
- 搜索结果支持聚合排序、分页和简介展示
- Web 端只展示“可搜索且可下载”的源
- Web 端支持多渠道并发搜索、翻页、创建下载任务
- 支持 TXT / HTML / EPUB 导出
- 支持按章节范围下载
- 原始数据、导出文件和运行状态统一落到 `data/`

## 快速开始

### 1. 初始化配置

```bash
go run ./cmd/novel-dl config init
```

默认配置文件会写到 `data/settings.toml`。

### 2. 交互式搜索下载

```bash
go run ./cmd/novel-dl 三体
```

或显式调用搜索命令：

```bash
go run ./cmd/novel-dl search 三体
go run ./cmd/novel-dl search 斗罗 --site sfacg --site n17k
go run ./cmd/novel-dl search vtuber --all-sites
```

### 3. 启动 Web UI

```bash
go run ./cmd/novel-dl web
```

默认地址：

```text
http://localhost:8080/novel
```

可用参数：

```bash
go run ./cmd/novel-dl web --port 18080 --no-browser
```

### 4. 直接下载

```bash
go run ./cmd/novel-dl download --site esjzone 1660702902
go run ./cmd/novel-dl download https://www.esjzone.cc/detail/1660702902.html
go run ./cmd/novel-dl download --site faloo 1482723 --start 1 --end 52
```

### 5. 导出已下载内容

```bash
go run ./cmd/novel-dl export --site esjzone 1660702902 --format epub
```

## Web UI

Web 端当前行为和代码保持一致：

- 页面路由前缀是 `/novel`
- `default_sources` 只包含“默认启用且可搜索可下载”的源
- `all_sources` 只包含“全部可搜索可下载”的源
- 搜索支持勾选多个渠道并发搜索
- 搜索结果支持分页
- 结果卡片会展示简介、封面、来源聚合信息和下载目标
- 显式传入不可搜索源时，`/novel/api/search` 会直接返回 `400`
- Web 多源搜索会为每个渠道启动一个 goroutine 并发请求，再聚合结果排序

### Web API

#### 获取渠道元数据

```http
GET /novel/api/meta
```

#### 搜索

```http
POST /novel/api/search
Content-Type: application/json
```

示例：

```json
{
  "keyword": "斗罗",
  "sites": ["n17k", "sfacg"],
  "page": 1,
  "page_size": 5
}
```

搜索响应包含：

- `results`
- `warnings`
- `page`
- `page_size`
- `total`
- `total_exact`
- `has_prev`
- `has_next`

#### 创建下载任务

```http
POST /novel/api/download-tasks
Content-Type: application/json
```

示例：

```json
{
  "site": "sfacg",
  "book_id": "248014"
}
```

## 命令概览

```text
novel-dl [keyword]
novel-dl search keyword
novel-dl download [book_ids | url]
novel-dl export [book_id ...]
novel-dl config init
novel-dl config set-lang zh_CN
novel-dl clean state
novel-dl clean logs
novel-dl clean cache
novel-dl clean book
novel-dl web
```

### 根命令

```bash
go run ./cmd/novel-dl --help
```

当前根命令行为：

- `novel-dl [keyword]`：进入交互式多源并发搜索
- `--site`：限制交互式搜索站点
- `--all-sites`：使用全部可搜索源，而不是默认源

## 站点能力矩阵

下面这张表直接对应各站点 `Capabilities()` 的当前代码状态，不代表源站一定长期稳定可用。

| Key | 下载 | 搜索 | 登录 |
| --- | --- | --- | --- |
| `biquge345` | 是 | 是 | 否 |
| `biquge5` | 是 | 是 | 否 |
| `ciweimao` | 是 | 是 | 否 |
| `ciyuanji` | 是 | 是 | 否 |
| `esjzone` | 是 | 是 | 是 |
| `faloo` | 是 | 是 | 否 |
| `fanqienovel` | 是 | 否 | 否 |
| `fsshu` | 是 | 是 | 否 |
| `hongxiuzhao` | 是 | 否 | 否 |
| `ixdzs8` | 是 | 是 | 否 |
| `linovelib` | 是 | 是 | 否 |
| `n17k` | 是 | 是 | 否 |
| `n23qb` | 是 | 是 | 否 |
| `n69shuba` | 是 | 否 | 否 |
| `novalpie` | 是 | 否 | 是 |
| `piaotia` | 是 | 是 | 否 |
| `qbtr` | 是 | 是 | 否 |
| `ruochu` | 是 | 是 | 否 |
| `sfacg` | 是 | 是 | 否 |
| `wenku8` | 是 | 否 | 否 |
| `westnovel` | 是 | 是 | 否 |
| `yibige` | 是 | 否 | 否 |
| `yodu` | 是 | 是 | 否 |

### 当前可搜索且可下载的源

共 17 个：

`biquge345`、`biquge5`、`ciweimao`、`ciyuanji`、`esjzone`、`faloo`、`fsshu`、`ixdzs8`、`linovelib`、`n17k`、`n23qb`、`piaotia`、`qbtr`、`ruochu`、`sfacg`、`westnovel`、`yodu`

### 当前仅下载、不支持搜索的源

共 6 个：

`fanqienovel`、`hongxiuzhao`、`n69shuba`、`novalpie`、`wenku8`、`yibige`

### Web 默认搜索源

默认启用的是 `defaultAvailableSiteKeys` 与“可搜索且可下载”能力交集，当前共 9 个：

`esjzone`、`westnovel`、`yodu`、`linovelib`、`n23qb`、`ruochu`、`sfacg`、`ciyuanji`、`ciweimao`

## 常用命令示例

```bash
# 通过 URL 自动识别站点并下载
go run ./cmd/novel-dl download https://www.esjzone.cc/detail/1660702902.html

# 指定站点和书号下载
go run ./cmd/novel-dl download --site westnovel wuxia-ynyh
go run ./cmd/novel-dl download --site linovelib 8
go run ./cmd/novel-dl download --site n23qb 12282
go run ./cmd/novel-dl download --site ciweimao 100812822

# 按章节范围下载
go run ./cmd/novel-dl download --site esjzone 1660702902 --start 294593 --end 305803

# 下载后导出
go run ./cmd/novel-dl export --site esjzone 1660702902 --format epub

# 启动 Web UI
go run ./cmd/novel-dl web --port 18080 --no-browser
```

## 配置说明

默认配置文件：

```text
data/settings.toml
```

配置模板：

```text
internal/config/resources/settings.sample.toml
```

当前常见配置段包括：

- `[general]`
- `[general.output]`
- `[general.parser]`
- `[general.debug]`
- `[[general.processors]]`
- `[sites.<site>]`
- `[plugins]`

## 数据目录结构

所有运行数据统一写入 `data/`。

原始数据：

```text
data/raw_data/<site>/<book_id>/book_info.<stage>.json
data/raw_data/<site>/<book_id>/chapters.<stage>.sqlite
data/raw_data/<site>/<book_id>/pipeline.json
```

其他运行数据：

```text
data/downloads/
data/logs/
data/novel_cache/
data/go-novel-dl/state.json
```

## 测试与构建

```bash
go test ./...
go build ./...
```

更多测试组织说明见：

- `tests/README.md`

## 说明

- 站点能力矩阵来自当前代码里的 `Capabilities()`，不是实时联网健康检查结论。
- 某些站点会受到 Cloudflare、限流、登录态或反爬策略影响，真实可用性会波动。
- `esjzone` 和 `novalpie` 当前带有登录能力标记；其中 `novalpie` 已保留为“可下载但不可搜索”。
