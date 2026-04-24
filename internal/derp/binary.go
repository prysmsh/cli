package derp

import (
	"encoding/binary"
	"errors"
)

// Binary frame types for high-performance WireGuard packet relay.
// Binary frames avoid JSON+base64 overhead (~33% size inflation + CPU).
const (
	BinaryFrameWGPacket byte = 0x01
)

// EncodeBinaryWGPacket builds a binary WebSocket frame for a WireGuard packet.
// Format: [type=0x01][2-byte from_len BE][from][2-byte to_len BE][to][payload]
func EncodeBinaryWGPacket(from, to string, payload []byte) []byte {
	fromB := []byte(from)
	toB := []byte(to)
	buf := make([]byte, 1+2+len(fromB)+2+len(toB)+len(payload))
	buf[0] = BinaryFrameWGPacket
	binary.BigEndian.PutUint16(buf[1:3], uint16(len(fromB)))
	copy(buf[3:], fromB)
	off := 3 + len(fromB)
	binary.BigEndian.PutUint16(buf[off:off+2], uint16(len(toB)))
	copy(buf[off+2:], toB)
	off += 2 + len(toB)
	copy(buf[off:], payload)
	return buf
}

// DecodeBinaryWGPacket parses a binary WireGuard frame.
// Returns from, to, payload, error.
func DecodeBinaryWGPacket(data []byte) (from, to string, payload []byte, err error) {
	if len(data) < 5 { // 1 + 2 + 2 minimum
		return "", "", nil, errors.New("binary frame too short")
	}
	if data[0] != BinaryFrameWGPacket {
		return "", "", nil, errors.New("unknown binary frame type")
	}
	fromLen := int(binary.BigEndian.Uint16(data[1:3]))
	if len(data) < 3+fromLen+2 {
		return "", "", nil, errors.New("binary frame truncated at from")
	}
	from = string(data[3 : 3+fromLen])
	off := 3 + fromLen
	toLen := int(binary.BigEndian.Uint16(data[off : off+2]))
	off += 2
	if len(data) < off+toLen {
		return "", "", nil, errors.New("binary frame truncated at to")
	}
	to = string(data[off : off+toLen])
	off += toLen
	payload = data[off:]
	return from, to, payload, nil
}
