package stormreplay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hotsbuff/stormreplay/protocol"
)

// TestBattleLobby decodes the battle lobby player section of every replay in STORMREPLAY_TEST_DIR and
// checks it against replay.details: same players in the same slots, the same toon ids, battletag names
// equal to player names, non-zero account levels, party ids shared by at least two players.
func TestBattleLobby(t *testing.T) {
	dir := os.Getenv("STORMREPLAY_TEST_DIR")
	if dir == "" {
		t.Skip("STORMREPLAY_TEST_DIR not set")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.StormReplay"))
	if len(files) == 0 {
		t.Fatal("no replays in", dir)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			r, err := Open(f)
			if err != nil {
				t.Fatal(err)
			}
			players, err := r.BattleLobby()
			if err != nil {
				t.Fatal(err)
			}
			details, err := r.Details()
			if err != nil {
				t.Fatal(err)
			}
			list, _ := details["m_playerList"].([]any)
			bySlot := map[int]protocol.Struct{}
			for _, v := range list {
				p := v.(protocol.Struct)
				bySlot[int(p.Int("m_workingSetSlotId"))] = p
			}
			if len(players) != len(bySlot) {
				t.Fatalf("lobby players %d, details players %d", len(players), len(bySlot))
			}
			parties := map[uint64]int{}
			for _, p := range players {
				d := bySlot[p.Slot]
				if d == nil {
					t.Fatalf("slot %d not in details", p.Slot)
				}
				toon := d.Sub("m_toon")
				name, _, _ := strings.Cut(p.BattleTag, "#")
				if p.ID != toon.Int("m_id") || int64(p.Region) != toon.Int("m_region") || name != d.Str("m_name") {
					t.Errorf("slot %d: lobby %+v, details toon %v name %q", p.Slot, p, toon, d.Str("m_name"))
				}
				if p.AccountLevel <= 0 {
					t.Errorf("slot %d: account level %d", p.Slot, p.AccountLevel)
				}
				if p.Party != 0 {
					parties[p.Party]++
				}
			}
			for id, n := range parties {
				if n < 2 {
					t.Errorf("party %x has one player", id)
				}
			}
		})
	}
}
