package remilia

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

type extCounter struct {
	N int
}

type extOnce struct {
	V int
}

func TestExtensions_SetGet_Value(t *testing.T) {
	e := newExtensions()
	ExtSet(e, extCounter{N: 1})
	v, ok := ExtGet[extCounter](e)
	require.True(t, ok)
	require.Equal(t, 1, v.N)
}

func TestExtensions_GetOrInit_OnlyOnce(t *testing.T) {
	e := newExtensions()

	var calls int32
	init := func() *extOnce {
		atomic.AddInt32(&calls, 1)
		return &extOnce{V: 42}
	}

	// Concurrency: many goroutines call GetOrInit simultaneously.
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := ExtGetOrInit(e, init)
			require.NotNil(t, got)
			require.Equal(t, 42, got.V)
		}()
	}
	wg.Wait()

	require.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestExtensions_Get_WrongType_ReturnsFalse(t *testing.T) {
	e := newExtensions()
	// Store as *extOnce
	ExtSet(e, &extOnce{V: 1})

	// Try to read as extOnce (non-pointer)
	_, ok := ExtGet[extOnce](e)
	require.False(t, ok)
}
