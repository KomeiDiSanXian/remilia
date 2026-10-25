package context

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestMatcherCost_Resolve 固化「冷启动 local、观测慢转 io、样本不足不判慢」。
func TestMatcherCost_Resolve(t *testing.T) {
	const slow = 500 * time.Millisecond

	tests := []struct {
		name string
		cost MatcherCost
		want CostClass
	}{
		{"cold start", MatcherCost{P50: 10 * time.Second, Samples: 0}, CostLocal},
		{"below min samples", MatcherCost{P50: 10 * time.Second, Samples: 2}, CostLocal},
		{"slow at threshold", MatcherCost{P50: slow, Samples: 10}, CostIO},
		{"slow above threshold", MatcherCost{P50: 3 * time.Second, Samples: 10}, CostIO},
		{"fast", MatcherCost{P50: 5 * time.Millisecond, Samples: 10}, CostLocal},
		{"zero threshold never io", MatcherCost{P50: 3 * time.Second, Samples: 10}, CostLocal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			threshold := slow
			if tt.name == "zero threshold never io" {
				threshold = 0
			}
			assert.Equal(t, tt.want, tt.cost.Resolve(threshold, 3))
		})
	}
}

// TestContext_MatcherCost 默认零值，注入后可读回。
func TestContext_MatcherCost(t *testing.T) {
	ctx := &Context{}
	assert.Equal(t, MatcherCost{}, ctx.MatcherCost())
	assert.Equal(t, CostLocal, ctx.MatcherCost().Resolve(time.Second, 1))

	ctx.SetMatcherCost(MatcherCost{P50: 2 * time.Second, Samples: 5})
	assert.Equal(t, MatcherCost{P50: 2 * time.Second, Samples: 5}, ctx.MatcherCost())

	var nilCtx *Context
	nilCtx.SetMatcherCost(MatcherCost{P50: time.Second, Samples: 1})
	assert.Equal(t, MatcherCost{}, nilCtx.MatcherCost())
}
