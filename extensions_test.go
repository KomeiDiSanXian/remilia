package remilia

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type extFoo struct{ N int }

type extBar struct{ S string }

func TestExtensions_SetGet_Typed(t *testing.T) {
	e := newExtensions()

	ExtSet(e, extFoo{N: 1})
	v, ok := ExtGet[extFoo](e)
	require.True(t, ok)
	require.Equal(t, 1, v.N)

	_, ok = ExtGet[extBar](e)
	require.False(t, ok)
}

func TestExtensions_GetOrInit_Once(t *testing.T) {
	e := newExtensions()

	calls := 0
	v1 := ExtGetOrInit(e, func() extFoo {
		calls++
		return extFoo{N: 42}
	})
	v2 := ExtGetOrInit(e, func() extFoo {
		calls++
		return extFoo{N: 100}
	})

	require.Equal(t, 1, calls)
	require.Equal(t, 42, v1.N)
	require.Equal(t, 42, v2.N)
}

func TestExtensions_GetOrInit_Concurrent(t *testing.T) {
	e := newExtensions()

	var calls int
	var mu sync.Mutex

	const workers = 64
	var wg sync.WaitGroup
	wg.Add(workers)
	out := make([]extFoo, workers)

	for i := 0; i < workers; i++ {
		go func(idx int) {
			defer wg.Done()
			v := ExtGetOrInit(e, func() extFoo {
				mu.Lock()
				calls++
				mu.Unlock()
				return extFoo{N: 7}
			})
			out[idx] = v
		}(i)
	}
	wg.Wait()

	require.Equal(t, 1, calls)
	for i := 0; i < workers; i++ {
		require.Equal(t, 7, out[i].N)
	}
}
