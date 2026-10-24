// Command migrations 对 Remilia 的数据文件执行**离线**兼容迁移。
//
// 迁移逻辑与运行时自动迁移等价：本命令只是把它从 Bot 启动流程里单独拿出来，
// 让运维可以先在副本上验证、或在停止 Bot 后批量处理数据文件。
//
// 用法：
//
//	go run ./scripts/migrations -db data/db/bot.db          # 只报告，不写入
//	go run ./scripts/migrations -db data/db/bot.db -apply   # 执行迁移（自动备份）
//	go run ./scripts/migrations -db data/db/bot.db -apply ai-session
//
// 仅支持 SQLite：其它驱动（PostgreSQL/MySQL）请参考 README 手工执行等价 SQL。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// 迁移名：可单跑，也可 all。
const (
	jobReport     = "report"
	jobMessagelog = "messagelog"
	jobAISession  = "ai-session"
	jobAll        = "all"
)

func main() {
	dbPath := flag.String("db", "", "SQLite 数据库文件路径（必填），如 data/db/bot.db")
	apply := flag.Bool("apply", false, "实际执行迁移；缺省只报告（dry-run）")
	backup := flag.Bool("backup", true, "-apply 时先用 VACUUM INTO 生成备份副本")
	flag.Parse()

	if *dbPath == "" {
		fail("missing -db <path>")
	}
	if !looksLikeSQLitePath(*dbPath) {
		fail("only SQLite file paths are supported, got %q (see scripts/migrations/README.md)", *dbPath)
	}
	if _, err := os.Stat(*dbPath); err != nil {
		fail("cannot access %s: %v", *dbPath, err)
	}

	job := jobAll
	if args := flag.Args(); len(args) > 0 {
		job = args[0]
	}
	switch job {
	case jobReport, jobMessagelog, jobAISession, jobAll:
	default:
		fail("unknown job %q (want report | messagelog | ai-session | all)", job)
	}

	db, err := openDB(*dbPath)
	if err != nil {
		fail("open %s: %v", *dbPath, err)
	}
	defer closeDB(db)

	fmt.Printf("database: %s\n", *dbPath)
	version, err := schemaVersion(db)
	if err != nil {
		fail("read schema version: %v", err)
	}
	fmt.Printf("schema:   user_version=%d\n", version)
	printTables(db)

	if job == jobReport {
		printPlan(db)
		return
	}
	printPlan(db)

	if !*apply {
		fmt.Println("\ndry-run: 未写入任何变更。确认后再加 -apply 执行。")
		return
	}

	if !hasWork(db, job) {
		fmt.Println("\nnothing to migrate.")
		return
	}

	if *backup {
		backupPath, err := backupDB(db, *dbPath)
		if err != nil {
			fail("backup: %v", err)
		}
		fmt.Printf("\nbackup:   %s\n", backupPath)
	}

	if job == jobMessagelog || job == jobAll {
		if err := runMessagelog(*dbPath); err != nil {
			fail("messagelog migration: %v", err)
		}
	}
	if job == jobAISession || job == jobAll {
		if err := runAISession(db, true); err != nil {
			fail("ai-session migration: %v", err)
		}
	}

	// 重新读取一遍，输出最终状态。
	final, err := schemaVersion(db)
	if err != nil {
		fail("re-read schema version: %v", err)
	}
	fmt.Printf("\ndone: user_version=%d\n", final)
	printTables(db)
}

// looksLikeSQLitePath 排除 DSN 形式（postgres://、user:pass@tcp(...) 等）。
func looksLikeSQLitePath(p string) bool {
	return !strings.Contains(p, "://")
}

func openDB(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, err
	}
	// 与运行时一致：迁移前放开写锁等待，避免与残留连接争抢时直接 SQLITE_BUSY。
	db.Exec("PRAGMA busy_timeout = 5000")
	return db, nil
}

func closeDB(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}

// backupDB 用 VACUUM INTO 生成一致性的数据库副本（比直接拷文件安全：WAL 会被合并）。
func backupDB(db *gorm.DB, src string) (string, error) {
	dst := fmt.Sprintf("%s.bak-%s", src, time.Now().Format("20060102-150405"))
	if err := db.Exec("VACUUM INTO ?", dst).Error; err != nil {
		return "", fmt.Errorf("VACUUM INTO %s: %w", dst, err)
	}
	if abs, err := filepath.Abs(dst); err == nil {
		return abs, nil
	}
	return dst, nil
}

// schemaVersion 读取 SQLite 的 PRAGMA user_version。
func schemaVersion(db *gorm.DB) (int, error) {
	var v int
	if err := db.Raw("PRAGMA user_version").Row().Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

func tableExists(db *gorm.DB, name string) bool {
	return db.Migrator().HasTable(name)
}

func printTables(db *gorm.DB) {
	for _, t := range []string{"message_records", "message_mentions", "records", "ai_session_records"} {
		state := "absent"
		if tableExists(db, t) {
			state = "present"
		}
		fmt.Printf("table:    %-20s %s\n", t, state)
	}
}

// printPlan 打印将要执行的迁移（不写入）。
func printPlan(db *gorm.DB) {
	fmt.Println("\nplan:")
	for _, line := range planMessagelog(db) {
		fmt.Printf("  [messagelog] %s\n", line)
	}
	for _, line := range planAISession(db) {
		fmt.Printf("  [ai-session] %s\n", line)
	}
}

// hasWork 报告指定 job 是否有实际变更；没有就不备份、不打开第二连接。
func hasWork(db *gorm.DB, job string) bool {
	if job == jobMessagelog || job == jobAll {
		if tableExists(db, "message_records") {
			if v, err := schemaVersion(db); err == nil && v < 2 {
				return true
			}
		}
	}
	if job == jobAISession || job == jobAll {
		if !tableExists(db, "ai_session_records") && tableExists(db, "records") {
			return true
		}
	}
	return false
}

func planMessagelog(db *gorm.DB) []string {
	if !tableExists(db, "message_records") {
		return []string{"跳过：没有 message_records（未启用 messagelog）"}
	}
	v, err := schemaVersion(db)
	if err != nil {
		return []string{fmt.Sprintf("读取 user_version 失败：%v", err)}
	}
	if v >= 2 {
		return []string{fmt.Sprintf("跳过：user_version=%d 已是当前版本", v)}
	}
	return []string{fmt.Sprintf("执行用户版本迁移 %d -> 2（加列 / event_id 升级为 UUIDv7 / 附件表 / 索引）", v)}
}

func planAISession(db *gorm.DB) []string {
	switch {
	case tableExists(db, "ai_session_records"):
		return []string{"跳过：ai_session_records 已存在"}
	case !tableExists(db, "records"):
		return []string{"跳过：没有遗留的 records 表"}
	default:
		return []string{
			`ALTER TABLE records RENAME TO ai_session_records`,
			`DROP INDEX IF EXISTS idx_records_user_id / idx_records_chat_id（旧索引不再需要）`,
		}
	}
}

// runMessagelog 复用 messagelog.OpenDB 的迁移流程（AutoMigrate + 版本迁移）。
//
// 不复制迁移 SQL：运行时与脚本必须走同一份实现，否则两边会漂移。
func runMessagelog(path string) error {
	db, err := messagelog.OpenDB(path)
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
	fmt.Println("\n[messagelog] migrated via builtin/messagelog.OpenDB")
	return nil
}

// runAISession 把历史表 records 改名为 ai_session_records（与
// builtin/ai/session 的运行时迁移等价）。
//
// 顺序与运行时一致：目标表已存在则跳过（避免覆盖），改名成功后清理旧索引。
func runAISession(db *gorm.DB, apply bool) error {
	if tableExists(db, "ai_session_records") || !tableExists(db, "records") {
		fmt.Println("[ai-session] nothing to do")
		return nil
	}
	stmts := []string{`ALTER TABLE records RENAME TO ai_session_records`}
	// 旧索引名固定为 idx_records_<column>（GORM 默认命名策略），改名不会带走索引名。
	for _, col := range []string{"user_id", "chat_id"} {
		stmts = append(stmts, fmt.Sprintf(`DROP INDEX IF EXISTS idx_records_%s`, col))
	}
	for _, s := range stmts {
		if !apply {
			fmt.Printf("[ai-session] would run: %s\n", s)
			continue
		}
		if err := db.Exec(s).Error; err != nil {
			return fmt.Errorf("%s: %w", s, err)
		}
		fmt.Printf("[ai-session] %s\n", s)
	}
	return nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "migrations: "+format+"\n", args...)
	os.Exit(1)
}
