package protocol

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Decoder decodes typed values from a Buffer using a type table.
type Decoder interface {
	Instance(typeID int) (any, error)
	Done() bool
	UsedBits() int
	ByteAlign()
}

// --- bit-packed (game events, message events, init data) ---

type bitPacked struct {
	buf   *Buffer
	types []TypeInfo
}

// NewBitPacked returns a decoder for the bit-packed format.
func NewBitPacked(data []byte, types []TypeInfo) Decoder {
	return &bitPacked{buf: NewBuffer(data, true), types: types}
}

func (d *bitPacked) Done() bool    { return d.buf.Done() }
func (d *bitPacked) UsedBits() int { return d.buf.UsedBits() }
func (d *bitPacked) ByteAlign()    { d.buf.ByteAlign() }

func (d *bitPacked) Instance(typeID int) (any, error) {
	if typeID < 0 || typeID >= len(d.types) {
		return nil, fmt.Errorf("%w: typeid %d out of range at %s", ErrCorrupted, typeID, d.buf)
	}
	t := &d.types[typeID]
	switch t.Kind {
	case KindInt:
		return d.int(t.Lo, t.Bits)
	case KindBool:
		v, err := d.int(0, 1)
		return v != 0, err
	case KindArray:
		n, err := d.int(t.Lo, t.Bits)
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, n)
		for i := int64(0); i < n; i++ {
			v, err := d.Instance(t.TypeID)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case KindBitArray:
		n, err := d.int(t.Lo, t.Bits)
		if err != nil {
			return nil, err
		}
		v, err := d.buf.ReadBits(int(n))
		return BitArray{Bits: int(n), Value: v}, err
	case KindBlob:
		n, err := d.int(t.Lo, t.Bits)
		if err != nil {
			return nil, err
		}
		return d.buf.ReadAlignedBytes(int(n))
	case KindChoice:
		tag, err := d.int(t.Lo, t.Bits)
		if err != nil {
			return nil, err
		}
		f, ok := t.Choices[int(tag)]
		if !ok {
			return nil, fmt.Errorf("%w: choice tag %d at %s", ErrCorrupted, tag, d.buf)
		}
		v, err := d.Instance(f.TypeID)
		return Struct{f.Name: v}, err
	case KindFourCC:
		v, err := d.buf.ReadBits(32)
		if err != nil {
			return nil, err
		}
		out := make([]byte, 4)
		binary.BigEndian.PutUint32(out, uint32(v))
		return out, nil
	case KindNull:
		return nil, nil
	case KindOptional:
		exists, err := d.int(0, 1)
		if err != nil || exists == 0 {
			return nil, err
		}
		return d.Instance(t.TypeID)
	case KindReal32:
		b, err := d.buf.ReadUnalignedBytes(4)
		if err != nil {
			return nil, err
		}
		return math.Float32frombits(binary.BigEndian.Uint32(b)), nil
	case KindReal64:
		b, err := d.buf.ReadUnalignedBytes(8)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(binary.BigEndian.Uint64(b)), nil
	case KindStruct:
		return decodeStructFields(d, t.Fields, func(f Field) (any, bool, error) {
			v, err := d.Instance(f.TypeID)
			return v, true, err
		})
	}
	return nil, fmt.Errorf("%w: unknown kind %d", ErrCorrupted, t.Kind)
}

func (d *bitPacked) int(lo int64, bits int) (int64, error) {
	v, err := d.buf.ReadBits(bits)
	return lo + int64(v), err
}

// decodeStructFields implements heroprotocol's __parent flattening: a parent struct's fields are
// merged into the result; a struct whose only field is __parent with a non-struct parent becomes
// that value.
func decodeStructFields(_ Decoder, fields []Field, read func(Field) (any, bool, error)) (any, error) {
	result := Struct{}
	for _, f := range fields {
		v, present, err := read(f)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		if f.Name == "__parent" {
			if parent, ok := v.(Struct); ok {
				for k, pv := range parent {
					result[k] = pv
				}
			} else if len(fields) == 1 {
				return v, nil
			} else {
				result[f.Name] = v
			}
		} else {
			result[f.Name] = v
		}
	}
	return result, nil
}

// --- versioned (tracker events, header, details) ---

type versioned struct {
	buf   *Buffer
	types []TypeInfo
}

// NewVersioned returns a decoder for the versioned (tagged) format.
func NewVersioned(data []byte, types []TypeInfo) Decoder {
	return &versioned{buf: NewBuffer(data, true), types: types}
}

func (d *versioned) Done() bool    { return d.buf.Done() }
func (d *versioned) UsedBits() int { return d.buf.UsedBits() }
func (d *versioned) ByteAlign()    { d.buf.ByteAlign() }

func (d *versioned) expect(tag uint64) error {
	v, err := d.buf.ReadBits(8)
	if err != nil {
		return err
	}
	if v != tag {
		return fmt.Errorf("%w: expected tag %d, got %d at %s", ErrCorrupted, tag, v, d.buf)
	}
	return nil
}

func (d *versioned) vint() (int64, error) {
	b, err := d.buf.ReadBits(8)
	if err != nil {
		return 0, err
	}
	negative := b&1 != 0
	result := int64((b >> 1) & 0x3F)
	bits := uint(6)
	for b&0x80 != 0 {
		b, err = d.buf.ReadBits(8)
		if err != nil {
			return 0, err
		}
		result |= int64(b&0x7F) << bits
		bits += 7
	}
	if negative {
		return -result, nil
	}
	return result, nil
}

func (d *versioned) Instance(typeID int) (any, error) {
	if typeID < 0 || typeID >= len(d.types) {
		return nil, fmt.Errorf("%w: typeid %d out of range at %s", ErrCorrupted, typeID, d.buf)
	}
	t := &d.types[typeID]
	switch t.Kind {
	case KindInt:
		if err := d.expect(9); err != nil {
			return nil, err
		}
		return d.vint()
	case KindBool:
		if err := d.expect(6); err != nil {
			return nil, err
		}
		v, err := d.buf.ReadBits(8)
		return v != 0, err
	case KindArray:
		if err := d.expect(0); err != nil {
			return nil, err
		}
		n, err := d.vint()
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, n)
		for i := int64(0); i < n; i++ {
			v, err := d.Instance(t.TypeID)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case KindBitArray:
		if err := d.expect(1); err != nil {
			return nil, err
		}
		n, err := d.vint()
		if err != nil {
			return nil, err
		}
		b, err := d.buf.ReadAlignedBytes(int((n + 7) / 8))
		return BitArray{Bits: int(n), Bytes: b}, err
	case KindBlob:
		if err := d.expect(2); err != nil {
			return nil, err
		}
		n, err := d.vint()
		if err != nil {
			return nil, err
		}
		return d.buf.ReadAlignedBytes(int(n))
	case KindChoice:
		if err := d.expect(3); err != nil {
			return nil, err
		}
		tag, err := d.vint()
		if err != nil {
			return nil, err
		}
		f, ok := t.Choices[int(tag)]
		if !ok {
			return Struct{}, d.skip()
		}
		v, err := d.Instance(f.TypeID)
		return Struct{f.Name: v}, err
	case KindFourCC:
		if err := d.expect(7); err != nil {
			return nil, err
		}
		return d.buf.ReadAlignedBytes(4)
	case KindNull:
		return nil, nil
	case KindOptional:
		if err := d.expect(4); err != nil {
			return nil, err
		}
		exists, err := d.buf.ReadBits(8)
		if err != nil || exists == 0 {
			return nil, err
		}
		return d.Instance(t.TypeID)
	case KindReal32:
		if err := d.expect(7); err != nil {
			return nil, err
		}
		b, err := d.buf.ReadAlignedBytes(4)
		if err != nil {
			return nil, err
		}
		return math.Float32frombits(binary.BigEndian.Uint32(b)), nil
	case KindReal64:
		if err := d.expect(8); err != nil {
			return nil, err
		}
		b, err := d.buf.ReadAlignedBytes(8)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(binary.BigEndian.Uint64(b)), nil
	case KindStruct:
		if err := d.expect(5); err != nil {
			return nil, err
		}
		n, err := d.vint()
		if err != nil {
			return nil, err
		}
		// Fields arrive tagged, in file order; unknown tags are skipped.
		present := make([]any, len(t.Fields))
		has := make([]bool, len(t.Fields))
		for i := int64(0); i < n; i++ {
			tag, err := d.vint()
			if err != nil {
				return nil, err
			}
			idx := -1
			for j := range t.Fields {
				if t.Fields[j].Tag == int(tag) {
					idx = j
					break
				}
			}
			if idx < 0 {
				if err := d.skip(); err != nil {
					return nil, err
				}
				continue
			}
			v, err := d.Instance(t.Fields[idx].TypeID)
			if err != nil {
				return nil, err
			}
			present[idx], has[idx] = v, true
		}
		return decodeStructFields(d, t.Fields, func(f Field) (any, bool, error) {
			for j := range t.Fields {
				if t.Fields[j].Name == f.Name && t.Fields[j].Tag == f.Tag {
					return present[j], has[j], nil
				}
			}
			return nil, false, nil
		})
	}
	return nil, fmt.Errorf("%w: unknown kind %d", ErrCorrupted, t.Kind)
}

func (d *versioned) skip() error {
	tag, err := d.buf.ReadBits(8)
	if err != nil {
		return err
	}
	switch tag {
	case 0: // array
		n, err := d.vint()
		if err != nil {
			return err
		}
		for i := int64(0); i < n; i++ {
			if err := d.skip(); err != nil {
				return err
			}
		}
	case 1: // bitblob
		n, err := d.vint()
		if err != nil {
			return err
		}
		_, err = d.buf.ReadAlignedBytes(int((n + 7) / 8))
		return err
	case 2: // blob
		n, err := d.vint()
		if err != nil {
			return err
		}
		_, err = d.buf.ReadAlignedBytes(int(n))
		return err
	case 3: // choice
		if _, err := d.vint(); err != nil {
			return err
		}
		return d.skip()
	case 4: // optional
		exists, err := d.buf.ReadBits(8)
		if err != nil {
			return err
		}
		if exists != 0 {
			return d.skip()
		}
	case 5: // struct
		n, err := d.vint()
		if err != nil {
			return err
		}
		for i := int64(0); i < n; i++ {
			if _, err := d.vint(); err != nil {
				return err
			}
			if err := d.skip(); err != nil {
				return err
			}
		}
	case 6:
		_, err = d.buf.ReadAlignedBytes(1)
		return err
	case 7:
		_, err = d.buf.ReadAlignedBytes(4)
		return err
	case 8:
		_, err = d.buf.ReadAlignedBytes(8)
		return err
	case 9:
		_, err = d.vint()
		return err
	default:
		return fmt.Errorf("%w: unknown skip tag %d at %s", ErrCorrupted, tag, d.buf)
	}
	return nil
}
