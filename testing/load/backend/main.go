// Test-only backend: the pinned httpbin application with explicit HTTP deadlines.
package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/mccutchen/go-httpbin/v2/httpbin"
)

func main() {
	headerTimeout := flag.Duration("read-header-timeout", 30*time.Second, "Header read deadline for the controlled experiment")
	flag.Parse()
	logger := log.New(os.Stdout, "backend ", log.Ldate|log.Ltime|log.Lmicroseconds)
	app := httpbin.New(httpbin.WithMaxBodySize(15*1024*1024), httpbin.WithObserver(httpbin.StdLogObserver(logger)))
	server := &http.Server{
		Addr: ":8000", Handler: app.Handler(), ReadHeaderTimeout: *headerTimeout,
		ReadTimeout: 120 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second,
		ConnState: func(c net.Conn, state http.ConnState) { logger.Printf("connection=%s state=%s", c.RemoteAddr(), state) },
	}
	logger.Printf("read_header_timeout=%s read_timeout=%s", *headerTimeout, server.ReadTimeout)
	logger.Fatal(server.ListenAndServe())
}
