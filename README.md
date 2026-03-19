# go-novel-dl

`go-novel-dl` 是一个参考 `novel-downloader` 架构和命令面实现的 Go 版小说下载器，目前以 CLI 为主，后续再继续补 Web。

整体流程保持为：

CLI -> 配置 -> 站点适配器 -> 下载 -> 处理流水线 -> 导出 -> 本地存储

当前项目重点放在：

- 命令行体验
- 站点适配器扩展能力
- 原始数据分阶段存储
- 导出能力
- 后续 Docker 挂载友好的 `data/` 目录结构

## 当前状态

- 已完成 Cobra 命令树
- 已完成 `data/settings.toml` 配置工作流
- 已完成 `download` / `search` / `export` / `config` / `clean` 命令
- 已完成分阶段原始数据存储
- 已完成 TXT / HTML / EPUB 导出
- 已完成章节级下载进度显示
- 已完成部分站点的真实下载验证
- Web 界面暂未开始

说明：当前最成熟的实现是 `esjzone`，其他站点按可用性逐步补齐。

## 支持站点

下表表示当前 `go-novel-dl` 的实际支持状态，形式尽量参考 `novel-downloader`：

- `是` = 已支持或已验证
- `否` = 当前不支持
- `部分` = 已有实现，但仍有限制或不稳定
- `站内` = 站点本身具备能力，但当前 CLI 侧没有完全打通

| 站点名称 | 标识符 | 下载状态 | 分卷 | 图片 | 登录 | 搜索 | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| ESJ Zone | `esjzone` | 可用 | 是 | 是 | 是 | 是 | 已验证登录、镜像、断点续下、简体输出 |
| WestNovel | `westnovel` | 可用 | 否 | 否 | 否 | 否 | 已验证基础下载 |
| 有度中文网 | `yodu` | 可用 | 是 | 站内 | 部分 | 否 | 已验证真实下载 |
| 铅笔小说 | `n23qb` | 可用 | 是 | 否 | 部分 | 否 | 已验证真实下载 |
| 笔趣阁 | `biquge345` | 可用 | 否 | 否 | 部分 | 否 | 已验证真实下载 |
| 飘天文学网 | `piaotia` | 可用 | 否 | 否 | 部分 | 否 | 已验证真实下载，标题与作者已修正 |
| 爱下电子书 | `ixdzs8` | 可用 | 否 | 否 | 部分 | 否 | 已验证 challenge 流程和章节下载 |
| 笔趣阁 | `biquge5` | 部分可用 | 否 | 否 | 部分 | 否 | 已验证单章下载，长文本下载仍可能偏慢 |
| 哔哩轻小说 | `linovelib` | 部分可用 | 是 | 是 | 部分 | 站内 | 已接入去混淆和重试，长书下载还需优化 |
| 一笔阁 | `yibige` | 部分可用 | 否 | 否 | 部分 | 站内 | 已有适配器，但当前 Cloudflare 会影响稳定下载 |
| 69书吧 | `n69shuba` | 部分可用 | 否 | 否 | 部分 | 站内 | 已有适配器，但当前站点有 Cloudflare 挡板 |
| 笔趣阁 | `fsshu` | 部分可用 | 否 | 否 | 部分 | 否 | 已校准现网页径，但完整 smoke 下载仍超时 |

## 命令总览

命令名称尽量保持与 `novel-downloader` 一致：

```bash
novel-cli download [book_ids | url]
novel-cli search keyword
novel-cli export [book_id ...]
novel-cli config init
novel-cli config set-lang zh_CN
novel-cli clean state
novel-cli clean logs
novel-cli clean cache
novel-cli clean book
```

## 最简单的用法

```bash
# 初始化配置
go run ./cmd/novel-cli config init

# 下载一本书
go run ./cmd/novel-cli download --site esjzone 1660702902

# 导出已有下载
go run ./cmd/novel-cli export --site esjzone 1660702902 --format epub

# 运行测试
go test ./...
```

## 常用示例

```bash
# 通过 URL 下载
go run ./cmd/novel-cli download https://www.esjzone.cc/detail/1660702902.html

# 通过站点和书号下载
go run ./cmd/novel-cli download --site esjzone 1660702902

# 只下载一个章节区间
go run ./cmd/novel-cli download --site esjzone 1660702902 --start 294593 --end 305803

# 下载 WestNovel
go run ./cmd/novel-cli download --site westnovel wuxia-ynyh

# 下载一笔阁
go run ./cmd/novel-cli download --site yibige 6238

# 下载有度
go run ./cmd/novel-cli download --site yodu 1

# 下载哔哩轻小说
go run ./cmd/novel-cli download --site linovelib 8

# 下载铅笔小说
go run ./cmd/novel-cli download --site n23qb 12282

# 下载笔趣阁345
go run ./cmd/novel-cli download --site biquge345 151120

# 下载笔趣阁5
go run ./cmd/novel-cli download --site biquge5 9_9194

# 下载 fsshu
go run ./cmd/novel-cli download --site fsshu 100_100256

# 下载 69书吧
go run ./cmd/novel-cli download --site n69shuba 54065

# 下载飘天文学网
go run ./cmd/novel-cli download --site piaotia 1-1705

# 下载爱下电子书
go run ./cmd/novel-cli download --site ixdzs8 15918

# 搜索后交互式选择下载
go run ./cmd/novel-cli search 三体

# 导出已下载书籍
go run ./cmd/novel-cli export --site esjzone 1660702902 --format epub

# 查看将清理哪些日志
go run ./cmd/novel-cli clean logs --dry-run
```

## 项目结构

```text
cmd/novel-cli           CLI 入口
internal/cli            Cobra 命令与交互逻辑
internal/app            下载/搜索/导出/清理编排层
internal/config         配置默认值、加载与合并
internal/site           站点注册、URL 解析、站点适配器
internal/pipeline       文本处理流水线
internal/exporter       TXT/HTML/EPUB 导出
internal/store          本地原始数据与流水线状态存储
internal/state          CLI 状态，如语言设置
internal/ui             控制台输出与交互
internal/model          通用领域模型
internal/progress       下载进度展示
tests/                  测试说明入口
```

## 配置说明

项目默认使用 `data/settings.toml`。

可以通过下面命令生成：

```bash
go run ./cmd/novel-cli config init
```

内置模板文件位于：

- `internal/config/resources/settings.sample.toml`

当前主要配置段包括：

- `[general]`
- `[general.output]`
- `[general.parser]`
- `[general.debug]`
- `[[general.processors]]`
- `[sites.<site>]`
- `[plugins]`

## 数据目录结构

所有运行数据统一写入 `data/`，方便后续 Docker 挂载。

原始数据按阶段保存到：

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

这套结构保持了参考项目“先抓原始数据，再处理，再导出”的思路。

## 测试

最常用的测试命令：

```bash
go test ./...
go build ./...
```

关于测试组织方式的说明见：

- `tests/README.md`

## 后续计划

1. 继续提高 `linovelib`、`fsshu`、`yibige`、`n69shuba` 的真实可用性
2. 增强图片下载与 EPUB 资源打包
3. 增加更细粒度的断点续下和刷新策略
4. 继续扩展更多站点适配器
5. 在稳定 CLI 的基础上补 Web API 与 Web UI
