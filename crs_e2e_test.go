//go:build e2e

package main_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/http-wasm/http-wasm-host-go/api"
	"github.com/http-wasm/http-wasm-host-go/handler"
	nethttp "github.com/http-wasm/http-wasm-host-go/handler/nethttp"
	"github.com/stretchr/testify/require"
)

type crsLogger struct {
	sync.Mutex
	messages []string
}

func (*crsLogger) IsEnabled(api.LogLevel) bool { return true }
func (l *crsLogger) Log(_ context.Context, _ api.LogLevel, message string) {
	l.Lock()
	defer l.Unlock()
	l.messages = append(l.messages, message)
}
func (l *crsLogger) reset()       { l.Lock(); defer l.Unlock(); l.messages = nil }
func (l *crsLogger) text() string { l.Lock(); defer l.Unlock(); return strings.Join(l.messages, "\n") }

// TestE2ERealCRS uses the bundled, unmodified CRS rules inside the WASM guest.
// No custom detection or blocking rules are used.
func TestE2ERealCRS(t *testing.T) {
	for _, mode := range []struct {
		name, engine string
		threshold    int
	}{
		{"blocking", "On", 5}, {"detection", "DetectionOnly", 5}, {"high_threshold", "On", 10000},
	} {
		t.Run(mode.name, func(t *testing.T) {
			logger := &crsLogger{}
			rules := fmt.Sprintf(`
Include @coraza.conf-recommended
SecRuleEngine %s
SecAuditEngine Off
SecRequestBodyAccess On
SecResponseBodyAccess On
SecResponseBodyMimeType text/html
Include @crs-setup.conf.example
SecAction "id:100001,phase:1,pass,nolog,setvar:tx.max_num_args=255,setvar:tx.inbound_anomaly_score_threshold=%d,setvar:tx.outbound_anomaly_score_threshold=%d"
Include @owasp_crs/**.conf
`, mode.engine, mode.threshold, mode.threshold)
			config, err := json.Marshal(map[string]interface{}{"directives": []string{rules}})
			require.NoError(t, err)
			mw, err := nethttp.NewMiddleware(testCtx, guest, handler.GuestConfig(config), handler.Logger(logger))
			require.NoError(t, err, logger.text())
			defer mw.Close(testCtx)
			wrapped := mw.NewHandler(testCtx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				if r.URL.Path == "/leak" {
					fmt.Fprint(w, "You have an error in your SQL syntax; check the manual that corresponds to your MySQL server version")
				} else {
					fmt.Fprint(w, "hello")
				}
			}))
			manyArgs := url.Values{}
			for i := 0; i < 256; i++ {
				manyArgs.Set(fmt.Sprintf("param%d", i), "1")
			}
			for _, tc := range []struct{ name, method, target, body, rule string }{
				{"normal", "GET", "/?q=hello", "", ""},
				{"many_args", "GET", "/?" + manyArgs.Encode(), "", "920380"},
				{"sqli", "GET", "/?q=" + url.QueryEscape("1' OR '1'='1' --"), "", "942"},
				{"xss", "GET", "/?q=" + url.QueryEscape("<script>alert(1)</script>"), "", "941"},
				{"request_body", "POST", "/", "q=" + url.QueryEscape("<script>alert(1)</script>"), "941"},
				{"response_body", "GET", "/leak", "", "951"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					logger.reset()
					req := httptest.NewRequest(tc.method, "http://example.com"+tc.target, strings.NewReader(tc.body))
					req.Header.Set("User-Agent", "Mozilla/5.0")
					req.Header.Set("Accept", "text/html")
					if tc.body != "" {
						req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					}
					result := httptest.NewRecorder()
					wrapped.ServeHTTP(result, req)
					want := 200
					if tc.rule != "" && mode.name == "blocking" {
						want = 403
					}
					require.Equal(t, want, result.Code, logger.text()+"\n"+result.Body.String())
					if tc.rule != "" {
						require.Contains(t, logger.text(), `[id "`+tc.rule, "expected actual CRS detection")
						if mode.name == "blocking" {
							blockingID := "949110"
							if tc.name == "response_body" {
								blockingID = "959100"
							}
							require.Contains(t, logger.text(), blockingID, "expected CRS anomaly scoring block")
						}
					}
				})
			}
		})
	}
}

// The WASM has no filesystem for Coraza's spill-to-disk path. Its inspection
// memory limit must match the inspection limit, independently of the outer cap.
func TestE2ERealCRSLargeBody(t *testing.T) {
	const rules = `
Include @coraza.conf-recommended
SecRuleEngine On
SecAuditEngine Off
SecRequestBodyLimit 13107200
SecRequestBodyInMemoryLimit 13107200
Include @crs-setup.conf.example
Include @owasp_crs/**.conf
`
	config, err := json.Marshal(map[string]interface{}{"directives": []string{rules}})
	require.NoError(t, err)
	mw, err := nethttp.NewMiddleware(testCtx, guest, handler.GuestConfig(config))
	require.NoError(t, err)
	defer mw.Close(testCtx)
	wrapped := mw.NewHandler(testCtx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(data)
	}))
	body := "q=" + strings.Repeat("a", 1024*1024-2)
	response := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "http://example.com/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	wrapped.ServeHTTP(response, req)
	require.Equal(t, 200, response.Code)
	require.Equal(t, body, response.Body.String())
}
