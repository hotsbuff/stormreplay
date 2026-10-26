#!/usr/bin/env python3
"""Dump a replay with Blizzard's heroprotocol as normalized JSON for comparing with the Go port.

Usage: golden.py <replay.StormReplay> <section> [--protocol-dir DIR]
  section: header | details | initdata | attributes | tracker | game | message

Normalization (mirrored by `stormreplay dump`): bytes are UTF-8 strings when valid, otherwise {"$hex": "..."};
one-element float tuples become plain floats; bit arrays become [length, value|bytes].
"""
import json, sys
import mpyq
from heroprotocol.versions import build, latest

def norm(v):
    if isinstance(v, dict):
        return {k: norm(x) for k, x in v.items()}
    if isinstance(v, (list, tuple)):
        if isinstance(v, tuple) and len(v) == 1 and isinstance(v[0], float):
            return v[0]
        return [norm(x) for x in v]
    if isinstance(v, (bytes, bytearray)):
        try:
            return v.decode('utf-8')
        except UnicodeDecodeError:
            return {'$hex': v.hex()}
    return v

def main():
    path, section = sys.argv[1], sys.argv[2]
    archive = mpyq.MPQArchive(path)
    header = latest().decode_replay_header(archive.header['user_data_header']['content'])
    base = header['m_version']['m_baseBuild']
    try:
        proto = build(base)
    except Exception:
        proto = latest()
    if section == 'header':
        out = [header]
    elif section == 'details':
        out = [proto.decode_replay_details(archive.read_file('replay.details'))]
    elif section == 'initdata':
        out = [proto.decode_replay_initdata(archive.read_file('replay.initData'))]
    elif section == 'attributes':
        out = [proto.decode_replay_attributes_events(archive.read_file('replay.attributes.events'))]
    elif section == 'tracker':
        out = proto.decode_replay_tracker_events(archive.read_file('replay.tracker.events'))
    elif section == 'game':
        out = proto.decode_replay_game_events(archive.read_file('replay.game.events'))
    elif section == 'message':
        out = proto.decode_replay_message_events(archive.read_file('replay.message.events'))
    else:
        sys.exit('unknown section')
    for item in out:
        sys.stdout.write(json.dumps(norm(item), sort_keys=True, ensure_ascii=False) + '\n')

if __name__ == '__main__':
    main()
