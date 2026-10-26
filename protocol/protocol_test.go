package protocol

import (
	"errors"
	"testing"
)

func TestBufferBits(t *testing.T) {
	b := NewBuffer([]byte{0b10110011, 0xFF}, true)
	v, _ := b.ReadBits(3) // big-endian within a byte means low bits first, as heroprotocol does
	if v != 0b011 {
		t.Fatalf("3 bits = %b", v)
	}
	v, _ = b.ReadBits(5)
	if v != 0b10110 {
		t.Fatalf("5 bits = %b", v)
	}
	if b.UsedBits() != 8 {
		t.Fatal("used bits")
	}
	if _, err := b.ReadBits(16); !errors.Is(err, ErrTruncated) {
		t.Fatalf("expected truncation, got %v", err)
	}
}

func TestVint(t *testing.T) {
	cases := map[int64][]byte{
		0:    {0x00},
		1:    {0x02},
		-1:   {0x03},
		63:   {0x7E},
		64:   {0x80, 0x01},
		1000: {0xD0, 0x0F},
	}
	for want, raw := range cases {
		d := &versioned{buf: NewBuffer(raw, true)}
		got, err := d.vint()
		if err != nil || got != want {
			t.Errorf("vint(%x) = %d, %v; want %d", raw, got, err, want)
		}
	}
}

func TestRegistry(t *testing.T) {
	builds := Builds()
	if len(builds) < 300 || builds[0] != BaseBuild {
		t.Fatalf("builds: %d, first %d", len(builds), builds[0])
	}
	p, used, err := Nearest(98348)
	if err != nil || used != builds[len(builds)-1] || p.Build != used {
		t.Fatalf("nearest: %v used=%d", err, used)
	}
	if _, _, err := Nearest(1); err == nil {
		t.Fatal("expected error for build older than base")
	}
	exact, err := ForBuild(91756)
	if err != nil || len(exact.Types) == 0 || len(exact.TrackerEventTypes) == 0 {
		t.Fatalf("91756: %v", err)
	}
	if UnitTag(UnitTagIndex(12345678), UnitTagRecycle(12345678)) != 12345678 {
		t.Fatal("unit tag roundtrip")
	}
}

func TestTruncatedStream(t *testing.T) {
	p, err := ForBuild(91756)
	if err != nil {
		t.Fatal(err)
	}
	// A versioned stream cut after the first byte: no complete event, ErrTruncated.
	evs, err := p.TrackerEvents([]byte{0x03}).All()
	if !IsTruncated(err) || len(evs) != 0 {
		t.Fatalf("expected ErrTruncated with no events, got %v (%d events)", err, len(evs))
	}
}
