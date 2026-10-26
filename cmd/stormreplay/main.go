// stormreplay dumps a .StormReplay as JSON lines, one decoded structure or event per line,
// in the same shape as heroprotocol's output (see tools/golden.py).
//
//	stormreplay <section> <file.StormReplay>
//	sections: header | details | initdata | attributes | tracker | game | message | info
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/hotsbuff/stormreplay"
	"github.com/hotsbuff/stormreplay/protocol"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: stormreplay <header|details|initdata|attributes|tracker|game|message|info> <file.StormReplay>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(section, path string) error {
	r, err := stormreplay.Open(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(os.Stdout, 1<<20)
	defer w.Flush()
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	emit := func(v any) error { return enc.Encode(stormreplay.ToJSON(v)) }
	switch section {
	case "info":
		return emit(map[string]any{
			"game_version": r.GameVersion(), "base_build": r.BaseBuild, "protocol_build": r.ProtocolBuild,
			"elapsed_gameloops": r.ElapsedGameLoops(), "files": r.Archive.Files(),
		})
	case "header":
		return emit(r.Header)
	case "details":
		v, err := r.Details()
		if err != nil {
			return err
		}
		return emit(v)
	case "initdata":
		v, err := r.InitData()
		if err != nil {
			return err
		}
		return emit(v)
	case "attributes":
		v, err := r.Attributes()
		if err != nil {
			return err
		}
		return emit(v)
	case "tracker", "game", "message":
		var rd *protocol.EventReader
		switch section {
		case "tracker":
			rd, err = r.TrackerEvents()
		case "game":
			rd, err = r.GameEvents()
		default:
			rd, err = r.MessageEvents()
		}
		if err != nil {
			return err
		}
		for {
			ev, err := rd.Next()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			if err := emit(ev); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("unknown section %q", section)
}
