package promptctx

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/retrieval"
	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
)

// TestRAGHitTieBreakNewerFirst 冻结历史检索的同分次序：分数相同则较新的消息
// 优先，同一时刻再按事件 ID、内容升序，结果不随底层加载顺序抖动。
func TestRAGHitTieBreakNewerFirst(t *testing.T) {
	older := ragHit{Entry: messagelog.RecordEntry{Timestamp: time.Unix(1000, 0), EventID: "old", Content: "旧"}}
	newer := ragHit{Entry: messagelog.RecordEntry{Timestamp: time.Unix(2000, 0), EventID: "new", Content: "新"}}
	sameMomentB := ragHit{Entry: messagelog.RecordEntry{Timestamp: time.Unix(3000, 0), EventID: "b", Content: "乙"}}
	sameMomentA := ragHit{Entry: messagelog.RecordEntry{Timestamp: time.Unix(3000, 0), EventID: "a", Content: "甲"}}

	items := []ragHit{older, sameMomentB, newer, sameMomentA}
	retrieval.RankByScore(items, func(h ragHit) float64 { return 0 }, ragHitTieBreak)

	got := []string{items[0].Entry.EventID, items[1].Entry.EventID, items[2].Entry.EventID, items[3].Entry.EventID}
	assert.Equal(t, []string{"a", "b", "new", "old"}, got,
		"较新的消息优先，同一时刻按事件 ID 升序")
}
