package remilia

import (
	"net/http"
	_ "net/http/pprof" // Import for side-effect: registers pprof handlers
)

// StartPprofServer starts the pprof HTTP server on the specified address.
//
// This function should be called explicitly if you need pprof profiling.
// The pprof handlers are registered on the default http.ServeMux.
//
// Example:
//
//	go remilia.StartPprofServer("localhost:9001")
//
// To access profiles:
//   - CPU profile: http://localhost:9001/debug/pprof/profile
//   - Heap profile: http://localhost:9001/debug/pprof/heap
//   - Goroutines: http://localhost:9001/debug/pprof/goroutine
func StartPprofServer(addr string) error {
	return http.ListenAndServe(addr, nil)
}
