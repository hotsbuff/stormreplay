// Package mpq reads MPQ archives as used by Heroes of the Storm replays and saves
// (.StormReplay, .StormSave): user data header, hash/block tables, sector-based
// files compressed with zlib or bzip2. Encrypted files and other compressions are
// not supported — replays do not use them. The behaviour mirrors mpyq.
package mpq

import (
	"bytes"
	"compress/bzip2"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	userDataMagic = "MPQ\x1b"
	headerMagic   = "MPQ\x1a"

	flagImplode    = 0x00000100
	flagCompress   = 0x00000200
	flagEncrypted  = 0x00010000
	flagSingleUnit = 0x01000000
	flagSectorCRC  = 0x04000000
	flagExists     = 0x80000000

	hashTableOffset = 0
	hashA           = 1
	hashB           = 2
	hashTable       = 3
)

var (
	ErrNotMPQ      = errors.New("mpq: not an MPQ archive")
	ErrNotFound    = errors.New("mpq: file not found")
	ErrUnsupported = errors.New("mpq: unsupported feature")
)

type hashEntry struct {
	hashA, hashB     uint32
	locale, platform uint16
	blockIndex       uint32
}

type blockEntry struct {
	offset, archivedSize, size, flags uint32
}

// Archive is a parsed MPQ archive held in memory.
type Archive struct {
	data            []byte
	userData        []byte
	headerOffset    int
	sectorSizeShift int
	hashes          []hashEntry
	blocks          []blockEntry
	files           []string
}

// Open reads and parses an archive file.
func Open(path string) (*Archive, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse parses an archive from memory. The slice is retained.
func Parse(data []byte) (*Archive, error) {
	if len(data) < 16 || string(data[:4]) != userDataMagic {
		return nil, ErrNotMPQ
	}
	a := &Archive{data: data}
	le := binary.LittleEndian
	// user data header: magic, user_data_size, mpq_header_offset, user_data_header_size
	a.headerOffset = int(le.Uint32(data[8:12]))
	udSize := int(le.Uint32(data[12:16]))
	if 16+udSize > len(data) || a.headerOffset+32 > len(data) {
		return nil, fmt.Errorf("mpq: truncated user data header")
	}
	a.userData = data[16 : 16+udSize]
	h := data[a.headerOffset:]
	if string(h[:4]) != headerMagic {
		return nil, fmt.Errorf("mpq: bad archive header magic %q", h[:4])
	}
	formatVersion := le.Uint16(h[12:14])
	a.sectorSizeShift = int(le.Uint16(h[14:16]))
	hashOff := int(le.Uint32(h[16:20]))
	blockOff := int(le.Uint32(h[20:24]))
	hashN := int(le.Uint32(h[24:28]))
	blockN := int(le.Uint32(h[28:32]))
	if formatVersion >= 1 && len(h) >= 44 {
		// extended block table offset and high offset bits: replays keep them zero
		hashOff |= int(le.Uint16(h[40:42])) << 32
		blockOff |= int(le.Uint16(h[42:44])) << 32
	}
	hashes, err := a.readTable(hashOff, hashN, "(hash table)")
	if err != nil {
		return nil, fmt.Errorf("mpq: hash table: %w", err)
	}
	blocks, err := a.readTable(blockOff, blockN, "(block table)")
	if err != nil {
		return nil, fmt.Errorf("mpq: block table: %w", err)
	}
	a.hashes = make([]hashEntry, hashN)
	for i := range a.hashes {
		e := hashes[i*16 : i*16+16]
		a.hashes[i] = hashEntry{le.Uint32(e[0:4]), le.Uint32(e[4:8]), le.Uint16(e[8:10]), le.Uint16(e[10:12]), le.Uint32(e[12:16])}
	}
	a.blocks = make([]blockEntry, blockN)
	for i := range a.blocks {
		e := blocks[i*16 : i*16+16]
		a.blocks[i] = blockEntry{le.Uint32(e[0:4]), le.Uint32(e[4:8]), le.Uint32(e[8:12]), le.Uint32(e[12:16])}
	}
	if lf, err := a.ReadFile("(listfile)"); err == nil {
		for _, name := range strings.Split(string(lf), "\r\n") {
			if name = strings.TrimSpace(name); name != "" {
				a.files = append(a.files, name)
			}
		}
	}
	return a, nil
}

func (a *Archive) readTable(off, n int, keyName string) ([]byte, error) {
	start := a.headerOffset + off
	end := start + n*16
	if start < 0 || end > len(a.data) {
		return nil, fmt.Errorf("table out of bounds (%d..%d of %d)", start, end, len(a.data))
	}
	return decrypt(a.data[start:end], hash(keyName, hashTable)), nil
}

// UserData returns the user data block that precedes the archive header (the replay header).
func (a *Archive) UserData() []byte { return a.userData }

// Files lists the names from "(listfile)", if present.
func (a *Archive) Files() []string { return a.files }

// Has reports whether the archive contains the named file.
func (a *Archive) Has(name string) bool { return a.findHash(name) != nil }

func (a *Archive) findHash(name string) *hashEntry {
	ha, hb := hash(name, hashA), hash(name, hashB)
	for i := range a.hashes {
		if a.hashes[i].hashA == ha && a.hashes[i].hashB == hb {
			return &a.hashes[i]
		}
	}
	return nil
}

// ReadFile extracts and decompresses a file by name.
func (a *Archive) ReadFile(name string) ([]byte, error) {
	he := a.findHash(name)
	if he == nil || int(he.blockIndex) >= len(a.blocks) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	b := a.blocks[he.blockIndex]
	if b.flags&flagExists == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if b.archivedSize == 0 {
		return []byte{}, nil
	}
	start := a.headerOffset + int(b.offset)
	end := start + int(b.archivedSize)
	if start < 0 || end > len(a.data) {
		return nil, fmt.Errorf("mpq: %s: data out of bounds", name)
	}
	raw := a.data[start:end]
	if b.flags&flagEncrypted != 0 {
		return nil, fmt.Errorf("%w: encrypted file %s", ErrUnsupported, name)
	}
	if b.flags&flagSingleUnit != 0 {
		if b.flags&flagCompress != 0 && b.size > b.archivedSize {
			return decompress(raw)
		}
		return raw, nil
	}
	sectorSize := 512 << uint(a.sectorSizeShift)
	sectors := int(b.size)/sectorSize + 1
	crc := b.flags&flagSectorCRC != 0
	if crc {
		sectors++
	}
	if len(raw) < 4*(sectors+1) {
		return nil, fmt.Errorf("mpq: %s: sector table truncated", name)
	}
	positions := make([]int, sectors+1)
	for i := range positions {
		positions[i] = int(binary.LittleEndian.Uint32(raw[i*4 : i*4+4]))
	}
	n := len(positions) - 1
	if crc {
		n--
	}
	out := make([]byte, 0, b.size)
	left := int(b.size)
	for i := 0; i < n; i++ {
		if positions[i] > positions[i+1] || positions[i+1] > len(raw) {
			return nil, fmt.Errorf("mpq: %s: bad sector offsets", name)
		}
		sector := raw[positions[i]:positions[i+1]]
		if b.flags&flagCompress != 0 && left > len(sector) {
			d, err := decompress(sector)
			if err != nil {
				return nil, fmt.Errorf("mpq: %s: sector %d: %w", name, i, err)
			}
			sector = d
		}
		left -= len(sector)
		out = append(out, sector...)
	}
	return out, nil
}

func decompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}
	switch data[0] {
	case 0:
		return data, nil
	case 2:
		r, err := zlib.NewReader(bytes.NewReader(data[1:]))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	case 16:
		return io.ReadAll(bzip2.NewReader(bytes.NewReader(data[1:])))
	}
	return nil, fmt.Errorf("%w: compression type 0x%02x", ErrUnsupported, data[0])
}

// --- encryption primitives (standard MPQ) ---

var cryptTable [0x500]uint32

func init() {
	seed := uint32(0x00100001)
	for i := 0; i < 0x100; i++ {
		idx := i
		for j := 0; j < 5; j++ {
			seed = (seed*125 + 3) % 0x2AAAAB
			t1 := (seed & 0xFFFF) << 0x10
			seed = (seed*125 + 3) % 0x2AAAAB
			t2 := seed & 0xFFFF
			cryptTable[idx] = t1 | t2
			idx += 0x100
		}
	}
}

func hash(s string, kind int) uint32 {
	seed1, seed2 := uint32(0x7FED7FED), uint32(0xEEEEEEEE)
	for _, ch := range []byte(strings.ToUpper(s)) {
		c := uint32(ch)
		seed1 = cryptTable[(kind<<8)+int(c)] ^ (seed1 + seed2)
		seed2 = c + seed1 + seed2 + (seed2 << 5) + 3
	}
	return seed1
}

func decrypt(data []byte, key uint32) []byte {
	seed1, seed2 := key, uint32(0xEEEEEEEE)
	out := make([]byte, len(data))
	copy(out, data)
	for i := 0; i+4 <= len(out); i += 4 {
		seed2 += cryptTable[0x400+int(seed1&0xFF)]
		v := binary.LittleEndian.Uint32(out[i:i+4]) ^ (seed1 + seed2)
		seed1 = ((^seed1 << 0x15) + 0x11111111) | (seed1 >> 0x0B)
		seed2 = v + seed2 + (seed2 << 5) + 3
		binary.LittleEndian.PutUint32(out[i:i+4], v)
	}
	return out
}
