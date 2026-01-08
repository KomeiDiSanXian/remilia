package remilia

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContext_SetState_RetryAttemptIsForbiddenInUserState(t *testing.T) {
	ctx := NewContext(nil, nil)
	ctx.SetState("retry_attempt", 2)
	_, ok := ctx.GetState("retry_attempt")
	assert.False(t, ok)
}
