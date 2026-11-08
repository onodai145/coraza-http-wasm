//go:build e2e

package main_test

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/http-wasm/http-wasm-host-go/handler"
	nethttp "github.com/http-wasm/http-wasm-host-go/handler/nethttp"
	"github.com/stretchr/testify/require"
)

// The FTW Go-platform assertions must still prove parser-level rejection.
// If a request reaches the WAF it returns 403, which cannot satisfy this test.
func TestE2EProtocolRejectionBeforeWAF(t *testing.T) {
	mw, err := nethttp.NewMiddleware(testCtx, guest, handler.GuestConfig([]byte(`{"includeCRS":false,"directives":["SecRuleEngine On","SecRule REQUEST_URI \"@rx .\" \"id:100001,phase:1,deny,status:403\""]}`)))
	require.NoError(t, err)
	defer mw.Close(testCtx)
	wrapped := mw.NewHandler(testCtx, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	var reached atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1); wrapped.ServeHTTP(w, r) }))
	defer server.Close()
	for _, tc := range []struct {
		name, version string
		status        int
	}{
		{"HTTP09", "HTTP/0.9", 505}, {"HTTP08", "HTTP/0.8", 505}, {"missing_version", "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := reached.Load()
			conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
			require.NoError(t, err)
			defer conn.Close()
			require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
			_, err = conn.Write([]byte("GET / " + tc.version + "\r\nHost: localhost\r\nConnection: close\r\n\r\n"))
			require.NoError(t, err)
			result, err := http.ReadResponse(bufio.NewReader(conn), nil)
			require.NoError(t, err)
			defer result.Body.Close()
			require.Equal(t, tc.status, result.StatusCode)
			require.Equal(t, before, reached.Load(), "invalid request reached the WAF")
		})
	}
}
