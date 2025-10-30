// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"bytes"
	"fmt"
	"math"

	"github.com/klauspost/compress/zstd"
)

var _ Compressor = (*zstdCompressor)(nil)

func NewZstdCompressor(maxSize int64) (Compressor, error) {
	return NewZstdCompressorWithLevel(maxSize, zstd.DefaultCompression)
}

func NewZstdCompressorWithLevel(maxSize int64, level int) (Compressor, error) {
	if maxSize == math.MaxInt64 {
		// "Decompress" creates "io.LimitReader" with max size + 1:
		// if the max size + 1 overflows, "io.LimitReader" reads nothing
		// returning 0 byte for the decompress call
		// require max size < math.MaxInt64 to prevent int64 overflows
		return nil, ErrInvalidMaxSizeCompressor
	}
	return &zstdCompressor{
		maxSize: maxSize,
		level:   level,
	}, nil
}

type zstdCompressor struct {
	maxSize int64
	level   int
}

func (z *zstdCompressor) Compress(msg []byte) ([]byte, error) {
	if int64(len(msg)) > z.maxSize {
		return nil, fmt.Errorf("%w: (%d) > (%d)", ErrMsgTooLarge, len(msg), z.maxSize)
	}
	return zstd.CompressLevel(nil, msg, z.level)
}

func (z *zstdCompressor) Decompress(msg []byte) ([]byte, error) {
	decompressed, err := z.decoder.DecodeAll(msg, nil)
	if err != nil {
		// If the decoder returns an error about size limit, wrap it with our error
		if err.Error() == "decompressed size exceeds configured limit" {
			return nil, fmt.Errorf("%w: decompression stopped due to size limit", ErrDecompressedMsgTooLarge)
		}
		return nil, err
	}
	if int64(len(decompressed)) > z.maxSize {
		return nil, fmt.Errorf("%w: (%d) > (%d)", ErrDecompressedMsgTooLarge, len(decompressed), z.maxSize)
	}
	return decompressed, nil
}
