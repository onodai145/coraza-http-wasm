//go:build e2e

package main_test

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/http-wasm/http-wasm-host-go/handler"
	nethttp "github.com/http-wasm/http-wasm-host-go/handler/nethttp"
	"github.com/stretchr/testify/require"
)

// This opt-in policy is a diagnostic example, not a global/default WAF policy.
//
//go:embed testing/argumentlimit/early-arguments.conf
var earlyArgumentPolicy string

func argumentLimitHandler(t *testing.T, engine string) (http.Handler, *crsLogger, *bool) {
	t.Helper()
	logger := &crsLogger{}
	rules := fmt.Sprintf(`
Include @coraza.conf-recommended
SecRuleEngine %s
SecAuditEngine Off
SecRequestBodyLimit 13107200
SecRequestBodyInMemoryLimit 13107200
Include @crs-setup.conf.example
%s
# Test-only marker: an early denial must not reach the following CRS rules.
SecAction "id:100103,phase:2,pass,log,msg:'Argument policy reached CRS'"
Include @owasp_crs/**.conf
`, engine, earlyArgumentPolicy)
	config, err := json.Marshal(map[string]any{"directives": []string{rules}})
	require.NoError(t, err)
	mw, err := nethttp.NewMiddleware(testCtx, guest, handler.GuestConfig(config), handler.Logger(logger))
	require.NoError(t, err, logger.text())
	t.Cleanup(func() { require.NoError(t, mw.Close(testCtx)) })
	called := new(bool)
	wrapped := mw.NewHandler(testCtx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, err = w.Write(data)
		require.NoError(t, err)
	}))
	return wrapped, logger, called
}

// Missing/reordered policy must fail the early-denial cases. All cases use
// the actual embedded CRS; the marker only observes whether phase 2 reached it.
func TestE2EArgumentLimits(t *testing.T) {
	type exampleCase struct {
		name, contentType, body, target string
		status                          int
		guardID, crsID                  string
	}
	const form = "application/x-www-form-urlencoded"
	const jsonType = "application/json"
	totalBoundary := "q0=" + strings.Repeat("a", 16382) + "&q1=" + strings.Repeat("a", 16382) + "&q2=" + strings.Repeat("a", 16382) + "&q3=" + strings.Repeat("a", 16382)
	totalOver := "q0=" + strings.Repeat("a", 16383) + "&q1=" + strings.Repeat("a", 16382) + "&q2=" + strings.Repeat("a", 16382) + "&q3=" + strings.Repeat("a", 16382)
	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	part, err := writer.CreateFormFile("upload", "test.txt")
	require.NoError(t, err)
	_, err = io.WriteString(part, strings.Repeat("a", 1024*1024))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	on := []exampleCase{
		{name: "normal", contentType: form, body: "q=hello", status: 200},
		{name: "form_boundary", contentType: form, body: "q=" + strings.Repeat("a", 16384), status: 200},
		{name: "form_one_over", contentType: form, body: "q=" + strings.Repeat("a", 16385), status: 413, guardID: "100101"},
		{name: "form_1MiB", contentType: form, body: "q=" + strings.Repeat("a", 1024*1024-2), status: 413, guardID: "100102"},
		{name: "percent_encoded_one_over", contentType: form, body: "q=" + strings.Repeat("%61", 16385), status: 413, guardID: "100101"},
		{name: "utf8_boundary", contentType: form, body: "q=" + url.QueryEscape(strings.Repeat("é", 8192)), status: 200},
		{name: "utf8_one_over", contentType: form, body: "q=" + url.QueryEscape(strings.Repeat("é", 8192)+"x"), status: 413, guardID: "100101"},
		{name: "name_one_over", contentType: form, body: strings.Repeat("a", 16385) + "=x", status: 413, guardID: "100101"},
		{name: "json_boundary", contentType: jsonType, body: `{"q":"` + strings.Repeat("a", 16384) + `"}`, status: 200},
		{name: "json_one_over", contentType: jsonType, body: `{"q":"` + strings.Repeat("a", 16385) + `"}`, status: 413, guardID: "100101"},
		{name: "json_escape_one_over", contentType: jsonType, body: `{"q":"` + strings.Repeat(`\u0061`, 16385) + `"}`, status: 413, guardID: "100101"},
		{name: "json_array_one_over", contentType: jsonType, body: `{"q":["small","` + strings.Repeat("a", 16385) + `"]}`, status: 413, guardID: "100101"},
		{name: "json_1MiB", contentType: jsonType, body: `{"q":"` + strings.Repeat("a", 1024*1024) + `"}`, status: 413, guardID: "100102"},
		{name: "total_boundary", contentType: form, body: totalBoundary, status: 200},
		{name: "total_one_over", contentType: form, body: totalOver, status: 413, guardID: "100102"},
		{name: "get_plus_post_total_over", contentType: form, body: totalBoundary, target: "/anything?extra=a", status: 413, guardID: "100102"},
		{name: "json_total_over", contentType: jsonType, body: `{"q0":"` + strings.Repeat("a", 16384) + `","q1":"` + strings.Repeat("a", 16384) + `","q2":"` + strings.Repeat("a", 16384) + `","q3":"` + strings.Repeat("a", 16384) + `"}`, status: 413, guardID: "100102"},
		{name: "sqli", contentType: form, body: "q=" + url.QueryEscape("1' OR '1'='1' --"), status: 403, crsID: "942"},
		{name: "xss", contentType: form, body: "q=" + url.QueryEscape("<script>alert(1)</script>"), status: 403, crsID: "941"},
		{name: "json_xss", contentType: jsonType, body: `{"q":"<script>alert(1)</script>"}`, status: 403, crsID: "941"},
		{name: "multipart_file_1MiB", contentType: writer.FormDataContentType(), body: upload.String(), status: 200},
	}
	detection := []exampleCase{
		{name: "normal", contentType: form, body: "q=hello", status: 200},
		{name: "form_one_over", contentType: form, body: "q=" + strings.Repeat("a", 16385), status: 200, guardID: "100101"},
		{name: "json_one_over", contentType: jsonType, body: `{"q":"` + strings.Repeat("a", 16385) + `"}`, status: 200, guardID: "100101"},
		{name: "total_one_over", contentType: form, body: totalOver, status: 200, guardID: "100102"},
		{name: "sqli", contentType: form, body: "q=" + url.QueryEscape("1' OR '1'='1' --"), status: 200, crsID: "942"},
		{name: "xss", contentType: form, body: "q=" + url.QueryEscape("<script>alert(1)</script>"), status: 200, crsID: "941"},
	}
	for _, mode := range []struct {
		engine string
		cases  []exampleCase
	}{{"On", on}, {"DetectionOnly", detection}} {
		t.Run(mode.engine, func(t *testing.T) {
			wrapped, logger, called := argumentLimitHandler(t, mode.engine)
			for _, tc := range mode.cases {
				t.Run(tc.name, func(t *testing.T) {
					logger.reset()
					*called = false
					target := tc.target
					if target == "" {
						target = "/anything"
					}
					request := httptest.NewRequest("POST", "http://example.com"+target, strings.NewReader(tc.body))
					request.Header.Set("Content-Type", tc.contentType)
					request.Header.Set("User-Agent", "Mozilla/5.0")
					request.Header.Set("Accept", "text/html")
					response := httptest.NewRecorder()
					started := time.Now()
					wrapped.ServeHTTP(response, request)
					elapsed := time.Since(started)
					t.Logf("body_bytes=%d elapsed=%s status=%d", len(tc.body), elapsed, response.Code)
					require.Equal(t, tc.status, response.Code, logger.text())
					if tc.status == 200 {
						require.True(t, *called)
						require.Equal(t, tc.body, response.Body.String(), "backend must receive every original byte")
					} else {
						require.False(t, *called)
						require.Empty(t, response.Body.String())
						require.Equal(t, "0", response.Header().Get("Content-Length"))
					}
					if tc.guardID != "" {
						require.Contains(t, logger.text(), `[id "`+tc.guardID+`"]`)
					}
					if tc.crsID != "" {
						require.Contains(t, logger.text(), `[id "`+tc.crsID, "actual CRS detection must remain active")
						if mode.engine == "On" {
							require.Contains(t, logger.text(), `[id "949110"]`, "actual CRS anomaly scoring must block")
						}
					}
					if mode.engine == "On" && tc.guardID != "" {
						require.NotContains(t, logger.text(), `[id "100103"]`, "early denial must precede CRS")
					} else {
						require.Contains(t, logger.text(), `[id "100103"]`, "all other requests must still enter CRS")
					}
				})
			}
		})
	}
}
