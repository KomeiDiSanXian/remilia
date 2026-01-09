package remilia

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContext_SetState_RetryAttemptIsForbiddenInUserState(t *testing.T) {
	ctx := NewContext(nil, nil)
	ctx.Set("retry_attempt", 2)
	_, ok := ctx.Get("retry_attempt")
	assert.False(t, ok)
}
