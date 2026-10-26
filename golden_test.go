package stormreplay

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hotsbuff/stormreplay/protocol"
)

// TestGolden compares every section of every replay in STORMREPLAY_TEST_DIR with the JSON lines
// produced by tools/golden.py (Blizzard's heroprotocol) in STORMREPLAY_GOLDEN_DIR.
func TestGolden(t *testing.T) {
	replayDir, goldenDir := os.Getenv("STORMREPLAY_TEST_DIR"), os.Getenv("STORMREPLAY_GOLDEN_DIR")
	if replayDir == "" || goldenDir == "" {
		t.Skip("STORMREPLAY_TEST_DIR / STORMREPLAY_GOLDEN_DIR not set")
	}
	files, _ := filepath.Glob(filepath.Join(replayDir, "*.StormReplay"))
	if len(files) == 0 {
		t.Fatal("no replays in", replayDir)
	}
	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f), ".StormReplay")
		t.Run(base, func(t *testing.T) {
			r, err := Open(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, section := range []string{"header", "details", "initdata", "attributes", "tracker", "game", "message"} {
				t.Run(section, func(t *testing.T) {
					want := readGolden(t, filepath.Join(goldenDir, base+"."+section+".jsonl"))
					got := decodeSection(t, r, section)
					if len(got) != len(want) {
						t.Errorf("%s: %d items, golden has %d", section, len(got), len(want))
					}
					n := len(got)
					if len(want) < n {
						n = len(want)
					}
					for i := 0; i < n; i++ {
						g := roundTrip(t, ToJSON(got[i]))
						if !reflect.DeepEqual(g, want[i]) {
							t.Fatalf("%s item %d differs:\n got: %s\nwant: %s\n%s", section, i, short(g), short(want[i]), diffPath(g, want[i], "$"))
						}
					}
				})
			}
		})
	}
}

func decodeSection(t *testing.T, r *Replay, section string) []any {
	t.Helper()
	one := func(v any, err error) []any {
		if err != nil {
			t.Fatal(err)
		}
		return []any{v}
	}
	switch section {
	case "header":
		return []any{r.Header}
	case "details":
		return one(r.Details())
	case "initdata":
		return one(r.InitData())
	case "attributes":
		return one(r.Attributes())
	}
	var rd *protocol.EventReader
	var err error
	switch section {
	case "tracker":
		rd, err = r.TrackerEvents()
	case "game":
		rd, err = r.GameEvents()
	case "message":
		rd, err = r.MessageEvents()
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []any
	for {
		ev, err := rd.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("after %d events: %v", len(out), err)
		}
		out = append(out, ev)
	}
}

func readGolden(t *testing.T, path string) []any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Skip("no golden:", path)
	}
	defer f.Close()
	var out []any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var v any
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

// roundTrip normalizes Go values through JSON so numbers compare as float64 like the golden side.
func roundTrip(t *testing.T, v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func short(v any) string {
	b, _ := json.Marshal(v)
	if len(b) > 400 {
		return string(b[:400]) + "…"
	}
	return string(b)
}

func diffPath(a, b any, path string) string {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			return fmt.Sprintf("at %s: got object, want %T", path, b)
		}
		for k, av := range x {
			bv, ok := y[k]
			if !ok {
				return fmt.Sprintf("at %s: extra key %q", path, k)
			}
			if d := diffPath(av, bv, path+"."+k); d != "" {
				return d
			}
		}
		for k := range y {
			if _, ok := x[k]; !ok {
				return fmt.Sprintf("at %s: missing key %q", path, k)
			}
		}
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return fmt.Sprintf("at %s: array mismatch (%d vs %T)", path, len(x), b)
		}
		for i := range x {
			if d := diffPath(x[i], y[i], fmt.Sprintf("%s[%d]", path, i)); d != "" {
				return d
			}
		}
	default:
		if !reflect.DeepEqual(a, b) {
			return fmt.Sprintf("at %s: got %v (%T), want %v (%T)", path, a, a, b, b)
		}
	}
	return ""
}
