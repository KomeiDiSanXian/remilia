# API 服务器

框架内置管理 API 服务器（`api/` 包），提供 Bot / 插件 / 平台 / 引擎 / 权限 / 日志等运维管理端点，统一 JSON 响应格式。

## 启用与配置

```yaml
api:
  enabled: true
  addr: ":8081"
  api_key: "your-secret-key"
```

- `enabled`：是否启动 API 服务器
- `addr`：监听地址
- `api_key`：**必填**——未配置时拒绝所有远程访问（仅允许本地环回）

### 认证

管理端点使用 **Bearer Token** 认证：

```bash
curl -H "Authorization: Bearer your-secret-key" http://localhost:8081/api/v1/plugins
```

`/api/v1/health` 与 `/api/v1/version` 为公开端点，无需认证。

## 端点总览

### 公开端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/health` | 健康检查（Bot/Adapter/DLQ 多层级健康树） |
| GET | `/api/v1/version` | 框架版本 / Git commit / 构建时间 / Go 版本 |

### Bot 管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/bots` | Bot 摘要列表 |
| GET | `/api/v1/bots/{name}` | Bot 详情 |
| POST | `/api/v1/bots/{name}/start` | 启动 |
| POST | `/api/v1/bots/{name}/stop` | 停止 |
| POST | `/api/v1/bots/{name}/restart` | 重启 |

### 插件管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/plugins` | 插件列表（含状态/版本/Matcher 数） |
| GET | `/api/v1/plugins/{name}` | 插件详情 |
| POST | `/api/v1/plugins/{name}/enable` | 启用 |
| POST | `/api/v1/plugins/{name}/disable` | 禁用 |
| POST | `/api/v1/plugins/{name}/reload` | 热重载 |

### 平台管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/platforms` | 已注册平台列表 |
| GET | `/api/v1/platforms/{name}` | 平台详情 |
| POST | `/api/v1/platforms` | 热添加平台 |
| DELETE | `/api/v1/platforms/{name}` | 移除平台 |

### 引擎

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/engine/commands` | 全部命令定义 |
| GET | `/api/v1/engine/matchers` | Matcher 统计 |
| POST | `/api/v1/engine/matchers/group/{name}/disable` | 禁用分组 |
| POST | `/api/v1/engine/matchers/group/{name}/enable` | 启用分组 |

### 审计日志

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/auditlog` | 审计日志（分页） |
| GET | `/api/v1/auditlog/user/{id}` | 按用户查询 |
| GET | `/api/v1/auditlog/action/{action}` | 按操作查询 |
| GET | `/api/v1/auditlog/count` | 计数统计 |

### 权限（RBAC）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET/POST | `/api/v1/permission/roles` | 角色列表 / 创建 |
| DELETE | `/api/v1/permission/roles/{role}` | 删除角色 |
| POST/DELETE | `/api/v1/permission/roles/{role}/permissions` | 增删角色权限 |
| POST/DELETE | `/api/v1/permission/users/{userID}/roles` | 分配 / 撤销用户角色 |
| GET | `/api/v1/permission/users/{userID}/permissions` | 用户权限 |
| POST | `/api/v1/permission/check` | 权限检查 |

### FSM 状态机

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/fsm` | 已注册 FSM 列表 |
| GET | `/api/v1/fsm/{name}` | FSM 定义 |
| GET | `/api/v1/fsm/sessions` | 活跃会话列表 |
| DELETE | `/api/v1/fsm/sessions/{id}` | 强制结束会话 |

### 调度任务（scheduler 插件）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/scheduler/jobs` | 任务列表 |
| DELETE | `/api/v1/scheduler/jobs/{id}` | 删除任务 |
| GET | `/api/v1/scheduler/history` | 执行历史 |

### 日志

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/logs` | 日志查询 |
| GET | `/api/v1/logs/stream` | 日志实时流（SSE） |

### 配置与统计

| 方法 | 路径 | 说明 |
|------|------|------|
| GET/PUT | `/api/v1/config` | 读取 / 写入配置 |
| POST | `/api/v1/config/reload` | 触发配置重载 |
| GET | `/api/v1/stats` | 插件管理器运行时统计快照 |

## 使用示例

```bash
# 查看版本
curl http://localhost:8081/api/v1/version

# 列出插件
curl -H "Authorization: Bearer key" http://localhost:8081/api/v1/plugins

# 热重载插件
curl -X POST -H "Authorization: Bearer key" http://localhost:8081/api/v1/plugins/weather/reload

# 查询日志
curl -H "Authorization: Bearer key" "http://localhost:8081/api/v1/logs?limit=50"
```

> 所有端点返回统一 JSON 结构（成功：`{"code":0,"data":...}`；错误：`{"code":<非零>,"message":"..."}`）。
