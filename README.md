# go-novel-dl

`go-novel-dl` 是一个以 Go 实现的多源聚合小说下载器，提供命令行和 Web 两种界面。

## 功能特性

- **多源聚合搜索**：并发搜索多个小说站点，结果自动聚合去重
- **交互式 TUI**：基于 Bubble Tea 的终端交互界面，支持翻页、选择、批量下载
- **Web UI**：现代化 Web 界面，支持搜索、详情查看、任务队列、浏览器下载
- **多格式导出**：支持 TXT / EPUB 格式
- **章节范围下载**：可指定起止章节
- **可配置分页**：CLI 和 Web 分页大小可通过配置或命令行调整

## 快速开始

### 1. 初始化配置

```bash
go run ./cmd/novel-dl config init
```

配置文件默认写入 `data/settings.toml`。

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
novel-dl config set-lang zh_CN  # 设置语言
novel-dl clean [state|logs|cache|book]  # 清理数据
```

### 命令行参数

#### search

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `-s, --site` | 指定搜索渠道 | 所有可用渠道 |
| `-l, --limit` | 总结果数上限 | 150 |
| `--site-limit` | 单渠道结果数上限 | 30 |
| `--page-size` | 每页显示数量 | 读取配置 |
| `--timeout` | 请求超时秒数 | 5 |
| `--format` | 导出格式 | 读取配置 |

#### web

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `-p, --port` | 服务端口 | 8080 |
| `--no-browser` | 不自动打开浏览器 | false |
| `--page-size` | 每页显示数量 | 读取配置 |

#### download

| 参数 | 说明 |
|------|------|
| `--site` | 渠道 key |
| `--start` | 起始章节 ID |
| `--end` | 结束章节 ID |
| `--format` | 导出格式 |
| `--no-export` | 仅下载不导出 |

## Web UI

### 功能

- **多渠道搜索**：支持勾选多个渠道并发搜索
- **分页浏览**：搜索结果分页显示，页码可配置
- **详情查看**：点击封面查看小说简介、章节目录
- **任务队列**：下载任务实时显示进度
- **浏览器下载**：已完成的任务可直接点击文件名下载

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

配置文件：`data/settings.toml`

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

完整配置示例见 `internal/config/resources/settings.sample.toml`。

## 站点能力矩阵

| Key | 下载 | 搜索 | 登录 |
| --- | :---: | :---: | :---: |
| `biquge345` | ✓ | ✓ | - |
| `biquge5` | ✓ | ✓ | - |
| `ciweimao` | ✓ | ✓ | - |
| `ciyuanji` | ✓ | ✓ | - |
| `esjzone` | ✓ | ✓ | ✓ |
| `faloo` | ✓ | ✓ | - |
| `fanqienovel` | ✓ | - | - |
| `fsshu` | ✓ | ✓ | - |
| `hongxiuzhao` | ✓ | - | - |
| `ixdzs8` | ✓ | ✓ | - |
| `linovelib` | ✓ | ✓ | - |
| `n17k` | ✓ | ✓ | - |
| `n23qb` | ✓ | ✓ | - |
| `n69shuba` | ✓ | - | - |
| `novalpie` | ✓ | - | ✓ |
| `piaotia` | ✓ | ✓ | - |
| `qbtr` | ✓ | ✓ | - |
| `ruochu` | ✓ | ✓ | - |
| `sfacg` | ✓ | ✓ | - |
| `wenku8` | ✓ | - | - |
| `westnovel` | ✓ | ✓ | - |
| `yibige` | ✓ | - | - |
| `yodu` | ✓ | ✓ | - |

## 数据目录

```text
data/
├── settings.toml              # 配置文件
├── raw_data/<site>/<book>/    # 原始数据
├── downloads/                 # 导出文件
├── novel_cache/               # 缓存
├── logs/                      # 日志
└── go-novel-dl/state.json    # 状态
```

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

## 注意事项

- 站点能力基于代码实现，非实时健康检查
- 部分站点可能受 Cloudflare、限流、登录态或反爬策略影响
- `esjzone` 和 `novalpie` 需要登录才能使用
