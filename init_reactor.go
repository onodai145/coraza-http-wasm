//go:build tinygo

package main

import _ "unsafe"

// Reactor modules run package initialization, while HTTP WASM hosts call _start.
// The build adapter aliases _initialize to _start and exposes these scheduled
// wrappers under the existing HTTP WASM names.
func init() { main() }

//go:linkname legacyHandleRequest handle_request
func legacyHandleRequest() uint64

//go:linkname legacyHandleResponse handle_response
func legacyHandleResponse(uint32, uint32)

//go:wasmexport http_wasm_request
func httpWasmRequest() uint64 { return legacyHandleRequest() }

//go:wasmexport http_wasm_response
func httpWasmResponse(ctx, failed uint32) { legacyHandleResponse(ctx, failed) }
