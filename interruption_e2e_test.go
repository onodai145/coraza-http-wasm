//go:build e2e

package main_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/http-wasm/http-wasm-host-go/handler"
	nethttp "github.com/http-wasm/http-wasm-host-go/handler/nethttp"
	"github.com/stretchr/testify/require"
)

// An empty denial must be explicitly framed, including when another buffering
// middleware wraps the host. Response-phase denials must not leak backend data.
func TestE2EInterruptionEmptyResponse(t *testing.T) {
	for phase := 1; phase <= 4; phase++ {
		t.Run(fmt.Sprint(phase), func(t *testing.T) {
			rules := fmt.Sprintf(`SecRuleEngine On
SecRequestBodyAccess On
SecResponseBodyAccess On
SecResponseBodyMimeType text/plain
SecAction "id:1,phase:%d,deny,status:403"`, phase)
			config, err := json.Marshal(map[string]interface{}{"includeCRS": false, "directives": []string{rules}})
			require.NoError(t, err)
			mw, err := nethttp.NewMiddleware(testCtx, guest, handler.GuestConfig(config))
			require.NoError(t, err)
			defer mw.Close(testCtx)
			called := false
			wrapped := mw.NewHandler(testCtx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.Header().Set("Content-Type", "text/plain")
				w.Header().Set("Content-Length", "13")
				fmt.Fprint(w, "sensitivebody")
			}))
			response := httptest.NewRecorder()
			wrapped.ServeHTTP(response, httptest.NewRequest("GET", "http://example.com/", nil))
			require.Equal(t, 403, response.Code)
			require.Equal(t, "0", response.Header().Get("Content-Length"))
			require.Empty(t, response.Body.String())
			require.Equal(t, phase >= 3, called)
		})
	}
}
