package stormreplay

import (
	"errors"
	"fmt"
)

// FileBattleLobby is the server lobby snapshot. It is not covered by heroprotocol: the format is a
// custom bit-packed structure. Its player section holds data that is nowhere else in the replay:
// full battletags, account levels and party membership.
const FileBattleLobby = "replay.server.battlelobby"

// LobbyPlayer is one player entry of the battle lobby player section.
type LobbyPlayer struct {
	// Slot is m_workingSetSlotId, the same as in replay.details and replay.initData.
	Slot int
	// Toon handle: region, program id ("Hero"), realm and id, as in replay.details m_toon.
	Region    int
	ProgramID string
	Realm     int
	ID        int64
	// BattleTag is the full battletag with the number ("Name#1234"); empty if not present.
	BattleTag string
	// AccountLevel is the player's account level (0 in custom games).
	AccountLevel int
	// Party is a party identifier: players in the same party have the same non-zero value.
	Party uint64
	// Penalties and flags.
	Silenced      bool
	VoiceSilenced bool
	BlizzardStaff bool
	ActiveBoost   bool
}

// ErrBattleLobbyUnsupported is returned for builds whose battle lobby layout is not supported.
var ErrBattleLobbyUnsupported = errors.New("stormreplay: battle lobby layout not supported for this build")

// BattleLobby decodes the player section of replay.server.battlelobby.
//
// The file starts with cache handles, lobby attributes, locales, mods and a large skin collection;
// their layouts change between builds. The player section follows the lobby random seed, which is
// also m_randomValue in replay.initData, so instead of walking the whole file the decoder locates the
// seed and decodes the player section from there. Candidate positions are validated by decoding the
// whole section (every toon must have program id "Hero"), so a false seed match is skipped.
//
// The player section layout follows Heroes.StormReplayParser (MIT, HeroesToolChest).
func (r *Replay) BattleLobby() ([]LobbyPlayer, error) {
	if r.BaseBuild < 69947 {
		return nil, ErrBattleLobbyUnsupported
	}
	data, err := r.file(FileBattleLobby)
	if err != nil {
		return nil, err
	}
	init, err := r.InitData()
	if err != nil {
		return nil, err
	}
	seed := init.Sub("m_syncLobbyState").Sub("m_gameDescription").Int("m_randomValue")
	return DecodeBattleLobbyPlayers(data, uint32(seed))
}

// DecodeBattleLobbyPlayers finds the player section of a battle lobby by its random seed and decodes it.
func DecodeBattleLobbyPlayers(data []byte, seed uint32) ([]LobbyPlayer, error) {
	total := len(data) * 8
	var lastErr error
	// The player section is near the end of the file: scan backwards from the end.
	for pos := total - 32; pos >= 0; pos-- {
		b := bitReader{data: data, pos: pos}
		if b.bits(32) != uint64(seed) {
			continue
		}
		players, err := decodeLobbyPlayers(&b)
		if err == nil {
			return players, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, fmt.Errorf("stormreplay: battle lobby: %w", lastErr)
	}
	return nil, errors.New("stormreplay: battle lobby: random seed not found")
}

func decodeLobbyPlayers(b *bitReader) ([]LobbyPlayer, error) {
	b.skip(32)
	n := int(b.bits(5))
	if n == 0 || n > 16 {
		return nil, fmt.Errorf("player count %d", n)
	}
	players := make([]LobbyPlayer, 0, n)
	for i := 0; i < n; i++ {
		var p LobbyPlayer
		b.skip(32)
		p.Slot = int(b.bits(5))
		p.Region = int(b.bits(8))
		p.ProgramID = b.fourCC()
		p.Realm = int(b.bits(32))
		p.ID = int64(b.bits(64))
		if p.ProgramID != "Hero" {
			return nil, fmt.Errorf("player %d: program id %q", i, p.ProgramID)
		}
		// String toon "T:..." with the region global id.
		b.skip(8)
		if s := b.fourCC(); s != "Hero" {
			return nil, fmt.Errorf("player %d: string toon program id %q", i, s)
		}
		b.skip(32)
		b.blob(7, 2)
		b.skip(1 + 2) // m_flags, unknown
		if b.flag() {
			b.skip(2 + 8)
			if s := b.fourCC(); s != "Hero" {
				return nil, fmt.Errorf("player %d: repeated toon program id %q", i, s)
			}
			b.skip(32)
			b.blob(7, 2)
			b.skip(1)
		}
		b.skip(198)
		if b.flag() {
			b.skip(64)
		}
		b.skip(4)
		b.skip(32)          // client base build
		if b.bits(1) == 0 { // collection choice
			b.skip(int(b.bits(12)))
			b.skip(1)
		}
		p.Silenced = b.flag()
		b.skip(1)
		p.VoiceSilenced = b.flag()
		p.BlizzardStaff = b.flag()
		if b.flag() {
			p.Party = b.bits(64)
		}
		if b.flag() {
			p.BattleTag = string(b.blob(7, 0))
		}
		p.AccountLevel = int(b.bits(32))
		p.ActiveBoost = b.flag()
		if b.err != nil {
			return nil, b.err
		}
		if p.Slot > 15 || p.BattleTag != "" && !validBattleTag(p.BattleTag) {
			return nil, fmt.Errorf("player %d: slot %d battletag %q", i, p.Slot, p.BattleTag)
		}
		players = append(players, p)
	}
	return players, nil
}

func validBattleTag(s string) bool {
	for i := len(s) - 1; i > 0; i-- {
		if s[i] == '#' {
			return i < len(s)-1
		}
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return false
}

// bitReader reads the battle lobby bit stream: bits are taken from the low end of each byte first and
// accumulated most significant first (the layout of the bit-packed replay streams). Byte blobs are
// byte-aligned.
type bitReader struct {
	data []byte
	pos  int // bit position
	err  error
}

func (b *bitReader) bits(n int) uint64 {
	var v uint64
	for n > 0 {
		if b.pos>>3 >= len(b.data) {
			b.err = errors.New("unexpected end of battle lobby")
			return 0
		}
		off := b.pos & 7
		take := min(8-off, n)
		chunk := uint64(b.data[b.pos>>3]>>off) & (1<<take - 1)
		v = v<<take | chunk
		b.pos += take
		n -= take
	}
	return v
}

func (b *bitReader) skip(n int) {
	if b.pos+n > len(b.data)*8 {
		b.err = errors.New("unexpected end of battle lobby")
		b.pos = len(b.data) * 8
		return
	}
	b.pos += n
}

func (b *bitReader) flag() bool { return b.bits(1) == 1 }

// fourCC reads a 32-bit value as four characters, most significant byte first.
func (b *bitReader) fourCC() string {
	v := b.bits(32)
	return string([]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

// blob reads a length of lenBits bits plus extra, then that many byte-aligned bytes.
func (b *bitReader) blob(lenBits, extra int) []byte {
	n := int(b.bits(lenBits)) + extra
	b.pos = (b.pos + 7) &^ 7
	start := b.pos >> 3
	if start+n > len(b.data) {
		b.err = errors.New("unexpected end of battle lobby")
		b.pos = len(b.data) * 8
		return nil
	}
	b.pos += n * 8
	return b.data[start : start+n]
}
