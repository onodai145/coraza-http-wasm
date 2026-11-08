//go:build e2e

package main_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/http-wasm/http-wasm-host-go/handler"
	nethttp "github.com/http-wasm/http-wasm-host-go/handler/nethttp"
	"github.com/stretchr/testify/require"
)

// Inspection must leave the complete original request body for the backend,
// including payloads larger than the guest SDK's read buffer.
func TestE2ERequestBodyPreservation(t *testing.T) {
	for _, mode := range []struct{ name, rules string }{
		{"inspect", "SecRuleEngine On\nSecRequestBodyAccess On"},
		{"partial", "SecRuleEngine On\nSecRequestBodyAccess On\nSecRequestBodyLimit 128\nSecRequestBodyLimitAction ProcessPartial"},
		{"detection", "SecRuleEngine DetectionOnly\nSecRequestBodyAccess On\nSecRequestBodyLimit 128\nSecRequestBodyLimitAction Reject"},
		{"body_off", "SecRuleEngine On\nSecRequestBodyAccess Off"},
		{"phase1_body_off", "SecRuleEngine On\nSecRequestBodyAccess On\nSecAction \"id:100002,phase:1,pass,nolog,ctl:requestBodyAccess=Off\""},
		{"engine_off", "SecRuleEngine Off\nSecRequestBodyAccess On"},
	} {
		t.Run(mode.name, func(t *testing.T) {
			config, err := json.Marshal(map[string]interface{}{"includeCRS": false, "directives": []string{mode.rules, "SecResponseBodyAccess Off"}})
			require.NoError(t, err)
			mw, err := nethttp.NewMiddleware(testCtx, guest, handler.GuestConfig(config))
			require.NoError(t, err)
			defer mw.Close(testCtx)
			wrapped := mw.NewHandler(testCtx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				w.Write(body)
			}))
			for _, size := range []int{256, 4096, 16384} {
				t.Run(fmt.Sprint(size), func(t *testing.T) {
					body := "q=" + strings.Repeat("a", size)
					req := httptest.NewRequest("POST", "http://example.com/", strings.NewReader(body))
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					result := httptest.NewRecorder()
					wrapped.ServeHTTP(result, req)
					require.Equal(t, 200, result.Code, result.Body.String())
					require.Equal(t, len(body), result.Body.Len(), "inspection changed body length")
					require.Equal(t, body, result.Body.String())
				})
			}
		})
	}
}

func TestE2ERequestBodyLimitReject(t *testing.T) {
	mw, err := nethttp.NewMiddleware(testCtx, guest, handler.GuestConfig([]byte(`{"includeCRS":false,"directives":["SecRuleEngine On","SecRequestBodyAccess On","SecRequestBodyLimit 128","SecRequestBodyLimitAction Reject"]}`)))
	require.NoError(t, err)
	defer mw.Close(testCtx)
	called := false
	wrapped := mw.NewHandler(testCtx, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest("POST", "http://example.com/", strings.NewReader(strings.Repeat("a", 4096)))
	req.Header.Set("Content-Type", "application/octet-stream")
	result := httptest.NewRecorder()
	wrapped.ServeHTTP(result, req)
	require.Equal(t, 413, result.Code)
	require.False(t, called, "oversized body reached the backend")
}

// A phase-1 ctl action may turn the engine off after the connector's initial
// fast path. Such a request must reach the backend without body inspection.
type backendOnlyBody struct {
	io.Reader
	backendStarted *bool
}

func (b backendOnlyBody) Read(p []byte) (int, error) {
	if !*b.backendStarted {
		return 0, fmt.Errorf("body read before backend despite ctl:ruleEngine=Off")
	}
	return b.Reader.Read(p)
}
func (backendOnlyBody) Close() error { return nil }
func TestE2EPhase1EngineOffDoesNotReadBody(t *testing.T) {
	mw, err := nethttp.NewMiddleware(testCtx, guest, handler.GuestConfig([]byte(`{"includeCRS":false,"directives":["SecRuleEngine On","SecRequestBodyAccess On","SecAction \"id:100003,phase:1,pass,nolog,ctl:ruleEngine=Off\""]}`)))
	require.NoError(t, err)
	defer mw.Close(testCtx)
	started := false
	payload := "unchanged request body"
	wrapped := mw.NewHandler(testCtx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started = true
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		w.Write(body)
	}))
	req := httptest.NewRequest("POST", "http://example.com/", nil)
	req.Body = backendOnlyBody{strings.NewReader(payload), &started}
	req.ContentLength = int64(len(payload))
	result := httptest.NewRecorder()
	wrapped.ServeHTTP(result, req)
	require.True(t, started)
	require.Equal(t, 200, result.Code, result.Body.String())
	require.Equal(t, payload, result.Body.String())
}
