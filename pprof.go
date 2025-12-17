package remilia

import (
	"net/http"
	_ "net/http/pprof"

	"github.com/sirupsen/logrus"
)

func init() {
	go func() {
		// Start the pprof server on the default port 9001
		if err := http.ListenAndServe("localhost:9001", nil); err != nil {
			logrus.Errorf("[Remilia] Failed to start pprof server: %v", err)
			return
		}
		logrus.Info("[Remilia] pprof server started on localhost:9001")
	}()
}
