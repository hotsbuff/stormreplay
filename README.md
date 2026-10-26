# stormreplay

Go parser for Heroes of the Storm replay files (`.StormReplay`), a port of Blizzard's
[heroprotocol](https://github.com/Blizzard/heroprotocol). It reads the MPQ archive, decodes the
header, details, init data, attributes and all three event streams (tracker, game, message) with the
same per-build type tables as the Python original, and produces the same structures.

- No cgo, no dependencies outside the standard library.
- Type tables for every build heroprotocol knows (29406 … 91756) are embedded, de-duplicated and
  gzipped (about 270 KB). Newer game builds fall back to the newest tables, which is what works in
  practice (verified on build 98348).
- Streams events one at a time; a file the game is still writing decodes up to the truncation point
  and reports `protocol.ErrTruncated`, so live overlays can poll the temporary replay directory.
- Verified against heroprotocol output byte for byte on real replays (see Testing).

## Battle lobby

`replay.server.battlelobby` is not covered by heroprotocol. `Replay.BattleLobby()` decodes its player
section: full battletags, account levels, party membership (players of one party share `Party`),
silence penalties and active boosts. The section is located by the lobby random seed
(`m_randomValue` in init data) and validated while decoding, so the variable-layout parts before it are
skipped. The player section layout follows
[Heroes.StormReplayParser](https://github.com/HeroesToolChest/Heroes.StormReplayParser) (MIT).
Supported for builds 69947 and newer; `TestBattleLobby` checks it against `replay.details`.

## Usage

```go
import "github.com/hotsbuff/stormreplay"

r, err := stormreplay.Open("match.StormReplay")
if err != nil { ... }
fmt.Println(r.GameVersion(), r.BaseBuild, r.ProtocolBuild)

details, _ := r.Details()
for _, p := range details["m_playerList"].([]any) {
    player := p.(protocol.Struct)
    fmt.Println(player.Str("m_name"), player.Str("m_hero"), player.Int("m_teamId"))
}

events, _ := r.TrackerEvents()
for {
    ev, err := events.Next()
    if err == io.EOF { break }
    if err != nil { ... } // protocol.IsTruncated(err) for live files
    if ev.Name() == "NNet.Replay.Tracker.SUnitBornEvent" {
        fmt.Println(ev.GameLoop(), ev.Str("m_unitTypeName"))
    }
}
```

Loose files (the live `TempWriteReplayP*` directory, or the contents of a `.StormSave`):

```go
p, _, _ := stormreplay.ProtocolFor(98348)
events, err := p.TrackerEvents(data).All() // events decoded so far + ErrTruncated if cut mid-event
```

Decoded values map to Go as follows: ints → `int64`, bools → `bool`, blobs and fourcc → `[]byte`,
bit arrays → `protocol.BitArray`, structs and choices → `protocol.Struct` (`map[string]any`), arrays
→ `[]any`, optionals → value or `nil`. Events carry `_event`, `_eventid`, `_gameloop`, `_userid`,
`_bits` like heroprotocol. `stormreplay.ToJSON` converts a value to plain JSON types.

## CLI

```sh
go install github.com/hotsbuff/stormreplay/cmd/stormreplay@latest
stormreplay info    match.StormReplay   # version, builds, files
stormreplay tracker match.StormReplay   # one JSON event per line
stormreplay game    match.StormReplay
stormreplay details match.StormReplay
```

Sections: `info`, `header`, `details`, `initdata`, `attributes`, `lobby`, `tracker`, `game`, `message`.

## Testing

Unit tests run with `go test ./...`. The golden test compares every section of real replays with
heroprotocol's output:

```sh
python3 -m venv .venv && .venv/bin/pip install heroprotocol mpyq
mkdir -p /tmp/replays /tmp/golden && cp ~/Replays/*.StormReplay /tmp/replays/
for f in /tmp/replays/*.StormReplay; do for s in header details initdata attributes tracker game message; do
  .venv/bin/python tools/golden.py "$f" $s > "/tmp/golden/$(basename "${f%.StormReplay}").$s.jsonl"; done; done
STORMREPLAY_TEST_DIR=/tmp/replays STORMREPLAY_GOLDEN_DIR=/tmp/golden go test ./...
```

## Updating protocol tables

When Blizzard publishes new `protocolNNNNN.py` files:

```sh
python3 tools/gen_protocols.py path/to/heroprotocol/versions protocol/data
```

## Layout

```
mpq/        MPQ archive reader (hash/block tables, sectors, zlib and bzip2)
protocol/   bit-packed and versioned decoders, type tables, event readers, embedded data
replay.go   Replay: open an archive and decode its files
json.go     ToJSON: plain JSON representation
cmd/        the stormreplay CLI
tools/      table generator and golden-file generator (Python)
```

## License

MIT. Protocol tables derive from heroprotocol, © Blizzard Entertainment, MIT.
