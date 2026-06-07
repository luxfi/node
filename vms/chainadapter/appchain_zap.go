// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainadapter

// ZAP-native codecs for the SQLiteMaterializer.
//
// Replaces three uses of json/v2:
//   1. CollectionSchema  — stored in _schemas.schema_zap (BLOB).
//   2. Dynamic-table user data (map[string]any) — stored in collection's
//      _data BLOB column.
//   3. Counter operation (Field, Delta) — engine-internal envelope read
//      from the decrypted OpIncrement/OpDecrement payload.
//
// All three use a tiny self-describing wire shape: a kind byte + ZAP
// envelope. Hard fork: state persists in SQLite and existing devnets
// already required re-init for LP-023; ZAP-on-disk lives in the same
// boat.
//
// User data values (the leaves of the map[string]any) are encoded by
// type tag — string/int/float/bool/bytes — so we don't drag a generic
// type registry along. Nested objects/arrays in the user data are NOT
// supported by this codec; if they appear, encoding returns an error.
// The chainadapter API documents this as the supported user-data shape.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/luxfi/zap"
)

const (
	// Schema envelope.
	schemaKind uint8 = 1

	// User-data envelope.
	userDataKind uint8 = 2

	// Counter envelope.
	counterKind uint8 = 3

	// User-data leaf type tags.
	tagNil    uint8 = 0
	tagBool   uint8 = 1
	tagInt64  uint8 = 2
	tagFloat  uint8 = 3
	tagString uint8 = 4
	tagBytes  uint8 = 5
)

// ErrUnsupportedUserDataValue is returned when the user provides a value
// type we don't know how to encode (nested map/slice, typed struct, etc.).
var ErrUnsupportedUserDataValue = errors.New("chainadapter: unsupported user-data value type")

// ErrCorruptZAPBlob is returned when a decoded buffer is internally
// inconsistent (kind byte mismatch, payload too short, unknown tag).
var ErrCorruptZAPBlob = errors.New("chainadapter: corrupt ZAP blob")

// ---------------- CollectionSchema ----------------

// encodeSchemaBlob serializes a CollectionSchema as a ZAP envelope.
//
// Wire shape:
//
//	Kind        u8     @ 0    # = schemaKind
//	CRDTType    u8     @ 1
//	Encrypted   u8     @ 2
//	Domain      u8     @ 3
//	Name        bytes  @ 4
//	PrimaryKey  bytes  @ 12
//	Fields      bytes  @ 20   # flat concatenated FieldSchema records
//	Indexes     bytes  @ 28   # flat concatenated IndexSchema records
//
// Each FieldSchema record: [u8 nameLen][name][u8 typeLen][type][u8 nullable][u8 encrypted]
// Each IndexSchema record: [u8 nameLen][name][u8 fieldCount][u8 unique][...fields].
func encodeSchemaBlob(s *CollectionSchema) []byte {
	b := zap.NewBuilder(256 + 64*len(s.Fields) + 64*len(s.Indexes))

	fieldsBlob := encodeFieldList(s.Fields)
	indexesBlob := encodeIndexList(s.Indexes)

	ob := b.StartObject(36)
	ob.SetUint8(0, schemaKind)
	ob.SetUint8(1, uint8(s.CRDTType))
	if s.Encrypted {
		ob.SetUint8(2, 1)
	}
	ob.SetUint8(3, uint8(s.Domain))
	ob.SetBytes(4, []byte(s.Name))
	ob.SetBytes(12, []byte(s.PrimaryKey))
	ob.SetBytes(20, fieldsBlob)
	ob.SetBytes(28, indexesBlob)
	ob.FinishAsRoot()
	return b.Finish()
}

func encodeFieldList(fields []*FieldSchema) []byte {
	var out []byte
	for _, f := range fields {
		out = appendVarBytes(out, []byte(f.Name))
		out = appendVarBytes(out, []byte(f.Type))
		if f.Nullable {
			out = append(out, 1)
		} else {
			out = append(out, 0)
		}
		if f.Encrypted {
			out = append(out, 1)
		} else {
			out = append(out, 0)
		}
		// Default value is omitted from the persisted schema — it's an
		// engineering hint at table-create time, not a wire invariant.
	}
	return out
}

func encodeIndexList(indexes []*IndexSchema) []byte {
	var out []byte
	for _, idx := range indexes {
		out = appendVarBytes(out, []byte(idx.Name))
		out = append(out, uint8(len(idx.Fields)))
		if idx.Unique {
			out = append(out, 1)
		} else {
			out = append(out, 0)
		}
		for _, f := range idx.Fields {
			out = appendVarBytes(out, []byte(f))
		}
	}
	return out
}

// ---------------- User-data map[string]any ----------------

// encodeUserData serializes a flat map[string]any as a ZAP envelope.
// Returns ErrUnsupportedUserDataValue if any value is not one of the
// supported leaf types.
//
// Wire shape:
//
//	Kind     u8     @ 0    # = userDataKind
//	Entries  bytes  @ 1    # flat concatenated [varbytes key][u8 tag][...value]
func encodeUserData(data map[string]any) ([]byte, error) {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys) // stable wire order

	var entries []byte
	for _, k := range keys {
		entries = appendVarBytes(entries, []byte(k))
		v, err := encodeUserDataValue(data[k])
		if err != nil {
			return nil, err
		}
		entries = append(entries, v...)
	}

	b := zap.NewBuilder(32 + len(entries))
	ob := b.StartObject(9)
	ob.SetUint8(0, userDataKind)
	ob.SetBytes(1, entries)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func encodeUserDataValue(v any) ([]byte, error) {
	switch x := v.(type) {
	case nil:
		return []byte{tagNil}, nil
	case bool:
		if x {
			return []byte{tagBool, 1}, nil
		}
		return []byte{tagBool, 0}, nil
	case int:
		out := []byte{tagInt64, 0, 0, 0, 0, 0, 0, 0, 0}
		binary.LittleEndian.PutUint64(out[1:], uint64(int64(x)))
		return out, nil
	case int32:
		out := []byte{tagInt64, 0, 0, 0, 0, 0, 0, 0, 0}
		binary.LittleEndian.PutUint64(out[1:], uint64(int64(x)))
		return out, nil
	case int64:
		out := []byte{tagInt64, 0, 0, 0, 0, 0, 0, 0, 0}
		binary.LittleEndian.PutUint64(out[1:], uint64(x))
		return out, nil
	case uint64:
		out := []byte{tagInt64, 0, 0, 0, 0, 0, 0, 0, 0}
		binary.LittleEndian.PutUint64(out[1:], x)
		return out, nil
	case float32:
		out := []byte{tagFloat, 0, 0, 0, 0, 0, 0, 0, 0}
		binary.LittleEndian.PutUint64(out[1:], float64bits(float64(x)))
		return out, nil
	case float64:
		out := []byte{tagFloat, 0, 0, 0, 0, 0, 0, 0, 0}
		binary.LittleEndian.PutUint64(out[1:], float64bits(x))
		return out, nil
	case string:
		out := []byte{tagString}
		out = appendVarBytes(out, []byte(x))
		return out, nil
	case []byte:
		out := []byte{tagBytes}
		out = appendVarBytes(out, x)
		return out, nil
	default:
		return nil, fmt.Errorf("%w: type %T", ErrUnsupportedUserDataValue, v)
	}
}

// decodeUserData parses a ZAP envelope back into a map[string]any.
func decodeUserData(blob []byte) (map[string]any, error) {
	msg, err := zap.Parse(blob)
	if err != nil {
		return nil, err
	}
	root := msg.Root()
	if root.Uint8(0) != userDataKind {
		return nil, fmt.Errorf("%w: bad kind", ErrCorruptZAPBlob)
	}
	entries := root.Bytes(1)
	out := make(map[string]any)
	for len(entries) > 0 {
		key, rest, err := readVarBytes(entries)
		if err != nil {
			return nil, err
		}
		entries = rest
		if len(entries) == 0 {
			return nil, fmt.Errorf("%w: trailing key without value", ErrCorruptZAPBlob)
		}
		tag := entries[0]
		entries = entries[1:]
		var val any
		switch tag {
		case tagNil:
			val = nil
		case tagBool:
			if len(entries) < 1 {
				return nil, fmt.Errorf("%w: bool tag", ErrCorruptZAPBlob)
			}
			val = entries[0] == 1
			entries = entries[1:]
		case tagInt64:
			if len(entries) < 8 {
				return nil, fmt.Errorf("%w: int64 tag", ErrCorruptZAPBlob)
			}
			val = int64(binary.LittleEndian.Uint64(entries[:8]))
			entries = entries[8:]
		case tagFloat:
			if len(entries) < 8 {
				return nil, fmt.Errorf("%w: float tag", ErrCorruptZAPBlob)
			}
			val = float64FromBits(binary.LittleEndian.Uint64(entries[:8]))
			entries = entries[8:]
		case tagString:
			s, rest, err := readVarBytes(entries)
			if err != nil {
				return nil, err
			}
			val = string(s)
			entries = rest
		case tagBytes:
			s, rest, err := readVarBytes(entries)
			if err != nil {
				return nil, err
			}
			val = append([]byte(nil), s...)
			entries = rest
		default:
			return nil, fmt.Errorf("%w: unknown tag 0x%02x", ErrCorruptZAPBlob, tag)
		}
		out[string(key)] = val
	}
	return out, nil
}

// ---------------- Counter operation (Field, Delta) ----------------

// CounterOp is the engine-internal counter operation payload.
type CounterOp struct {
	Field string
	Delta int64
}

// encodeCounterOp serializes a CounterOp as a ZAP envelope.
//
// Wire shape:
//
//	Kind   u8     @ 0    # = counterKind
//	Delta  i64    @ 1
//	Field  bytes  @ 9
func encodeCounterOp(op CounterOp) []byte {
	b := zap.NewBuilder(32 + len(op.Field))
	ob := b.StartObject(17)
	ob.SetUint8(0, counterKind)
	ob.SetInt64(1, op.Delta)
	ob.SetBytes(9, []byte(op.Field))
	ob.FinishAsRoot()
	return b.Finish()
}

// decodeCounterOp parses a ZAP envelope into a CounterOp.
func decodeCounterOp(blob []byte) (CounterOp, error) {
	msg, err := zap.Parse(blob)
	if err != nil {
		return CounterOp{}, err
	}
	root := msg.Root()
	if root.Uint8(0) != counterKind {
		return CounterOp{}, fmt.Errorf("%w: bad kind", ErrCorruptZAPBlob)
	}
	return CounterOp{
		Field: string(root.Bytes(9)),
		Delta: root.Int64(1),
	}, nil
}

// ---------------- Helpers ----------------

// appendVarBytes appends a u32 length prefix + data.
func appendVarBytes(buf, data []byte) []byte {
	var lenbuf [4]byte
	binary.LittleEndian.PutUint32(lenbuf[:], uint32(len(data)))
	buf = append(buf, lenbuf[:]...)
	buf = append(buf, data...)
	return buf
}

func readVarBytes(buf []byte) (data, rest []byte, err error) {
	if len(buf) < 4 {
		return nil, nil, fmt.Errorf("%w: varbytes header", ErrCorruptZAPBlob)
	}
	n := binary.LittleEndian.Uint32(buf[:4])
	if uint32(len(buf)) < 4+n {
		return nil, nil, fmt.Errorf("%w: varbytes payload %d < %d", ErrCorruptZAPBlob, len(buf)-4, n)
	}
	return buf[4 : 4+n], buf[4+n:], nil
}

// float64bits / float64FromBits — wrappers; keep math import local.
func float64bits(f float64) uint64       { return math.Float64bits(f) }
func float64FromBits(b uint64) float64   { return math.Float64frombits(b) }
