package http3

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/rand/v2"

	"github.com/Noooste/uquic-go"
	"github.com/Noooste/uquic-go/http3/qlog"
	"github.com/Noooste/uquic-go/qlogwriter"
	"github.com/Noooste/uquic-go/quicvarint"
)

// FrameType is the frame type of a HTTP/3 frame
type FrameType uint64

type frame any

// The maximum length of an encoded HTTP/3 frame header is 16:
// The frame has a type and length field, both QUIC varints (maximum 8 bytes in length)
const frameHeaderLen = 16

type countingByteReader struct {
	quicvarint.Reader
	NumRead int
}

func (r *countingByteReader) ReadByte() (byte, error) {
	b, err := r.Reader.ReadByte()
	if err == nil {
		r.NumRead++
	}
	return b, err
}

func (r *countingByteReader) Read(b []byte) (int, error) {
	n, err := r.Reader.Read(b)
	r.NumRead += n
	return n, err
}

func (r *countingByteReader) Reset() {
	r.NumRead = 0
}

type frameParser struct {
	r         io.Reader
	streamID  quic.StreamID
	closeConn func(quic.ApplicationErrorCode, string) error
}

func (p *frameParser) ParseNext(qlogger qlogwriter.Recorder) (frame, error) {
	r := &countingByteReader{Reader: quicvarint.NewReader(p.r)}
	for {
		t, err := quicvarint.Read(r)
		if err != nil {
			return nil, err
		}
		l, err := quicvarint.Read(r)
		if err != nil {
			return nil, err
		}

		switch t {
		case 0x0: // DATA
			if qlogger != nil {
				qlogger.RecordEvent(qlog.FrameParsed{
					StreamID: p.streamID,
					Raw: qlog.RawInfo{
						Length:        int(l) + r.NumRead,
						PayloadLength: int(l),
					},
					Frame: qlog.Frame{Frame: qlog.DataFrame{}},
				})
			}
			return &dataFrame{Length: l}, nil
		case 0x1: // HEADERS
			return &headersFrame{
				Length:    l,
				headerLen: r.NumRead,
			}, nil
		case 0x4: // SETTINGS
			return parseSettingsFrame(r, l, p.streamID, qlogger)
		case 0x3: // unsupported: CANCEL_PUSH
			if qlogger != nil {
				qlogger.RecordEvent(qlog.FrameParsed{
					StreamID: p.streamID,
					Raw:      qlog.RawInfo{Length: r.NumRead, PayloadLength: int(l)},
					Frame:    qlog.Frame{Frame: qlog.CancelPushFrame{}},
				})
			}
		case 0x5: // unsupported: PUSH_PROMISE
			if qlogger != nil {
				qlogger.RecordEvent(qlog.FrameParsed{
					StreamID: p.streamID,
					Raw:      qlog.RawInfo{Length: r.NumRead, PayloadLength: int(l)},
					Frame:    qlog.Frame{Frame: qlog.PushPromiseFrame{}},
				})
			}
		case 0x7: // GOAWAY
			return parseGoAwayFrame(r, l, p.streamID, qlogger)
		case 0xd: // unsupported: MAX_PUSH_ID
			if qlogger != nil {
				qlogger.RecordEvent(qlog.FrameParsed{
					StreamID: p.streamID,
					Raw:      qlog.RawInfo{Length: r.NumRead, PayloadLength: int(l)},
					Frame:    qlog.Frame{Frame: qlog.MaxPushIDFrame{}},
				})
			}
		case 0x2, 0x6, 0x8, 0x9: // reserved frame types
			if qlogger != nil {
				qlogger.RecordEvent(qlog.FrameParsed{
					StreamID: p.streamID,
					Raw:      qlog.RawInfo{Length: r.NumRead + int(l), PayloadLength: int(l)},
					Frame:    qlog.Frame{Frame: qlog.ReservedFrame{Type: t}},
				})
			}
			p.closeConn(quic.ApplicationErrorCode(ErrCodeFrameUnexpected), "")
			return nil, fmt.Errorf("http3: reserved frame type: %d", t)
		default:
			// unknown frame types
			if qlogger != nil {
				qlogger.RecordEvent(qlog.FrameParsed{
					StreamID: p.streamID,
					Raw:      qlog.RawInfo{Length: r.NumRead, PayloadLength: int(l)},
					Frame:    qlog.Frame{Frame: qlog.UnknownFrame{Type: t}},
				})
			}
		}

		// skip over the payload
		if _, err := io.CopyN(io.Discard, r, int64(l)); err != nil {
			return nil, err
		}
		r.Reset()
	}
}

type dataFrame struct {
	Length uint64
}

func (f *dataFrame) Append(b []byte) []byte {
	b = quicvarint.Append(b, 0x0)
	return quicvarint.Append(b, f.Length)
}

type headersFrame struct {
	Length    uint64
	headerLen int // number of bytes read for type and length field
}

func (f *headersFrame) Append(b []byte) []byte {
	b = quicvarint.Append(b, 0x1)
	return quicvarint.Append(b, f.Length)
}

const (
	SettingsQpackMaxTableCapacity uint64 = 0x1
	SettingsQpackBlockedStreams   uint64 = 0x7

	// SETTINGS_MAX_FIELD_SECTION_SIZE
	SettingsMaxFieldSectionSize = 0x6
	// Extended CONNECT, RFC 9220
	settingExtendedConnect uint64 = 0x8
	// SettingsH3Datagram is used to enable HTTP datagrams, RFC 9297
	SettingsH3Datagram         uint64 = 0x33
	SettingsEnableWebTransport uint64 = 727725890     // Enable WebTransport, RFC 9298
	SettingsGREASE             uint64 = 0x1f*1 + 0x21 // GREASE value, RFC 9114
)

type settingsFrame struct {
	MaxFieldSectionSize int64 // SETTINGS_MAX_FIELD_SECTION_SIZE, -1 if not set

	Datagram        bool              // HTTP Datagrams, RFC 9297
	ExtendedConnect bool              // Extended CONNECT, RFC 9220
	Other           map[uint64]uint64 // all settings that we don't explicitly recognize

	Order []uint64 // the order in which the settings were received, for serialization purposes
}

func pointer[T any](v T) *T {
	return &v
}

func parseSettingsFrame(r *countingByteReader, l uint64, streamID quic.StreamID, qlogger qlogwriter.Recorder) (*settingsFrame, error) {
	if l > 8*(1<<10) {
		return nil, fmt.Errorf("unexpected size for SETTINGS frame: %d", l)
	}
	buf := make([]byte, l)
	if _, err := io.ReadFull(r, buf); err != nil {
		if err == io.ErrUnexpectedEOF {
			return nil, io.EOF
		}
		return nil, err
	}
	frame := &settingsFrame{MaxFieldSectionSize: -1}
	b := bytes.NewReader(buf)
	settingsFrame := qlog.SettingsFrame{MaxFieldSectionSize: -1}
	var readMaxFieldSectionSize, readDatagram, readExtendedConnect bool
	for b.Len() > 0 {
		id, err := quicvarint.Read(b)
		if err != nil { // should not happen. We allocated the whole frame already.
			return nil, err
		}
		val, err := quicvarint.Read(b)
		if err != nil { // should not happen. We allocated the whole frame already.
			return nil, err
		}

		switch id {
		case SettingsMaxFieldSectionSize:
			if readMaxFieldSectionSize {
				return nil, fmt.Errorf("duplicate setting: %d", id)
			}
			readMaxFieldSectionSize = true
			frame.MaxFieldSectionSize = int64(val)
			settingsFrame.MaxFieldSectionSize = int64(val)
		case settingExtendedConnect:
			if readExtendedConnect {
				return nil, fmt.Errorf("duplicate setting: %d", id)
			}
			readExtendedConnect = true
			if val != 0 && val != 1 {
				return nil, fmt.Errorf("invalid value for SETTINGS_ENABLE_CONNECT_PROTOCOL: %d", val)
			}
			frame.ExtendedConnect = val == 1
			if qlogger != nil {
				settingsFrame.ExtendedConnect = pointer(frame.ExtendedConnect)
			}
		case SettingsH3Datagram:
			if readDatagram {
				return nil, fmt.Errorf("duplicate setting: %d", id)
			}
			readDatagram = true
			if val != 0 && val != 1 {
				return nil, fmt.Errorf("invalid value for SETTINGS_H3_DATAGRAM: %d", val)
			}
			frame.Datagram = val == 1
			if qlogger != nil {
				settingsFrame.Datagram = pointer(frame.Datagram)
			}
		default:
			if _, ok := frame.Other[id]; ok {
				return nil, fmt.Errorf("duplicate setting: %d", id)
			}
			if frame.Other == nil {
				frame.Other = make(map[uint64]uint64)
			}
			frame.Other[id] = val
		}
	}
	if qlogger != nil {
		settingsFrame.Other = maps.Clone(frame.Other)

		qlogger.RecordEvent(qlog.FrameParsed{
			StreamID: streamID,
			Raw: qlog.RawInfo{
				Length:        r.NumRead,
				PayloadLength: int(l),
			},
			Frame: qlog.Frame{Frame: settingsFrame},
		})
	}
	return frame, nil
}

func (f *settingsFrame) Append(b []byte) []byte {
	b = quicvarint.Append(b, 0x4)
	var l int
	//if f.MaxFieldSectionSize >= 0 {
	//	l += quicvarint.Len(SettingsMaxFieldSectionSize) + quicvarint.Len(uint64(f.MaxFieldSectionSize))
	//}
	for id, val := range f.Other {
		if id == SettingsGREASE {
			l += quicvarint.Len(quicvarint.Max)
			if val != 0 {
				l += quicvarint.Len(val)
			} else {
				l += quicvarint.Len(quicvarint.Max)
			}
		} else {
			l += quicvarint.Len(id) + quicvarint.Len(val)
		}
	}
	if f.Datagram {
		l += quicvarint.Len(SettingsH3Datagram) + quicvarint.Len(1)
	}
	if f.ExtendedConnect {
		l += quicvarint.Len(settingExtendedConnect) + quicvarint.Len(1)
	}
	b = quicvarint.Append(b, uint64(l))
	//if f.MaxFieldSectionSize >= 0 {
	//	b = quicvarint.Append(b, SettingsMaxFieldSectionSize)
	//	b = quicvarint.Append(b, uint64(f.MaxFieldSectionSize))
	//}
	if f.Datagram {
		b = quicvarint.Append(b, SettingsH3Datagram)
		b = quicvarint.Append(b, 1)
	}
	if f.ExtendedConnect {
		b = quicvarint.Append(b, settingExtendedConnect)
		b = quicvarint.Append(b, 1)
	}
	for id, val := range f.Other {
		if id == SettingsH3Datagram && f.Datagram {
			// We already added this setting.
			continue
		}

		if id == SettingsMaxFieldSectionSize && f.MaxFieldSectionSize >= 0 {
			// We already added this setting.
			continue
		}

		if id == settingExtendedConnect && f.ExtendedConnect {
			// We already added this setting.
			continue
		}

		if id == SettingsGREASE && val == 0 {
			// generate a GREASE value
			key := 0x1f*uint64(rand.Int32()) + 0x21
			val = rand.Uint64() % (1 << 32)
			b = quicvarint.Append(b, key) // GREASE value, RFC 9114
			b = quicvarint.Append(b, val)
			continue // GREASE values are not added to the Other map
		}

		b = quicvarint.Append(b, id)
		b = quicvarint.Append(b, val)
	}
	return b
}

func (f *settingsFrame) AppendWithOrder(b []byte) []byte {
	if f.Order == nil {
		return f.Append(b)
	}

	b = quicvarint.Append(b, 0x4)
	var l int
	for _, id := range f.Order {
		val, ok := f.Other[id]
		if !ok {
			continue // skip unknown settings
		}
		if id == SettingsGREASE {
			l += quicvarint.Len(quicvarint.Max)
			if val != 0 {
				l += quicvarint.Len(val)
			} else {
				l += quicvarint.Len(quicvarint.Max)
			}
		} else {
			l += quicvarint.Len(id) + quicvarint.Len(val)
		}
	}
	if f.Datagram {
		l += quicvarint.Len(SettingsH3Datagram) + quicvarint.Len(1)
	}
	if f.ExtendedConnect {
		l += quicvarint.Len(settingExtendedConnect) + quicvarint.Len(1)
	}
	b = quicvarint.Append(b, uint64(l))
	var datagramAdded, extendedConnectAdded bool
	for _, id := range f.Order {
		val, ok := f.Other[id]
		if !ok {
			continue // skip unknown settings
		}
		if id == SettingsH3Datagram {
			datagramAdded = true
		}
		if id == settingExtendedConnect {
			extendedConnectAdded = true
		}
		if id == SettingsGREASE && val == 0 {
			// generate a GREASE value
			key := 0x1f*uint64(rand.Int32()) + 0x21
			val = rand.Uint64() % (1 << 32)
			b = quicvarint.Append(b, key) // GREASE value, RFC 9114
			b = quicvarint.Append(b, val)
			continue // GREASE values are not added to the Other map
		}

		b = quicvarint.Append(b, id)
		b = quicvarint.Append(b, val)
	}

	if f.Datagram && !datagramAdded {
		b = quicvarint.Append(b, SettingsH3Datagram)
		b = quicvarint.Append(b, 1)
	}
	if f.ExtendedConnect && !extendedConnectAdded {
		b = quicvarint.Append(b, settingExtendedConnect)
		b = quicvarint.Append(b, 1)
	}

	return b
}

type goAwayFrame struct {
	StreamID quic.StreamID
}

func parseGoAwayFrame(r *countingByteReader, l uint64, streamID quic.StreamID, qlogger qlogwriter.Recorder) (*goAwayFrame, error) {
	frame := &goAwayFrame{}
	startLen := r.NumRead
	id, err := quicvarint.Read(r)
	if err != nil {
		return nil, err
	}
	if r.NumRead-startLen != int(l) {
		return nil, errors.New("GOAWAY frame: inconsistent length")
	}
	frame.StreamID = quic.StreamID(id)
	if qlogger != nil {
		qlogger.RecordEvent(qlog.FrameParsed{
			StreamID: streamID,
			Raw:      qlog.RawInfo{Length: r.NumRead, PayloadLength: int(l)},
			Frame:    qlog.Frame{Frame: qlog.GoAwayFrame{StreamID: frame.StreamID}},
		})
	}
	return frame, nil
}

func (f *goAwayFrame) Append(b []byte) []byte {
	b = quicvarint.Append(b, 0x7)
	b = quicvarint.Append(b, uint64(quicvarint.Len(uint64(f.StreamID))))
	return quicvarint.Append(b, uint64(f.StreamID))
}
