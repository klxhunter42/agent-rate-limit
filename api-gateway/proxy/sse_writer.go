package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
)

const maxPoolBufSize = 512 * 1024

var sseBufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

var (
	sseDataPrefix  = []byte("data: ")
	sseEventPrefix = []byte("event: ")
	sseLineEnd     = []byte("\n\n")
	sseNewline     = []byte("\n")
)

func getBuf() *bytes.Buffer {
	b := sseBufPool.Get().(*bytes.Buffer)
	b.Reset()
	return b
}

func putBuf(b *bytes.Buffer) {
	if b.Cap() > maxPoolBufSize {
		return
	}
	b.Reset()
	sseBufPool.Put(b)
}

func writeRawLine(w io.Writer, line []byte) {
	buf := getBuf()
	buf.Write(line)
	buf.WriteByte('\n')
	w.Write(buf.Bytes())
	putBuf(buf)
}

func writeSSEEvent(w io.Writer, event string, data []byte) {
	buf := getBuf()
	buf.Write(sseEventPrefix)
	buf.WriteString(event)
	buf.WriteByte('\n')
	buf.Write(sseDataPrefix)
	buf.Write(data)
	buf.Write(sseLineEnd)
	w.Write(buf.Bytes())
	putBuf(buf)
}

func writeSSEData(w io.Writer, data []byte) {
	buf := getBuf()
	buf.Write(sseDataPrefix)
	buf.Write(data)
	buf.Write(sseLineEnd)
	w.Write(buf.Bytes())
	putBuf(buf)
}

func writeSSEJSON(w io.Writer, flusher http.Flusher, event string, v any) {
	b, _ := json.Marshal(v)
	writeSSEEvent(w, event, b)
	if flusher != nil {
		flusher.Flush()
	}
}
