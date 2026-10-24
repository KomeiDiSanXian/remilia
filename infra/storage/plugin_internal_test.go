// plugin_internal_test.go — 打开数据库时的连接级设置（内部契约）。
//
// synchronous / busy_timeout 是连接级 PRAGMA：用 db.Exec 执行只会改到池里的
// 某一条连接，这里把"每条连接都要生效"钉成可执行契约，避免退回按连接随机的
// 持久化策略。
package storage

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithSQLiteConnectionPragmas(t *testing.T) {
	cases := []struct{ in, want string }{
		{"bot.db", "bot.db?_pragma=synchronous(NORMAL)"},
		{"data/bot.db?cache=shared", "data/bot.db?cache=shared&_pragma=synchronous(NORMAL)"},
		{"file::memory:?cache=shared", "file::memory:?cache=shared&_pragma=synchronous(NORMAL)"},
		// 调用方显式配置过 _pragma 时不介入：它可能有意选择更强的持久化策略。
		{"bot.db?_pragma=synchronous(FULL)", "bot.db?_pragma=synchronous(FULL)"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, withSQLiteConnectionPragmas(c.in), "dsn %q", c.in)
	}
}

func TestOpenAppliesConnectionPragmas(t *testing.T) {
	client, err := Open(WithDSN(t.TempDir() + "/pragma.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := client.DB().DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	var synchronous int
	require.NoError(t, client.DB().Raw("PRAGMA synchronous").Scan(&synchronous).Error)
	assert.Equal(t, 1, synchronous, "synchronous should be NORMAL (1) on every pooled connection")

	var journalMode string
	require.NoError(t, client.DB().Raw("PRAGMA journal_mode").Scan(&journalMode).Error)
	assert.Equal(t, "wal", strings.ToLower(journalMode))

	sqlDB, err := client.DB().DB()
	require.NoError(t, err)
	assert.Equal(t, 5, sqlDB.Stats().MaxOpenConnections, "pool size should be capped for SQLite")
}
