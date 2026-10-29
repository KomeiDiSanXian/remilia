package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// corruptPayload 故意写成非法 JSON（且不可能被解析成 []memoryFact）。
const corruptPayload = `{"text": 123, "updated_at": "not-a-time"`

// TestMemoryUnreadableScopeIsNotOverwritten 守卫"读不出来的记忆不被覆盖"。
//
// 触发路径很窄但后果很重：进程启动后第一次写某个 scope 时走 loadLocked，
// 若库里那份数据反序列化失败，内存视图会是空的；此时若照常 Add → 整记录
// upsert，就会把库里读不出来的旧数据抹成一条新事实。
//
// session 包用 loadFailed 墓碑防住了同一类问题（见 Session.MarkLoadFailed），
// 记忆这里选择直接拒绝写入：降级表现为"这次没记住"，而不是"旧记忆被清空"。
//
// LevelDB 同一目录不能开两个实例，因此整个用例只用**一个** store：
// 先写坏一条数据（模拟磁盘损坏），再触发写路径验证保护。
func TestMemoryUnreadableScopeIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	scope := userScope("u1")

	m, err := openMemoryStore(dir, 50, 0)
	require.NoError(t, err)
	t.Cleanup(m.Close)

	// 直接往 KV 写一份坏数据，模拟"磁盘上已有读不出来的旧记忆"。
	require.NoError(t, m.store.Set([]byte(scope), []byte(corruptPayload)))

	// 第一次写该 scope：loadLocked 读失败 → 标记不可写 → Add 直接返回。
	m.Add(scope, "用户讨厌香菜")

	assert.Empty(t, m.Facts(scope), "读不出来的 scope 不应凭空出现新事实")

	// 磁盘上必须仍是那份坏数据：没有被"空视图 + 一条新事实"覆盖。
	raw, err := m.store.Get([]byte(scope))
	require.NoError(t, err)
	assert.Equal(t, corruptPayload, string(raw), "坏数据不得被新事实覆盖")
}

// TestMemoryWritableScopeIsPersisted 对照组：正常 scope 的写入必须落盘且可读回。
func TestMemoryWritableScopeIsPersisted(t *testing.T) {
	dir := t.TempDir()
	scope := groupScope("g1")

	m, err := openMemoryStore(dir, 50, 0)
	require.NoError(t, err)
	defer m.Close()

	m.Add(scope, "本群约定每周五发周报")
	require.Len(t, m.Facts(scope), 1)
	assert.Equal(t, "本群约定每周五发周报", m.Facts(scope)[0].Text)

	// 值确实写进了 KV（不是只留在内存里）。
	raw, err := m.store.Get([]byte(scope))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "本群约定每周五发周报")
}

// TestMemoryUnreadableScopeStaysWritableForOthers 守卫"坏 scope 不影响其他 scope"。
func TestMemoryUnreadableScopeStaysWritableForOthers(t *testing.T) {
	dir := t.TempDir()
	broken := userScope("broken")
	healthy := userScope("healthy")

	m, err := openMemoryStore(dir, 50, 0)
	require.NoError(t, err)
	defer m.Close()

	require.NoError(t, m.store.Set([]byte(broken), []byte(corruptPayload)))

	m.Add(broken, "这条不该落库")
	m.Add(healthy, "这条应该正常保存")

	assert.Empty(t, m.Facts(broken))
	assert.Len(t, m.Facts(healthy), 1, "其他 scope 不受影响")
}
