package protocol

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"sync"
)

//go:embed data/*
var dataFS embed.FS

// BaseBuild is the oldest protocol; its tables decode the replay header of every build.
const BaseBuild = 29406

var (
	once   sync.Once
	index  map[int]string // build -> data file id
	builds []int
	cache  sync.Map // file id -> *Protocol (tables shared by builds with identical definitions)
)

func loadIndex() {
	raw, err := dataFS.ReadFile("data/index.json")
	if err != nil {
		panic("protocol: embedded index missing: " + err.Error())
	}
	var idx struct {
		Builds map[string]string `json:"builds"`
	}
	if err := json.Unmarshal(raw, &idx); err != nil {
		panic("protocol: bad embedded index: " + err.Error())
	}
	index = make(map[int]string, len(idx.Builds))
	for b, id := range idx.Builds {
		n, err := strconv.Atoi(b)
		if err != nil {
			continue
		}
		index[n] = id
		builds = append(builds, n)
	}
	sort.Ints(builds)
}

// Builds lists every build with embedded tables, ascending.
func Builds() []int {
	once.Do(loadIndex)
	return append([]int(nil), builds...)
}

// ForBuild returns the protocol for an exact build.
func ForBuild(build int) (*Protocol, error) {
	once.Do(loadIndex)
	id, ok := index[build]
	if !ok {
		return nil, fmt.Errorf("protocol: build %d not available", build)
	}
	return load(build, id)
}

// Nearest returns the protocol for build, or the newest build not above it. Game builds without
// a published protocol (newer than heroprotocol) decode fine with the latest one in practice.
// The second result is the build actually used.
func Nearest(build int) (*Protocol, int, error) {
	once.Do(loadIndex)
	if _, ok := index[build]; ok {
		p, err := ForBuild(build)
		return p, build, err
	}
	i := sort.SearchInts(builds, build) // first >= build
	if i == 0 {
		return nil, 0, fmt.Errorf("protocol: build %d is older than the oldest known (%d)", build, builds[0])
	}
	b := builds[i-1]
	p, err := ForBuild(b)
	return p, b, err
}

// Latest returns the newest embedded protocol.
func Latest() (*Protocol, error) {
	once.Do(loadIndex)
	return ForBuild(builds[len(builds)-1])
}

type protocolJSON struct {
	TypeInfos         []TypeInfo     `json:"typeinfos"`
	GameEventTypes    eventTypesJSON `json:"game_event_types"`
	MessageEventTypes eventTypesJSON `json:"message_event_types"`
	TrackerEventTypes eventTypesJSON `json:"tracker_event_types"`
	IDs               struct {
		GameEventID    int `json:"game_eventid"`
		MessageEventID int `json:"message_eventid"`
		TrackerEventID int `json:"tracker_eventid"`
		SVarUint32     int `json:"svaruint32"`
		ReplayUserID   int `json:"replay_userid"`
		ReplayHeader   int `json:"replay_header"`
		GameDetails    int `json:"game_details"`
		ReplayInitData int `json:"replay_initdata"`
	} `json:"ids"`
}

func load(build int, id string) (*Protocol, error) {
	if v, ok := cache.Load(id); ok {
		p := *v.(*Protocol)
		p.Build = build
		return &p, nil
	}
	raw, err := dataFS.ReadFile("data/" + id + ".json.gz")
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	js, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	var pj protocolJSON
	if err := json.Unmarshal(js, &pj); err != nil {
		return nil, fmt.Errorf("protocol: parse tables %s: %w", id, err)
	}
	p := &Protocol{Types: pj.TypeInfos}
	if p.GameEventTypes, err = pj.GameEventTypes.toMap(); err != nil {
		return nil, err
	}
	if p.MessageEventTypes, err = pj.MessageEventTypes.toMap(); err != nil {
		return nil, err
	}
	if p.TrackerEventTypes, err = pj.TrackerEventTypes.toMap(); err != nil {
		return nil, err
	}
	p.gameEventID, p.messageEventID, p.trackerEventID = pj.IDs.GameEventID, pj.IDs.MessageEventID, pj.IDs.TrackerEventID
	p.svarUint32, p.replayUserID = pj.IDs.SVarUint32, pj.IDs.ReplayUserID
	p.replayHeader, p.gameDetails, p.replayInitData = pj.IDs.ReplayHeader, pj.IDs.GameDetails, pj.IDs.ReplayInitData
	cache.Store(id, p)
	out := *p
	out.Build = build
	return &out, nil
}
