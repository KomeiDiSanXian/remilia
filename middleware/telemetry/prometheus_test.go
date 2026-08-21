package telemetry

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/middleware/testutil"
	"github.com/prometheus/client_golang/prometheus"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testPromNS atomic.Int64

func TestPrometheusMetrics(t *testing.T) {
	t.Run("creates valid middleware", func(t *testing.T) {
		ns := fmt.Sprintf("test_%d", testPromNS.Add(1))
		mw := PrometheusMetrics(ns)
		assert.NotNil(t, mw)
	})

	t.Run("wraps handler without panic", func(t *testing.T) {
		ns := fmt.Sprintf("test_%d", testPromNS.Add(1))
		mw := PrometheusMetrics(ns)
		handler := mw(testutil.MockHandler(nil, 0))

		ctx := testutil.CreateTestContext()
		err := handler(ctx)

		assert.NoError(t, err)
	})

	t.Run("increments counters on call", func(t *testing.T) {
		ns := fmt.Sprintf("test_%d", testPromNS.Add(1))
		mw := PrometheusMetrics(ns)
		handler := mw(testutil.MockHandler(nil, 0))

		ctx := testutil.CreateTestContext()
		require.NoError(t, handler(ctx))

		fqName := prometheus.BuildFQName(ns, "", "handler_requests_total")
		expected := fmt.Sprintf(`
# HELP %s Total handler requests
# TYPE %s counter
%s{event="PRIVATE_MESSAGE"} 1
`, fqName, fqName, fqName)

		err := promtestutil.GatherAndCompare(
			prometheus.DefaultGatherer,
			strings.NewReader(expected),
			fqName,
		)
		require.NoError(t, err)
	})

	t.Run("different namespaces create different metrics", func(t *testing.T) {
		ns1 := fmt.Sprintf("test_%d", testPromNS.Add(1))
		ns2 := fmt.Sprintf("test_%d", testPromNS.Add(1))

		mw1 := PrometheusMetrics(ns1)
		mw2 := PrometheusMetrics(ns2)

		handler1 := mw1(testutil.MockHandler(nil, 0))
		handler2 := mw2(testutil.MockHandler(nil, 0))

		ctx1 := testutil.CreateTestContext()
		ctx2 := testutil.CreateTestContext()

		assert.NoError(t, handler1(ctx1))
		assert.NoError(t, handler2(ctx2))
	})
}
