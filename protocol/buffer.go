// Package protocol decodes the bit-packed and versioned serialization formats of
// Heroes of the Storm replay files using Blizzard's per-build type tables.
// It is a port of heroprotocol (https://github.com/Blizzard/heroprotocol).
package protocol

import (
	"errors"
	"fmt"
)

// ErrTruncated means the input ended in the middle of a value. Live replay files that the game
// is still writing end this way; everything decoded before the error is valid.
var ErrTruncated = errors.New("protocol: truncated input")

// ErrCorrupted means the input does not match the protocol tables.
var ErrCorrupted = errors.New("protocol: corrupted input")

// Buffer reads bits from a byte slice, big-endian by default (little-endian only for attributes).
type Buffer struct {
	data      []byte
	used      int
	next      uint32
	nextBits  int
	bigEndian bool
}

func NewBuffer(data []byte, bigEndian bool) *Buffer {
	return &Buffer{data: data, bigEndian: bigEndian}
}

func (b *Buffer) Done() bool    { return b.nextBits == 0 && b.used >= len(b.data) }
func (b *Buffer) UsedBits() int { return b.used*8 - b.nextBits }
func (b *Buffer) ByteAlign()    { b.nextBits = 0 }
func (b *Buffer) String() string {
	return fmt.Sprintf("buffer(byte %d of %d, %d bits pending)", b.used, len(b.data), b.nextBits)
}

func (b *Buffer) ReadAlignedBytes(n int) ([]byte, error) {
	b.ByteAlign()
	if n < 0 || b.used+n > len(b.data) {
		return nil, fmt.Errorf("%w at %s (want %d bytes)", ErrTruncated, b, n)
	}
	out := b.data[b.used : b.used+n]
	b.used += n
	return out, nil
}

// ReadBits reads up to 64 bits.
func (b *Buffer) ReadBits(bits int) (uint64, error) {
	var result uint64
	resultBits := 0
	for resultBits != bits {
		if b.nextBits == 0 {
			if b.Done() {
				return 0, fmt.Errorf("%w at %s (want %d bits)", ErrTruncated, b, bits)
			}
			b.next = uint32(b.data[b.used])
			b.used++
			b.nextBits = 8
		}
		copyBits := bits - resultBits
		if copyBits > b.nextBits {
			copyBits = b.nextBits
		}
		cp := uint64(b.next & ((1 << uint(copyBits)) - 1))
		if b.bigEndian {
			result |= cp << uint(bits-resultBits-copyBits)
		} else {
			result |= cp << uint(resultBits)
		}
		b.next >>= uint(copyBits)
		b.nextBits -= copyBits
		resultBits += copyBits
	}
	return result, nil
}

func (b *Buffer) ReadUnalignedBytes(n int) ([]byte, error) {
	out := make([]byte, n)
	for i := range out {
		v, err := b.ReadBits(8)
		if err != nil {
			return nil, err
		}
		out[i] = byte(v)
	}
	return out, nil
}
