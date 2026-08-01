package sauce

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRedactTransportError 验证传输错误中的 SauceNAO api_key 被脱敏。
func TestRedactTransportError(t *testing.T) {
	uerr := &url.Error{
		Op:  "Get",
		URL: "https://saucenao.com/search.php?api_key=supersecretkey&output_type=2&db=999&url=https%3A%2F%2Fx%2Fa.jpg",
		Err: context.DeadlineExceeded,
	}

	redacted := redactTransportError(uerr)
	assert.NotContains(t, redacted.Error(), "supersecretkey")
	assert.Contains(t, redacted.Error(), "redacted")

	// errors.As 判定能力保留
	var target *url.Error
	require.True(t, errors.As(redacted, &target))

	// 无凭据参数原样返回
	plain := &url.Error{Op: "Get", URL: "https://iqdb.org/?url=x", Err: context.DeadlineExceeded}
	assert.Same(t, plain, redactTransportError(plain))

	generic := errors.New("boom")
	assert.Same(t, generic, redactTransportError(generic))
}
