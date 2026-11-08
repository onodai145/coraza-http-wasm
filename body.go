package main

import (
	"io"

	"github.com/http-wasm/http-wasm-guest-tinygo/handler/api"
)

type readWriterTo struct {
	api.Body
}

func (sr readWriterTo) Read(p []byte) (n int, err error) {
	size, eof := sr.Body.Read(p)
	if eof {
		err = io.EOF
	}
	n = int(size)
	return
}

// WriteTo implements io.WriterTo and it is handy for copying the body to the
// into the transaction buffer.
func (sr readWriterTo) WriteTo(w io.Writer) (n int64, err error) {
	size, err := sr.Body.WriteTo(w)
	return int64(size), err
}

var _ io.Reader = readWriterTo{}
var _ io.WriterTo = readWriterTo{}

// bodyWriter adapts the HTTP WASM write_body API to io.Writer. The host caches
// the original read stream separately, so it can still be drained after the
// first write creates the replacement body.
type bodyWriter struct{ api.Body }

func (w bodyWriter) Write(p []byte) (int, error) { w.Body.Write(p); return len(p), nil }

// restoreRequestBody follows Coraza's net/http connector: replay the inspected
// prefix, then the unread remainder when ProcessPartial was used. Copy in
// chunks so an oversized partial body is not duplicated in the WASM heap.
// The host buffers the reconstructed body, so deployment must enforce a
// finite request-size limit before this middleware (also for DetectionOnly).
func restoreRequestBody(tx interface{ RequestBodyReader() (io.Reader, error) }, body api.Body) error {
	inspected, err := tx.RequestBodyReader()
	if err != nil {
		return err
	}
	_, err = io.Copy(bodyWriter{body}, io.MultiReader(inspected, readWriterTo{body}))
	return err
}
