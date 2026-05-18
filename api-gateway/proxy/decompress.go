package proxy

import (
	"compress/flate"
	"compress/gzip"
	"io"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// decompressReader wraps resp.Body with the appropriate decompressor
// based on the Content-Encoding header from the upstream response.
func decompressReader(body io.ReadCloser, encoding string) io.ReadCloser {
	enc := strings.ToLower(strings.TrimSpace(encoding))
	switch enc {
	case "gzip":
		gr, err := gzip.NewReader(body)
		if err != nil {
			return body
		}
		return &readCloserWrapper{Reader: gr, Closer: body}
	case "deflate":
		return &readCloserWrapper{Reader: flate.NewReader(body), Closer: body}
	case "zstd":
		zr, err := zstd.NewReader(body)
		if err != nil {
			return body
		}
		return &zstdReadCloser{Decoder: zr, Orig: body}
	default:
		return body
	}
}

type readCloserWrapper struct {
	io.Reader
	io.Closer
}

type zstdReadCloser struct {
	*zstd.Decoder
	Orig io.Closer
}

func (z *zstdReadCloser) Close() error {
	z.Decoder.Close()
	return z.Orig.Close()
}
