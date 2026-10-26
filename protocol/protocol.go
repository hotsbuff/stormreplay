package protocol

import (
	"errors"
	"fmt"
	"io"
)

// Protocol is the type table of one game build plus the ids needed to decode each replay file.
type Protocol struct {
	Build             int
	Types             []TypeInfo
	GameEventTypes    map[int]EventType
	MessageEventTypes map[int]EventType
	TrackerEventTypes map[int]EventType

	gameEventID, messageEventID, trackerEventID int
	svarUint32, replayUserID                    int
	replayHeader, gameDetails, replayInitData   int
}

// DecodeHeader decodes the replay header (the MPQ user data). Decode it with Latest(): the header
// type only gains fields, and older tables skip the ones they do not know.
func (p *Protocol) DecodeHeader(data []byte) (Struct, error) {
	v, err := NewVersioned(data, p.Types).Instance(p.replayHeader)
	return asStruct(v, err)
}

// DecodeDetails decodes replay.details.
func (p *Protocol) DecodeDetails(data []byte) (Struct, error) {
	v, err := NewVersioned(data, p.Types).Instance(p.gameDetails)
	return asStruct(v, err)
}

// DecodeInitData decodes replay.initData.
func (p *Protocol) DecodeInitData(data []byte) (Struct, error) {
	if p.replayInitData < 0 {
		return nil, fmt.Errorf("protocol %d: no initdata type", p.Build)
	}
	v, err := NewBitPacked(data, p.Types).Instance(p.replayInitData)
	return asStruct(v, err)
}

func asStruct(v any, err error) (Struct, error) {
	if err != nil {
		return nil, err
	}
	s, ok := v.(Struct)
	if !ok {
		return nil, fmt.Errorf("%w: expected struct, got %T", ErrCorrupted, v)
	}
	return s, nil
}

// Attribute is one entry of replay.attributes.events.
type Attribute struct {
	Namespace int64  `json:"namespace"`
	AttrID    int64  `json:"attrid"`
	Value     []byte `json:"value"` // 4 bytes, reversed and stripped of NULs, as heroprotocol does
}

// Attributes is the decoded replay.attributes.events.
type Attributes struct {
	Source       int64
	MapNamespace int64
	Scopes       map[int64]map[int64][]Attribute // scope (player slot or 16 for global) -> attrid -> values
}

// DecodeAttributes decodes replay.attributes.events (a fixed little-endian layout, no type table).
func (p *Protocol) DecodeAttributes(data []byte) (*Attributes, error) {
	b := NewBuffer(data, false)
	out := &Attributes{Scopes: map[int64]map[int64][]Attribute{}}
	if b.Done() {
		return out, nil
	}
	var err error
	rd := func(bits int) int64 {
		if err != nil {
			return 0
		}
		var v uint64
		v, err = b.ReadBits(bits)
		return int64(v)
	}
	out.Source = rd(8)
	out.MapNamespace = rd(32)
	_ = rd(32) // count
	if err != nil {
		return nil, err
	}
	for !b.Done() {
		a := Attribute{Namespace: rd(32), AttrID: rd(32)}
		scope := rd(8)
		if err != nil {
			return nil, err
		}
		raw, rerr := b.ReadAlignedBytes(4)
		if rerr != nil {
			return nil, rerr
		}
		val := make([]byte, 0, 4)
		for i := 3; i >= 0; i-- {
			val = append(val, raw[i])
		}
		// strip NULs on both ends, like bytes.strip(b'\x00')
		s, e := 0, len(val)
		for s < e && val[s] == 0 {
			s++
		}
		for e > s && val[e-1] == 0 {
			e--
		}
		a.Value = val[s:e]
		if out.Scopes[scope] == nil {
			out.Scopes[scope] = map[int64][]Attribute{}
		}
		out.Scopes[scope][a.AttrID] = append(out.Scopes[scope][a.AttrID], a)
	}
	return out, nil
}

// EventReader decodes an event stream one event at a time.
type EventReader struct {
	p        *Protocol
	dec      Decoder
	types    map[int]EventType
	eventID  int
	userID   bool
	gameloop int64
	err      error
}

// Next returns the next event, io.EOF at the end, or ErrTruncated/ErrCorrupted (wrapped) on failure.
// After an error Next keeps returning it.
func (r *EventReader) Next() (Event, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.dec.Done() {
		r.err = io.EOF
		return nil, r.err
	}
	ev, err := r.next()
	if err != nil {
		r.err = err
		return nil, err
	}
	return ev, nil
}

func (r *EventReader) next() (Event, error) {
	start := r.dec.UsedBits()
	delta, err := r.dec.Instance(r.p.svarUint32)
	if err != nil {
		return nil, err
	}
	if s, ok := delta.(Struct); ok {
		for _, v := range s {
			if n, ok := v.(int64); ok {
				r.gameloop += n
			}
		}
	}
	var userID any
	if r.userID {
		if userID, err = r.dec.Instance(r.p.replayUserID); err != nil {
			return nil, err
		}
	}
	idV, err := r.dec.Instance(r.eventID)
	if err != nil {
		return nil, err
	}
	id, _ := idV.(int64)
	et, ok := r.types[int(id)]
	if !ok {
		return nil, fmt.Errorf("%w: unknown event id %d at gameloop %d", ErrCorrupted, id, r.gameloop)
	}
	v, err := r.dec.Instance(et.TypeID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", et.Name, err)
	}
	ev, ok := v.(Struct)
	if !ok {
		ev = Struct{"value": v}
	}
	ev["_event"] = et.Name
	ev["_eventid"] = id
	ev["_gameloop"] = r.gameloop
	if r.userID {
		ev["_userid"] = userID
	}
	r.dec.ByteAlign()
	ev["_bits"] = int64(r.dec.UsedBits() - start)
	return ev, nil
}

// All drains the reader. On ErrTruncated the events decoded so far are returned with the error,
// so partially written live files can still be used.
func (r *EventReader) All() ([]Event, error) {
	var out []Event
	for {
		ev, err := r.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, ev)
	}
}

// TrackerEvents returns a reader for replay.tracker.events.
func (p *Protocol) TrackerEvents(data []byte) *EventReader {
	return &EventReader{p: p, dec: NewVersioned(data, p.Types), types: p.TrackerEventTypes, eventID: p.trackerEventID}
}

// GameEvents returns a reader for replay.game.events.
func (p *Protocol) GameEvents(data []byte) *EventReader {
	return &EventReader{p: p, dec: NewBitPacked(data, p.Types), types: p.GameEventTypes, eventID: p.gameEventID, userID: true}
}

// MessageEvents returns a reader for replay.message.events.
func (p *Protocol) MessageEvents(data []byte) *EventReader {
	return &EventReader{p: p, dec: NewBitPacked(data, p.Types), types: p.MessageEventTypes, eventID: p.messageEventID, userID: true}
}

// IsTruncated reports whether err is (or wraps) ErrTruncated.
func IsTruncated(err error) bool { return errors.Is(err, ErrTruncated) }

// UnitTag combines the index and recycle fields of unit events into one tag.
func UnitTag(index, recycle int64) int64 { return (index << 18) + recycle }

// UnitTagIndex extracts the index from a unit tag.
func UnitTagIndex(tag int64) int64 { return (tag >> 18) & 0x3fff }

// UnitTagRecycle extracts the recycle counter from a unit tag.
func UnitTagRecycle(tag int64) int64 { return tag & 0x3ffff }
