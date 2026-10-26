// Package stormreplay parses Heroes of the Storm replay files (.StormReplay) and the loose
// replay files the game writes while a match is running. It is a Go port of Blizzard's
// heroprotocol: the same per-build type tables, the same decoded structures.
package stormreplay

import (
	"fmt"

	"github.com/hotsbuff/stormreplay/mpq"
	"github.com/hotsbuff/stormreplay/protocol"
)

// File names inside a replay archive.
const (
	FileDetails    = "replay.details"
	FileInitData   = "replay.initData"
	FileAttributes = "replay.attributes.events"
	FileTracker    = "replay.tracker.events"
	FileGame       = "replay.game.events"
	FileMessage    = "replay.message.events"
)

// Replay is an opened replay archive.
type Replay struct {
	Archive *mpq.Archive
	// Header is the decoded replay header (m_version, m_elapsedGameLoops, ...).
	Header protocol.Struct
	// BaseBuild is the game build the replay was written by (m_version.m_baseBuild).
	BaseBuild int
	// Protocol decodes this replay; ProtocolBuild is the build whose tables are used, which is the
	// newest known build when BaseBuild is newer than the embedded tables.
	Protocol      *protocol.Protocol
	ProtocolBuild int
}

// Open parses a replay file.
func Open(path string) (*Replay, error) {
	a, err := mpq.Open(path)
	if err != nil {
		return nil, err
	}
	return fromArchive(a)
}

// Parse parses a replay from memory.
func Parse(data []byte) (*Replay, error) {
	a, err := mpq.Parse(data)
	if err != nil {
		return nil, err
	}
	return fromArchive(a)
}

func fromArchive(a *mpq.Archive) (*Replay, error) {
	// The header is decoded with the newest tables, like heroprotocol does: the type only grows
	// (newer fields are skipped by older tables), and its base fields are stable since 29406.
	latest, err := protocol.Latest()
	if err != nil {
		return nil, err
	}
	header, err := latest.DecodeHeader(a.UserData())
	if err != nil {
		return nil, fmt.Errorf("stormreplay: header: %w", err)
	}
	build := int(header.Sub("m_version").Int("m_baseBuild"))
	p, used, err := protocol.Nearest(build)
	if err != nil {
		return nil, err
	}
	return &Replay{Archive: a, Header: header, BaseBuild: build, Protocol: p, ProtocolBuild: used}, nil
}

// GameVersion returns "major.minor.revision.build" from the header.
func (r *Replay) GameVersion() string {
	v := r.Header.Sub("m_version")
	return fmt.Sprintf("%d.%d.%d.%d", v.Int("m_major"), v.Int("m_minor"), v.Int("m_revision"), v.Int("m_build"))
}

// ElapsedGameLoops is the match length in game loops (16 per second).
func (r *Replay) ElapsedGameLoops() int64 { return r.Header.Int("m_elapsedGameLoops") }

func (r *Replay) file(name string) ([]byte, error) {
	data, err := r.Archive.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("stormreplay: %w", err)
	}
	return data, nil
}

// Details decodes replay.details (players, map, time).
func (r *Replay) Details() (protocol.Struct, error) {
	data, err := r.file(FileDetails)
	if err != nil {
		return nil, err
	}
	return r.Protocol.DecodeDetails(data)
}

// InitData decodes replay.initData (lobby and game options).
func (r *Replay) InitData() (protocol.Struct, error) {
	data, err := r.file(FileInitData)
	if err != nil {
		return nil, err
	}
	return r.Protocol.DecodeInitData(data)
}

// Attributes decodes replay.attributes.events.
func (r *Replay) Attributes() (*protocol.Attributes, error) {
	data, err := r.file(FileAttributes)
	if err != nil {
		return nil, err
	}
	return r.Protocol.DecodeAttributes(data)
}

// TrackerEvents returns a reader over replay.tracker.events.
func (r *Replay) TrackerEvents() (*protocol.EventReader, error) {
	data, err := r.file(FileTracker)
	if err != nil {
		return nil, err
	}
	return r.Protocol.TrackerEvents(data), nil
}

// GameEvents returns a reader over replay.game.events.
func (r *Replay) GameEvents() (*protocol.EventReader, error) {
	data, err := r.file(FileGame)
	if err != nil {
		return nil, err
	}
	return r.Protocol.GameEvents(data), nil
}

// MessageEvents returns a reader over replay.message.events.
func (r *Replay) MessageEvents() (*protocol.EventReader, error) {
	data, err := r.file(FileMessage)
	if err != nil {
		return nil, err
	}
	return r.Protocol.MessageEvents(data), nil
}

// ProtocolFor returns the protocol to decode loose files (a live TempWriteReplay directory or a
// .StormSave) of a known game build. Use Nearest semantics: newer builds fall back to the latest tables.
func ProtocolFor(build int) (*protocol.Protocol, int, error) { return protocol.Nearest(build) }
