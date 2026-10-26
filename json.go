package stormreplay

import (
	"encoding/hex"
	"unicode/utf8"

	"github.com/hotsbuff/stormreplay/protocol"
)

// ToJSON converts a decoded value into plain JSON-encodable Go values, the way the project's
// golden files (tools/golden.py) and `stormreplay dump` represent them:
// blobs become strings when valid UTF-8, otherwise {"$hex": "..."}; bit arrays become
// [bits, value] (bit-packed) or [bits, bytes] (versioned); structs become maps.
func ToJSON(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case protocol.Struct:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = ToJSON(val)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = ToJSON(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = ToJSON(val)
		}
		return out
	case []byte:
		return bytesToJSON(x)
	case protocol.BitArray:
		if x.Bytes != nil {
			return []any{x.Bits, bytesToJSON(x.Bytes)}
		}
		return []any{x.Bits, x.Value}
	case float32:
		return float64(x)
	case *protocol.Attributes:
		scopes := map[string]any{}
		for scope, attrs := range x.Scopes {
			m := map[string]any{}
			for id, list := range attrs {
				vals := make([]any, len(list))
				for i, a := range list {
					vals[i] = map[string]any{"namespace": a.Namespace, "attrid": a.AttrID, "value": bytesToJSON(a.Value)}
				}
				m[itoa(id)] = vals
			}
			scopes[itoa(scope)] = m
		}
		return map[string]any{"source": x.Source, "mapNamespace": x.MapNamespace, "scopes": scopes}
	}
	return v
}

func bytesToJSON(b []byte) any {
	if utf8.Valid(b) {
		return string(b)
	}
	return map[string]any{"$hex": hex.EncodeToString(b)}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
