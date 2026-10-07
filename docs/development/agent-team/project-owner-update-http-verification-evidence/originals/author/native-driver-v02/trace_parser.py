"""Pure parsing only; derived from accepted Usage native01/recover.py.

The former Usage run.py port regex missed inode-only strace -yy annotations.
Reassemble per-TID unfinished/resumed syscalls and bind successful getsockname
addresses to TCP inodes. Never treat an empty port set as native cleanup proof.
"""
import re


def parse_trace(raw, expected):
    pending, calls = {}, []
    for number, line in enumerate(raw.splitlines(), 1):
        match = re.match(r'^(\d+) (\d+\.\d+) (.*)$', line)
        if not match:
            continue
        tid, when, body = match.groups()
        if body.endswith('<unfinished ...>'):
            assert tid not in pending, (tid, line)
            pending[tid] = (body.removesuffix('<unfinished ...>'), number, float(when))
            continue
        resumed = re.match(r'<\.\.\. (\w+) resumed>(.*)', body)
        if resumed:
            prefix, begin, timestamp = pending.pop(tid)
            assert prefix.startswith(resumed[1] + '('), (prefix, body)
            body = prefix + resumed[2]
        else:
            begin, timestamp = number, float(when)
        calls.append({'tid': int(tid), 'line': begin, 'completed_line': number,
                      'time': timestamp, 'body': body})
    assert not pending, pending
    listeners, local, connections, accepted, created, closed, binds = {}, {}, [], [], set(), set(), []
    for call in calls:
        body = call['body']
        inode = re.search(r'<TCP(?:v6)?:\[(\d+)\]>', body)
        result = re.search(r'= (\d+)<TCP(?:v6)?:\[(\d+)\]>', body)
        if body.startswith('socket(') and result:
            created.add(int(result[2]))
        if body.startswith('close(') and inode and body.endswith('= 0'):
            closed.add(int(inode[1]))
        if body.startswith('listen(') and body.endswith('= 0'):
            assert inode, body
            identity = int(inode[1])
            assert identity not in listeners, body
            listeners[identity] = call
        if body.startswith('getsockname(') and body.endswith('= 0'):
            port = re.search(r'sin_port=htons\((\d+)\).*sin_addr=inet_addr\("127\.0\.0\.1"\)', body)
            assert inode and port, body
            local[int(inode[1])] = int(port[1])
        if body.startswith('connect('):
            port = re.search(r'sin_port=htons\((\d+)\).*sin_addr=inet_addr\("127\.0\.0\.1"\)', body)
            assert inode and port, body
            connections.append({'inode': int(inode[1]), 'server_port': int(port[1]), 'source': call})
        if body.startswith(('accept4(', 'accept(')) and result:
            accepted.append({'inode': int(result[2]), 'source': call})
        if body.startswith('bind('):
            binds.append(call)
    assert len(listeners) == expected, (len(listeners), expected)
    assert len(connections) == len(accepted) == expected, (len(connections), len(accepted), expected)
    assert all(identity in local for identity in listeners)
    records = [{'inode': identity, 'port': local[identity], 'listen': call,
                'closed': identity in closed} for identity, call in listeners.items()]
    for connection in connections:
        connection['client_port'] = local[connection['inode']]
        assert connection['server_port'] in [row['port'] for row in records], connection
    all_inodes = created | set(local) | set(listeners) | {row['inode'] for row in accepted}
    assert all_inodes <= closed, sorted(all_inodes - closed)
    # Sequential groups may legitimately reuse an ephemeral port. Pair mapping
    # and inode close are exact; no distinct-port-count assumption is needed.
    ports = sorted(set(local.values()))
    assert not expected or ports, 'empty native port proof is invalid'
    return {'calls': calls, 'listeners': records, 'connections': connections,
            'accepted_connections': accepted, 'all_ports': ports,
            'all_inodes': sorted(all_inodes), 'created_inodes': sorted(created),
            'all_closed_inodes': sorted(closed), 'binds': binds}
