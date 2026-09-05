package sender

import (
	"errors"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestOutboundObserverCounter 验证出站观察者按平台记录成功/失败。
func TestOutboundObserverCounter(t *testing.T) {
	beforeOK := testutil.ToFloat64(sendTotal.WithLabelValues("qq", "success"))
	beforeErr := testutil.ToFloat64(sendTotal.WithLabelValues("qq", "error"))

	obs := NewOutboundObserver("qq")
	obs.OnOutbound("chat1", platform.SendRequest{}, platform.SendResult{}, nil)
	obs.OnOutbound("chat1", platform.SendRequest{}, platform.SendResult{}, errors.New("send failed"))

	if got := testutil.ToFloat64(sendTotal.WithLabelValues("qq", "success")); got != beforeOK+1 {
		t.Errorf("success 计数: got %v, 期望 %v", got, beforeOK+1)
	}
	if got := testutil.ToFloat64(sendTotal.WithLabelValues("qq", "error")); got != beforeErr+1 {
		t.Errorf("error 计数: got %v, 期望 %v", got, beforeErr+1)
	}
}
