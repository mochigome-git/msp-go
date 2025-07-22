package profiler

import (
	"log"
	"net/http"
	"strconv"
)

func Start(port int, logger *log.Logger) {
	addr := "127.0.0.1:" + strconv.Itoa(port)
	logger.Printf("Starting pprof on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		logger.Fatalf("pprof server failed: %v", err)
	}
}
