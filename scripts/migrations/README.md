# 数据迁移脚本

离线执行 Remilia 数据层的**兼容迁移**。迁移逻辑与运行时自动迁移等价——本目录只是把
它从 Bot 启动流程里单独拿出来，方便：

- 升级前先在**副本**上验证迁移结果；
- 停止 Bot 后批量处理数据文件（例如容器里挂载的旧库）；
- 排查"库是什么版本、缺哪些表"；
- 作为未来把自动迁移分支移出运行时的参考实现（见文末计划）。

## 前提

1. **先停 Bot。** 脚本直接打开 SQLite 文件，与运行中的 Bot 并发写会互相阻塞。
2. 只支持 **SQLite 文件**（`-db` 传文件路径）。PostgreSQL / MySQL 请参考文末手工 SQL。
3. `-apply` 时默认先用 `VACUUM INTO` 生成一致性备份（`.bak-<时间戳>`），
   比直接拷文件安全（WAL 会被合并）。`-backup=false` 可关闭，但**不建议**。

## 用法

```bash
# 1) 只报告：版本、表状态、将要执行什么（不写入）
go run ./scripts/migrations -db data/db/bot.db

# 2) 执行全部迁移（自动备份）
go run ./scripts/migrations -db data/db/bot.db -apply

# 3) 只跑其中一项
go run ./scripts/migrations -db data/db/bot.db -apply ai-session
go run ./scripts/migrations -db data/db/bot.db -apply messagelog

# 4) 显式关掉备份（不建议）
go run ./scripts/migrations -db data/db/bot.db -apply -backup=false
```

子命令（位置参数）：

| 子命令 | 作用 |
|---|---|
| `report`（默认 `all` 也会先打印计划） | 只读检查 |
| `messagelog` | 把 messagelog 库从 `user_version=0/1` 升到 `2` |
| `ai-session` | 历史表 `records` → `ai_session_records` 并清理旧索引 |
| `all` | 以上全部（默认） |

所有操作都是**幂等**的：重复执行只会输出 `跳过` / `nothing to migrate`。

## 迁移项

### messagelog（`message_records`，`user_version` 0/1 → 2）

针对 v1.46 遗留结构：补列（`platform_message_id` / `reply_to_event_id` /
`send_status` / `is_edited` 等）、把 `event_id` 从"平台消息 ID"升级为自生成
UUIDv7（原值回填到 `platform_message_id`，并同步改写提及/附件引用与回复链）、
建附件表与幂等唯一索引、清理早期误建的 `attachment_records`。

实现直接调用 `builtin/messagelog.OpenDB`——脚本与运行时共用同一份迁移代码，
避免两边 SQL 漂移。

### ai-session（`records` → `ai_session_records`）

v1.77.0 的 `Record` 没有显式表名，GORM 把表建成了通用的 `records`。脚本执行：

```sql
ALTER TABLE records RENAME TO ai_session_records;
DROP INDEX IF EXISTS idx_records_user_id;
DROP INDEX IF EXISTS idx_records_chat_id;
```

改名不复制数据；目标表已存在时跳过（避免覆盖新表）。

## 与运行时自动迁移的关系

**当前版本启动时仍会自动迁移**，旧库可以直接启动、无需先跑本脚本——这是刻意的：
自动迁移保证"下载新版就能用"。本脚本是可选的离线等价路径。

后续计划（下个大版本）：

1. `builtin/messagelog` 检测到 `user_version < 2` 时不再自动改数据，
   改为报错并提示运行本脚本（或显式设置 `messagelog.auto_migrate: true` 的过渡开关）；
2. 届时删除运行时里的遗留迁移分支，只保留版本检查与新表建表。

## 手工 SQL（PostgreSQL / MySQL）

脚本只处理 SQLite；其它驱动需要手工执行。核心语句与 SQLite 版本相同：

```sql
-- ai-session
ALTER TABLE records RENAME TO ai_session_records;
DROP INDEX IF EXISTS idx_records_user_id;
DROP INDEX IF EXISTS idx_records_chat_id;

-- messagelog：建议先在副本上执行，并参考 builtin/messagelog/migrate.go
-- 的完整步骤（列回填、event_id 重写、索引创建）。MySQL 8 的 DROP INDEX
-- 不支持 IF EXISTS，需先查 information_schema.statistics 确认存在性。
```

## 安全边界

- 脚本只做结构迁移与既有数据重写，**不删除业务数据**；唯一的 DROP 是
  早期误建的 `attachment_records` 与不再使用的旧索引。
- 任何一步失败都会带上语句与原因退出；失败后可直接重跑（事务/幂等语句）。
- 备份文件不会自动清理，确认无误后自行删除。
