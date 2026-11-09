# Local patch

Source: github.com/http-wasm/http-wasm-guest-tinygo v0.4.0 (Apache-2.0).
Only handler/ plus module metadata/license are copied.

handler/internal/mem/mem.go: GetString large-buffer path now uses string(buf).
The original unsafe cast treated string contents as a Go string header and caused invalid pointers/lengths for URIs over ReadBufLimit.
Reproducer: root TestE2ERealCRS/*/many_args and FTW 920380-1.

handler/body.go: Write and WriteString now forward zero-length writes to the
host. HTTP WASM write_body with buf_len=0 replaces the original body with
nothing; the SDK previously skipped the import, leaving backend data present
after a response-phase denial. Reproducer: root TestE2EInterruptionEmptyResponse.
Keep the public HTTP WASM API unchanged. Reevaluate this patch on upstream releases.
