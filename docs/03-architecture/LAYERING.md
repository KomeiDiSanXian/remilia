# 分层与依赖约定

> **最后更新**: 2026-09-13

本文件定义 Remilia 各顶层包的层次归属与允许的依赖方向，用于回答两个问题：

1. **新增包应该放在哪里？**
2. **一次改动是否破坏了分层？**

---

## 为什么需要这份约定

顶层包名（`core/`、`platform/`、`middleware/`…）是**公开导入路径**，同时也是分层
边界。目录可以移动，但导入路径一旦发布就不能随意改（会破坏所有使用方），因此
「包放在哪一层」这个决策是长期负担。

过去出现的具体问题都源于缺少成文约定：同一层的包互相引用形成环、底层包反向依赖
上层、以及跨层适配器落在错误的一侧。这些改动在编译期不会报错（Go 只禁止**包**
级循环，不禁止**分组**级循环），只能靠评审发现。

---

## 层次划分

从下到上，**只允许依赖同层或更低层**：

| 层 | 分组 / 包 | 职责 |
|---|---|---|
| **L0** | `errutil`、`stats`、`infra/*` 叶子包（`atomic`、`bytesconv`、`logger`、`fs`、`cache`…） | 零依赖基础设施与纯工具 |
| **L1** | `command`、`config`、`lifecycle`、`infra/*` 其余包（`dlq`、`health`、`pprof`、`buildinfo`…） | 解析、配置、生命周期、基础设施服务 |
| **L2** | `core/context`、`core/engine`、`core/fsm`、`core/permission` | 事件引擎与运行时核心 |
| **L3** | `router`、`platform/*`、`helper` | 命令路由、平台适配、公开工具 |
| **L4** | `plugin/*`、`middleware/*` | 插件系统与中间件 |
| **L5** | `remilia`（根包） | 面向用户的 Bot 门面与组装 API |
| **L6** | `builtin/*` | 随框架分发的内置插件 |
| **L7** | `api`、`testbot` | 管理 API、测试替身 |
| **L8** | `cmd/*`、`examples/*` | 可执行程序与示例 |

### 分层要点

- `infra/` **不得**引用 `core/` 或 `platform/`。`infra` 是最底层，任何反向依赖
  都意味着某个「基础设施」其实知道上层业务，应把它移上去而不是让 `infra` 依赖下层之外的东西。
- `middleware/` **不得**引用根包 `remilia`。需要根包里的类型时，应把该类型下沉到
  `infra` 或 `core`（例如 pprof 服务器下沉为 `infra/pprof`，根包保留类型别名）。
- `core/` **不得**引用 `middleware/`、`plugin/`、`builtin/`、`api`。
- 跨层**适配器**放在上层一侧：适配器的职责是「用上层的上下文调用下层的服务」，
  放在下层会让下层反向依赖上层。
- `infra/` 内部不要出现 `platform.Event`。需要平台类型时，把该功能放到
  `platform/` 下的子包。

---

## 当前依赖图

由 `go list` 实测得出（分组级，箭头表示「依赖」）：

```
L8  cmd/*, examples/*  ──▶ 几乎全部
L7  api, testbot       ──▶ builtin, root, core, infra, platform, plugin, config
L6  builtin/*          ──▶ root, core, infra, platform, plugin, command, middleware, config
L5  remilia (根包)      ──▶ core, infra, platform, plugin, router, lifecycle, errutil
L4  plugin/*           ──▶ core, infra, platform, command, config, lifecycle, errutil
    middleware/*       ──▶ core, infra, platform, command, config, errutil
L3  router             ──▶ core, infra
    platform/*         ──▶ infra, config, errutil
    helper             ──▶ core, infra, platform
L2  core/*             ──▶ infra, command, platform, errutil
L1  command            ──▶ infra
    config             ──▶ infra, errutil    （+ 一处到 core/engine 的例外，见下）
    lifecycle          ──▶ infra
    infra/*            ──▶ infra/*（同组内）
L0  errutil, stats     ──▶ 仅标准库
```

**当前分组级循环：无。**

---

## 既有偏差（已知且有意保留）

以下依赖违反上面的层次表，但都是**有意识的取舍**，不宜当作待修问题：

### 1. `config → core/engine`

`config/bridge.go` 提供 `EngineOptions(cfg EngineConfig) []engine.Option`。

这是刻意设计的桥接：与其让 `core/engine` 依赖 `config`（engine 需要读取配置类型），
不如把转换逻辑放在 `config` 一侧，使 **engine 包完全不依赖 config 包**。代价是
`config` 在依赖方向上位于 `core` 之上。

改动它需要同时调整 `cmd/bot`、`examples/config-integration` 以及外部使用方，
收益仅是让层次表更好看一行，因此保留。

### 2. `api → builtin/*`

`api/auditlog.go` 与 `api/scheduler.go` 为管理端点直接引用
`builtin/auditlog.Plugin`、`builtin/scheduler.Plugin` 的具体类型。原因是响应体需要
序列化插件自己的数据类型（`auditlog.LogEntry`、`scheduler.JobID`），而接口无法
在签名中表达这些返回类型。

彻底解耦需要引入「插件自带 HTTP 端点」的注册机制（例如让插件实现
`MountEndpoints(mux)` 并由 `api` 统一挂载）。这是一次独立的功能设计，不属于分层
清理，因此暂以文档记录。

### 3. `helper → core, infra, platform`

`helper` 曾是「字节转换 + 字符串哈希 + 平台事件取值 + handler 组合器」的混合包。
零拷贝转换与哈希已抽为 `infra/bytesconv`（叶子包），消除了原先
`platform ↔ helper` 的环；其余函数保留为 `Deprecated` 转发以兼容既有调用方，
因此 `helper` 仍依赖 `platform`（`ExtractContent` 等）与 `core/context`
（`Chain` / `ToMiddleware` 等）。

`helper` 目前**已无仓库内使用者**，仅有公开 API 与测试。后续可在主版本中把
`Extract*` 并入 `platform`、把组合器并入 `core/context`，彻底清空本包。

### 4. `infra/health` 测试引用 `core/engine`

仅存在于 `infra/health/checkers_test.go`：用真实 `engine.Engine` 验证它满足本包
定义的 `EngineStats` 接口。生产代码中 `infra/health` 只依赖该本地接口，**无**
内部依赖。这是集成测试的合理用法，因为它是测试文件，不影响发布产物的依赖图。

---

## 新增包应该放哪里

按用途判断：

| 这个包做什么 | 放哪里 |
|---|---|
| 纯算法 / 数据结构，不涉及平台与事件 | `infra/<name>`（或 `errutil` / `stats`） |
| 通用的字节、哈希、编码原语 | `infra/<name>`，保持零仓库内依赖 |
| 读取配置、解析命令 | `command` / `config` |
| 事件引擎、上下文、状态机 | `core/<name>` |
| 某种平台协议的适配 | `platform/<name>` |
| 用事件上下文调用下层服务 | `middleware/<name>` |
| 随框架分发、面向机器人用户的功能 | `builtin/<name>` |
| 宿主程序的组装逻辑 | 根包或 `cmd/*` |

判断依赖方向时问一句：**这个包需要 `context.Context` 或 `platform.Event` 吗？**
如果需要，它就不属于 `infra`。

---

## 如何自查

分组级循环不会被编译器发现。**包级**循环会编译失败，但「A 分组的包引用 B 分组、
B 分组的另一个包引用 A 分组」不会——而这正是本文件要防的问题。CI 中的
`go vet ./...` 只能捕获前者。

### 分组级环检测（PowerShell）

在仓库根执行：

```powershell
$edges=@{}; $map=@{}
foreach($p in (go list ./...)){
  $map[$p]=@(((go list -f '{{range .Imports}}{{.}},{{end}}' $p) -join '') -split ',' |
    Where-Object { $_ -like 'github.com/KomeiDiSanXian/remilia*' })
}
function G($x){
  $r=$x.Replace('github.com/KomeiDiSanXian/remilia','')
  if($r -eq ''){'(root)'}else{($r.TrimStart('/') -split '/')[0]}
}
foreach($k in $map.Keys){
  $a=G $k
  foreach($i in $map[$k]){ $b=G $i; if($a -ne $b){ $edges["$a -> $b"]=1 } }
}
$cyc=@()
foreach($e in $edges.Keys){
  $p=$e -split ' -> '
  if($edges.ContainsKey("$($p[1]) -> $($p[0])")){ $cyc += (($p|Sort-Object) -join ' <-> ') }
}
if($cyc){ 'CYCLE: ' + (($cyc|Sort-Object -Unique) -join ', ') } else { 'no group-level cycles' }
```

期望输出：`no group-level cycles`。

### 两条硬性规则

```bash
# 规则 1：infra 不应引用 core 或 platform（测试文件除外）
grep -rn '"github.com/KomeiDiSanXian/remilia/\(core\|platform\)' infra/ --include=*.go

# 规则 2：middleware 不应引用根包（测试文件除外）
grep -rn '"github.com/KomeiDiSanXian/remilia"' middleware/ --include=*.go
```

两条命令都应只返回**测试文件**中的匹配，或返回空。
