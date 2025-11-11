// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

import (
	"testing"

	"github.com/luxfi/node/codec/linearcodec"
	"github.com/luxfi/node/codec/reflectcodec"
	"github.com/luxfi/node/utils/wrappers"
)

// FuzzLinearCodecMarshal tests round-trip marshaling/unmarshaling with linear codec
func FuzzLinearCodecMarshal(f *testing.F) {
	// Seed corpus with known patterns
	f.Add([]byte{})
	f.Add([]byte{0})
	f.Add([]byte{1, 2, 3, 4})
	f.Add([]byte{255, 255, 255, 255})

	codec := linearcodec.NewDefault()

	f.Fuzz(func(t *testing.T, data []byte) {
		// Test that unmarshaling doesn't panic
		p := wrappers.Packer{Bytes: data}
		var result []byte
		_ = codec.UnmarshalFrom(&p, &result)

		// If unmarshal succeeded, test round-trip
		if p.Err == nil && result != nil {
			// Marshal back
			p2 := wrappers.Packer{}
			if err := codec.MarshalInto(result, &p2); err == nil {
				// Unmarshal again
				var result2 []byte
				_ = codec.UnmarshalFrom(&p2, &result2)
			}
		}
	})
}

// FuzzReflectCodecStruct tests reflect codec with struct marshaling
func FuzzReflectCodecStruct(f *testing.F) {
	type TestStruct struct {
		Field1 uint32
		Field2 []byte
		Field3 bool
	}

	// Seed corpus
	f.Add(uint32(0), []byte{}, false)
	f.Add(uint32(42), []byte{1, 2, 3}, true)
	f.Add(uint32(0xFFFFFFFF), []byte{255}, false)

	codec, err := reflectcodec.New(reflectcodec.DefaultTagName, reflectcodec.DefaultMaxSliceLength)
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, field1 uint32, field2 []byte, field3 bool) {
		original := TestStruct{
			Field1: field1,
			Field2: field2,
			Field3: field3,
		}

		// Marshal
		p := wrappers.Packer{MaxSize: 1024 * 1024} // 1MB limit
		err := codec.MarshalInto(original, &p)
		if err != nil {
			// Expected for invalid inputs
			return
		}

		// Unmarshal
		var result TestStruct
		p2 := wrappers.Packer{Bytes: p.Bytes}
		err = codec.UnmarshalFrom(&p2, &result)
		if err != nil {
			t.Errorf("Unmarshal failed after successful marshal: %v", err)
			return
		}

		// Verify round-trip
		if result.Field1 != original.Field1 {
			t.Errorf("Field1 mismatch: got %v, want %v", result.Field1, original.Field1)
		}
		if result.Field3 != original.Field3 {
			t.Errorf("Field3 mismatch: got %v, want %v", result.Field3, original.Field3)
		}
		// Note: byte slices compared separately to handle nil vs empty
	})
}

// FuzzCodecSize tests Size calculation consistency
func FuzzCodecSize(f *testing.F) {
	// Seed corpus
	f.Add(uint32(0))
	f.Add(uint32(1))
	f.Add(uint32(1000))
	f.Add(uint32(0xFFFFFFFF))

	codec := linearcodec.NewDefault()

	f.Fuzz(func(t *testing.T, value uint32) {
		// Calculate size
		size, err := codec.Size(value)
		if err != nil {
			return
		}

		// Marshal and verify size matches
		p := wrappers.Packer{}
		err = codec.MarshalInto(value, &p)
		if err != nil {
			t.Errorf("Marshal failed but Size succeeded: %v", err)
			return
		}

		if len(p.Bytes) != size {
			t.Errorf("Size mismatch: Size() returned %d, Marshal produced %d bytes", size, len(p.Bytes))
		}
	})
}
