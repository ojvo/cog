package store

import (
	"encoding/binary"
	"fmt"
	crc32hash "hash/crc32"
	"io"
)

const (
	// WAL record types
	recordTypeSet = 1
	recordTypeDel = 2

	// walHeaderSize: CRC(4) + Seq(8) + Type(1) + KeyLen(2) + ValueLen(4)
	walHeaderSize = 4 + 8 + 1 + 2 + 4
)

// walLogf is the WAL diagnostic logger; silent by default.
var walLogf = func(format string, args ...interface{}) {}

// SetWALLogger overrides the default (silent) WAL diagnostic logger.
func SetWALLogger(fn func(format string, args ...interface{})) {
	if fn != nil {
		walLogf = fn
	}
}

// logRecord is a single WAL log entry.
type logRecord struct {
	Seq   uint64
	Type  byte
	Key   string
	Value []byte
	CRC   uint32
}

// encode serializes the record to binary format.
func (r *logRecord) encode() ([]byte, error) {
	keyLen := len(r.Key)
	valLen := len(r.Value)

	if keyLen > 65535 {
		return nil, fmt.Errorf("key too large: %d", keyLen)
	}

	totalLen := walHeaderSize + keyLen + valLen
	buf := make([]byte, totalLen)

	// Seq (8)
	binary.LittleEndian.PutUint64(buf[4:12], r.Seq)
	// Type (1)
	buf[12] = r.Type
	// KeyLen (2)
	binary.LittleEndian.PutUint16(buf[13:15], uint16(keyLen))
	// ValueLen (4)
	binary.LittleEndian.PutUint32(buf[15:19], uint32(valLen))

	// Key
	copy(buf[19:], r.Key)
	// Value
	copy(buf[19+keyLen:], r.Value)

	// CRC (4) - covers Seq, Type, KeyLen, ValueLen, Key, Value
	crc := crc32hash.ChecksumIEEE(buf[4:])
	binary.LittleEndian.PutUint32(buf[0:4], crc)

	return buf, nil
}

// decodeRecord reads and decodes a single record from r.
func decodeRecord(r io.Reader) (*logRecord, error) {
	header := make([]byte, walHeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	expectedCRC := binary.LittleEndian.Uint32(header[0:4])
	seq := binary.LittleEndian.Uint64(header[4:12])
	typ := header[12]
	keyLen := int(binary.LittleEndian.Uint16(header[13:15]))
	valLen := int(binary.LittleEndian.Uint32(header[15:19]))

	data := make([]byte, keyLen+valLen)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	// Verify CRC
	crcCalc := crc32hash.NewIEEE()
	crcCalc.Write(header[4:])
	crcCalc.Write(data)
	if crcCalc.Sum32() != expectedCRC {
		return nil, fmt.Errorf("crc mismatch")
	}

	return &logRecord{
		Seq:   seq,
		Type:  typ,
		Key:   string(data[:keyLen]),
		Value: data[keyLen:],
		CRC:   expectedCRC,
	}, nil
}
