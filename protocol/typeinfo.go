package protocol

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Kind of a protocol type.
type Kind uint8

const (
	KindInt Kind = iota
	KindChoice
	KindStruct
	KindBlob
	KindBool
	KindArray
	KindOptional
	KindFourCC
	KindNull
	KindBitArray
	KindReal32
	KindReal64
)

var kindNames = map[string]Kind{
	"int": KindInt, "choice": KindChoice, "struct": KindStruct, "blob": KindBlob, "bool": KindBool,
	"array": KindArray, "optional": KindOptional, "fourcc": KindFourCC, "null": KindNull,
	"bitarray": KindBitArray, "real32": KindReal32, "real64": KindReal64,
}

// Field of a struct or a choice alternative.
type Field struct {
	Name   string
	TypeID int
	Tag    int // struct: field tag in the versioned format; choice: alternative tag
}

// TypeInfo is one entry of a protocol's type table.
type TypeInfo struct {
	Kind    Kind
	Lo      int64 // int/array/blob/bitarray/choice: offset of the bounds
	Bits    int   // int/array/blob/bitarray/choice: bit width of the bounds
	TypeID  int   // array element / optional inner type
	Fields  []Field
	Choices map[int]Field
}

type typeInfoJSON struct {
	K string              `json:"k"`
	B []int64             `json:"b"`
	T int                 `json:"t"`
	F [][]json.RawMessage `json:"f"`
	C map[string][]any    `json:"c"`
}

func (t *TypeInfo) UnmarshalJSON(b []byte) error {
	var j typeInfoJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	k, ok := kindNames[j.K]
	if !ok {
		return fmt.Errorf("protocol: unknown type kind %q", j.K)
	}
	t.Kind, t.TypeID = k, j.T
	if len(j.B) == 2 {
		t.Lo, t.Bits = j.B[0], int(j.B[1])
	}
	for _, f := range j.F {
		if len(f) != 3 {
			return fmt.Errorf("protocol: bad struct field %s", f)
		}
		var fld Field
		if err := json.Unmarshal(f[0], &fld.Name); err != nil {
			return err
		}
		if err := json.Unmarshal(f[1], &fld.TypeID); err != nil {
			return err
		}
		if err := json.Unmarshal(f[2], &fld.Tag); err != nil {
			return err
		}
		t.Fields = append(t.Fields, fld)
	}
	if j.C != nil {
		t.Choices = map[int]Field{}
		for tagStr, v := range j.C {
			tag, err := strconv.Atoi(tagStr)
			if err != nil || len(v) != 2 {
				return fmt.Errorf("protocol: bad choice %s", tagStr)
			}
			name, _ := v[0].(string)
			tid, _ := v[1].(float64)
			t.Choices[tag] = Field{Name: name, TypeID: int(tid), Tag: tag}
		}
	}
	return nil
}

// EventType maps an event id to its struct type and name.
type EventType struct {
	TypeID int
	Name   string
}

type eventTypesJSON map[string][]any

func (e eventTypesJSON) toMap() (map[int]EventType, error) {
	out := make(map[int]EventType, len(e))
	for idStr, v := range e {
		id, err := strconv.Atoi(idStr)
		if err != nil || len(v) != 2 {
			return nil, fmt.Errorf("protocol: bad event type %s", idStr)
		}
		tid, _ := v[0].(float64)
		name, _ := v[1].(string)
		out[id] = EventType{TypeID: int(tid), Name: name}
	}
	return out, nil
}
