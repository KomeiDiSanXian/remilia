package pool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTypedPool_GetPut(t *testing.T) {
	p := NewTypedPool(func() []int { return make([]int, 0, 4) })

	x := p.Get()
	assert.Equal(t, 0, len(x))
	assert.GreaterOrEqual(t, cap(x), 4)

	x = append(x, 1, 2, 3)
	p.Put(x)

	y := p.Get()
	assert.GreaterOrEqual(t, cap(y), 4)
}

func TestTypedPool_StatsIncrements(t *testing.T) {
	p := NewTypedPool(func() int { return 1 })
	_ = p.Get()
	_ = p.Get()
	p.Put(1)

	stats := p.Stats()
	assert.GreaterOrEqual(t, stats.Gets, uint64(2))
}
