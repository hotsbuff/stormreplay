package protocol

// Decoded values use these Go types:
//
//	int        -> int64
//	bool       -> bool
//	blob       -> []byte
//	fourcc     -> []byte (4 bytes)
//	bitarray   -> BitArray
//	real32     -> float32
//	real64     -> float64
//	null       -> nil
//	optional   -> inner value or nil
//	array      -> []any
//	struct     -> Struct (or the parent's value when the struct has only a __parent field)
//	choice     -> Struct with a single key

// Struct is a decoded struct or choice.
type Struct map[string]any

// BitArray is a decoded bit array. The bit-packed format yields Value (Bits <= 64); the versioned
// format yields Bytes ((Bits+7)/8 bytes).
type BitArray struct {
	Bits  int
	Value uint64
	Bytes []byte
}

// Event is a decoded replay event with the bookkeeping keys heroprotocol adds:
// _event (name), _eventid, _gameloop, _userid (Struct{m_userId}) for game/message events, _bits.
type Event = Struct

// Int returns a nested int field, or 0 if missing or not an int.
func (s Struct) Int(key string) int64 {
	v, _ := s[key].(int64)
	return v
}

// Str returns a nested blob field as string.
func (s Struct) Str(key string) string {
	switch v := s[key].(type) {
	case []byte:
		return string(v)
	case string:
		return v
	}
	return ""
}

// Sub returns a nested struct.
func (s Struct) Sub(key string) Struct {
	v, _ := s[key].(Struct)
	return v
}

// Name returns the event name (_event).
func (s Struct) Name() string { return s.Str("_event") }

// GameLoop returns the event time in game loops (16 per second).
func (s Struct) GameLoop() int64 { return s.Int("_gameloop") }
