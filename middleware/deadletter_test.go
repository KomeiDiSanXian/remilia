package middleware

import (
	"errors"
	"testing"

	"github.com/KomeiDiSanXian/remilia/infra/dlq"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeadLetter(t *testing.T) {
	t.Run("nil queue returns valid middleware", func(t *testing.T) {
		mw := DeadLetter(nil)
		require.NotNil(t, mw)

		handler := mw(mockHandler(nil, 0))
		ctx := createTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
	})

	t.Run("handler returns nil - no enqueue", func(t *testing.T) {
		queue := dlq.New[platform.Event](dlq.Config[platform.Event]{
			MaxSize: 100,
		})
		t.Cleanup(func() { _ = queue.Close(100) })

		mw := DeadLetter(queue)
		handler := mw(mockHandler(nil, 0))

		ctx := createTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
		assert.Equal(t, 0, queue.Size())
	})

	t.Run("handler returns error - event enqueued", func(t *testing.T) {
		queue := dlq.New[platform.Event](dlq.Config[platform.Event]{
			MaxSize: 100,
		})
		t.Cleanup(func() { _ = queue.Close(100) })

		testErr := errors.New("dead letter test error")
		mw := DeadLetter(queue)
		handler := mw(mockHandler(testErr, 0))

		ctx := createTestContext()
		err := handler(ctx)

		assert.Error(t, err)
		assert.ErrorIs(t, err, testErr)
		assert.Equal(t, 1, queue.Size())

		stats := queue.Stats()
		assert.Equal(t, 1, stats.QueueSize)
	})
}
