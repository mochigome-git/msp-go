// Package profiler serves net/http/pprof on localhost when enabled.
//
//	PPROF_PORT=6060, then inside the container:
//	curl http://127.0.0.1:6060/debug/pprof/heap > heap.out
//	go tool pprof heap.out
package profiler

import (
	"log"
	"net/http"
	"net/http/pprof"
	"strconv"
)

// Start serves pprof on 127.0.0.1:port and blocks. port 0 disables it.
// A failure (e.g. port in use) is logged; it never stops the application.
func Start(port int, logger *log.Logger) {
	if port <= 0 {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	addr := "127.0.0.1:" + strconv.Itoa(port)
	logger.Printf("Starting pprof on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Printf("⚠️ pprof server stopped: %v (application keeps running)", err)
	}
}
