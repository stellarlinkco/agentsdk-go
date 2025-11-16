package wal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

const (
	recordMagic   uint32 = 0xA17E57AA
	recordVersion byte   = 1
	crcSize              = 4
	headerSize           = 4 + 1 + 2 + 4 // magic + version + typeLen + dataLen
)

var (
	errPartial = errors.New("wal: partial entry")
	// ErrCorrupt signals on-disk data corruption.
	ErrCorrupt = errors.New("wal: corrupt entry")
)

// Position identifies the absolute order of a record inside the WAL.
type Position int64

// Entry describes a logical record persisted in the WAL.
type Entry struct {
	Type     string
	Data     []byte
	Position Position
}

func (e Entry) encode() ([]byte, error) {
	if len(e.Type) == 0 {
		return nil, fmt.Errorf("wal: entry type required")
	}
	if len(e.Type) > 0xffff {
		return nil, fmt.Errorf("wal: entry type exceeds 64K")
	}
	if len(e.Data) > int(^uint32(0)) {
		return nil, fmt.Errorf("wal: entry payload too large")
	}

	typeLen := len(e.Type)
	dataLen := len(e.Data)
	total := headerSize + typeLen + dataLen + crcSize

	buf := make([]byte, total)
	binary.BigEndian.PutUint32(buf[0:4], recordMagic)
	buf[4] = recordVersion
	binary.BigEndian.PutUint16(buf[5:7], uint16(typeLen))
	binary.BigEndian.PutUint32(buf[7:11], uint32(dataLen))

	copy(buf[headerSize:headerSize+typeLen], e.Type)
	copy(buf[headerSize+typeLen:headerSize+typeLen+dataLen], e.Data)

	checksum := crc32.NewIEEE()
	checksum.Write(buf[4 : total-crcSize])
	binary.BigEndian.PutUint32(buf[total-crcSize:], checksum.Sum32())
	return buf, nil
}

func decodeEntry(r io.Reader) (Entry, int64, error) {
	header := make([]byte, headerSize)
	n, err := io.ReadFull(r, header)
	if err != nil {
		if errors.Is(err, io.EOF) && n == 0 {
			return Entry{}, 0, io.EOF
		}
		if errors.Is(err, io.ErrUnexpectedEOF) || (errors.Is(err, io.EOF) && n > 0) {
			return Entry{}, int64(n), errPartial
		}
		return Entry{}, int64(n), err
	}
	if binary.BigEndian.Uint32(header[0:4]) != recordMagic {
		return Entry{}, int64(n), ErrCorrupt
	}
	if header[4] != recordVersion {
		return Entry{}, int64(n), ErrCorrupt
	}

	typeLen := int(binary.BigEndian.Uint16(header[5:7]))
	dataLen := int(binary.BigEndian.Uint32(header[7:11]))
	if typeLen < 0 || dataLen < 0 {
		return Entry{}, int64(n), ErrCorrupt
	}

	payload := make([]byte, typeLen+dataLen+crcSize)
	read, err := io.ReadFull(r, payload)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return Entry{}, int64(n + read), errPartial
		}
		return Entry{}, int64(n + read), err
	}

	checksum := crc32.NewIEEE()
	checksum.Write(header[4:])
	checksum.Write(payload[:typeLen+dataLen])
	expected := binary.BigEndian.Uint32(payload[typeLen+dataLen:])
	if checksum.Sum32() != expected {
		return Entry{}, int64(n + read), ErrCorrupt
	}

	var entry Entry
	entry.Type = string(payload[:typeLen])
	if dataLen > 0 {
		entry.Data = make([]byte, dataLen)
		copy(entry.Data, payload[typeLen:typeLen+dataLen])
	}
	return entry, int64(n + read), nil
}
