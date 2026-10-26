#!/usr/bin/env python3
"""Extract protocol type tables from Blizzard's heroprotocol into JSON for the Go port.

Usage: gen_protocols.py <path to heroprotocol/versions> <output dir>

Writes <out>/index.json ({"builds": {"91756": "<sha>"}, ...}) and <out>/<sha>.json.gz, one per
distinct table set (many builds share identical tables).
"""
import gzip, hashlib, importlib.util, json, os, re, sys

KIND = {'_int': 'int', '_choice': 'choice', '_struct': 'struct', '_blob': 'blob', '_bool': 'bool',
        '_array': 'array', '_optional': 'optional', '_fourcc': 'fourcc', '_null': 'null',
        '_bitarray': 'bitarray', '_real32': 'real32', '_real64': 'real64'}

def convert_typeinfo(kind, args):
    k = KIND[kind]
    t = {'k': k}
    if k == 'int' or k == 'blob' or k == 'bitarray':
        t['b'] = list(args[0])
    elif k == 'array':
        t['b'] = list(args[0]); t['t'] = args[1]
    elif k == 'optional':
        t['t'] = args[0]
    elif k == 'choice':
        t['b'] = list(args[0]); t['c'] = {str(tag): [name, tid] for tag, (name, tid) in sorted(args[1].items())}
    elif k == 'struct':
        t['f'] = [[name, tid, tag] for (name, tid, tag) in args[0]]
    return t

def load(path):
    name = os.path.basename(path)[:-3]
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod

def main():
    src, out = sys.argv[1], sys.argv[2]
    os.makedirs(out, exist_ok=True)
    index = {}
    seen = set()
    for fn in sorted(os.listdir(src)):
        m = re.match(r'protocol(\d+)\.py$', fn)
        if not m:
            continue
        build = int(m.group(1))
        mod = load(os.path.join(src, fn))
        ev = lambda d: {str(k): [v[0], v[1]] for k, v in d.items()}
        p = {
            'typeinfos': [convert_typeinfo(k, a) for k, a in mod.typeinfos],
            'game_event_types': ev(getattr(mod, 'game_event_types', {})),
            'message_event_types': ev(getattr(mod, 'message_event_types', {})),
            'tracker_event_types': ev(getattr(mod, 'tracker_event_types', {})),
            'ids': {
                'game_eventid': getattr(mod, 'game_eventid_typeid', -1),
                'message_eventid': getattr(mod, 'message_eventid_typeid', -1),
                'tracker_eventid': getattr(mod, 'tracker_eventid_typeid', -1),
                'svaruint32': getattr(mod, 'svaruint32_typeid', -1),
                'replay_userid': getattr(mod, 'replay_userid_typeid', -1),
                'replay_header': getattr(mod, 'replay_header_typeid', -1),
                'game_details': getattr(mod, 'game_details_typeid', -1),
                'replay_initdata': getattr(mod, 'replay_initdata_typeid', -1),
            },
        }
        canon = json.dumps(p, sort_keys=True, separators=(',', ':')).encode()
        sha = hashlib.sha256(canon).hexdigest()[:16]
        index[str(build)] = sha
        if sha not in seen:
            seen.add(sha)
            with gzip.open(os.path.join(out, sha + '.json.gz'), 'wb', compresslevel=9) as f:
                f.write(canon)
    with open(os.path.join(out, 'index.json'), 'w') as f:
        json.dump({'builds': index}, f, indent=0, sort_keys=True)
    print('builds:', len(index), 'distinct tables:', len(seen))

if __name__ == '__main__':
    main()
