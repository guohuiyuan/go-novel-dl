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
- 已补充一批中文站点适配器
- 已补充默认范围完整下载的手工健康检查
- 已按 2026-03-23 的实测结果更新下载状态
- Web 界面暂未开始

说明：下面的“下载状态”专门指“不指定开始结束，按站点默认范围完整下载”是否通过本地手工健康检查；`faloo` 当前按公开章节范围判定，`wenku8` / `hongxiuzhao` / `n69shuba` 等站点仍受 Cloudflare 或站点风控影响。

## 支持站点

下表表示当前 `go-novel-dl` 的默认范围完整下载状态：

- `可用` = 已在 2026-03-23 完整跑通默认范围下载
- `部分可用` = 已有实现，但当前整本默认范围下载仍会被风控、超时、错误页或字段漂移打断

| 站点名称   | 标识符          | 下载状态 | 实测说明 |
| ---------- | --------------- | -------- | -------- |
| ESJ Zone   | `esjzone`       | 可用     | 已完整下载 156 章；当前最稳定，需要已配置登录信息 |
| WestNovel  | `westnovel`     | 可用     | 已完整下载 48 章 |
| 一笔阁     | `yibige`        | 部分可用 | 默认范围下载时命中站点 `502` |
| 有度中文网 | `yodu`          | 可用     | 已完整下载 45 章 |
| 哔哩轻小说 | `linovelib`     | 可用     | 已完整下载 11 章 |
| 铅笔小说   | `n23qb`         | 可用     | 已完整下载 128 章 |
| 笔趣阁     | `biquge345`     | 部分可用 | 默认范围下载时中途章节返回 `502` |
| 笔趣阁     | `biquge5`       | 部分可用 | URL 解析已修正，但默认范围整本下载仍会超时 |
| 笔趣阁     | `fsshu`         | 部分可用 | URL 解析已修正，但默认范围整本下载仍会超时 |
| 69书吧     | `n69shuba`      | 部分可用 | 章节请求实测返回 `403` |
| 飘天文学网 | `piaotia`       | 部分可用 | 默认范围下载时中途命中 `429` |
| 爱下电子书 | `ixdzs8`        | 部分可用 | challenge 流程能走通，但默认范围整本下载在高页码阶段持续超时 |
| Novalpie   | `novalpie`      | 部分可用 | 已补 `novalpie.jp` / `viewer` URL 解析，当前详情字段仍有漂移，实测标题为空 |
| 若初文学网 | `ruochu`        | 可用     | 已完整下载 18 章 |
| 17K 小说网 | `n17k`          | 部分可用 | 默认范围下载时章节请求返回 `405` |
| 红袖招     | `hongxiuzhao`   | 部分可用 | 公开访问实测命中 Cloudflare challenge，样例章节 URL 也不稳定 |
| 番茄小说网 | `fanqienovel`   | 可用     | 已完整下载 10 章 |
| 飞卢小说网 | `faloo`         | 部分可用 | 已完整下载 62 章公开章节；VIP 章节仍依赖额外登录能力 |
| 轻小说文库 | `wenku8`        | 部分可用 | 已加节流和 `Referer`，但当前 `net/http` 仍会被 Cloudflare challenge 拦截 |
| SF 轻小说  | `sfacg`         | 可用     | 已完整下载 35 章 |
| 次元姬     | `ciyuanji`      | 可用     | 已修正 `Referer` 和站点级节流，已完整下载 33 章 |
| 轻小说机翻 | `qbtr`          | 部分可用 | 默认范围整本下载中途章节请求超时 |
| 刺猬猫     | `ciweimao`      | 可用     | 已完整下载 89 章 |

`novalpie` 推荐优先使用浏览器登录后的 Bearer Token，而不是让 CLI 直接登录。原因是该站点登录时有人机验证。

推荐做法：

1. 在浏览器中正常登录并完成验证
2. 从开发者工具里复制 `Authorization: Bearer ...` 的值
3. 写入 `data/settings.toml` 的 `sites.novalpie.cookie`
4. 之后直接用 CLI 下载

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

# 下载 Novalpie
go run ./cmd/novel-cli download --site novalpie 353245

# 下载 番茄小说网
go run ./cmd/novel-cli download --site fanqienovel 7276384138653862966

# 下载 飞卢小说网（无 Cookie 时建议使用公开章节范围）
go run ./cmd/novel-cli download --site faloo 1482723 --start 1 --end 52

# 下载 若初文学网
go run ./cmd/novel-cli download --site ruochu 121261

# 下载 17K 小说网
go run ./cmd/novel-cli download --site n17k 3374595

# 下载 红袖招
go run ./cmd/novel-cli download --site hongxiuzhao ZG6rmWO

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
