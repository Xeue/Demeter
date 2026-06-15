#!/usr/bin/env python3
"""Decode a RollCall (TCP/2050) capture into a readable transcript.

Reverse-engineering aid for replacing RollTrak.exe with a native client.
See docs/ROLLCALL_PROTOCOL.md for the format these notes are based on.

Usage:
    pip install scapy
    python3 tools/rollcall-decode.py Rollcall.pcapng [--port 2050]

Prints every RollCall message in time order with opcode, command id and value,
plus raw hex for control/handshake messages. Run it against a fresh capture of a
*known* `RollTrak.exe -a <ip> <cmd>@<net>:<addr>:<slot>?` to pin down the
CLI-to-wire address mapping (the one remaining open item).
"""
import argparse
import struct
from scapy.all import rdpcap, TCP, IP, Raw

OPC = {0x45: 'GET', 0x46: 'SET', 0x47: 'RPL',
       0x1c: 'OPEN', 0x01: 'ACK', 0x14: 'IDENT', 0x21: 'IDENT'}


def printable(b):
    return ''.join(chr(c) if 32 <= c < 127 else '.' for c in b)


def addr(b):
    net, unit, port = struct.unpack('>HHH', b)
    return f"{net:04x}:{unit:04x}:{port:04x}"


def frame_messages(stream):
    """Split a reassembled byte stream into RollCall messages (00 0C <len> ...)."""
    i, out = 0, []
    while i + 4 <= len(stream):
        if stream[i:i + 2] != b'\x00\x0c':
            i += 1
            continue
        ln = struct.unpack('>H', stream[i + 2:i + 4])[0]
        body = stream[i + 4:i + 4 + ln]
        if len(body) < ln:
            break
        out.append(body)
        i += 4 + ln
    return out


def decode(body):
    dst, src = addr(body[0:6]), addr(body[6:12])
    inner = body[14:14 + struct.unpack('>H', body[12:14])[0]]
    if not inner:
        return dst, src, '?', None, ''
    op = OPC.get(inner[0], f"0x{inner[0]:02x}")
    flags = inner[1] if len(inner) > 1 else 0
    cmd = struct.unpack('>I', inner[2:6])[0] if inner[0] in (0x45, 0x46, 0x47) and len(inner) >= 6 else None
    val = ''
    rest = inner[6:]
    if cmd is not None and len(rest) >= 8:
        dtype, v = struct.unpack('>II', rest[:8])
        if dtype == 2:
            val = f"str={rest[8:].split(chr(0).encode())[0].decode('latin1', 'replace')!r}"
        elif dtype == 1:
            val = f"int={v}"
        else:
            val = f"raw={rest.hex()}"
    note = ' [notify]' if flags == 0x80 else ''
    return dst, src, op, cmd, val + note


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('pcap')
    ap.add_argument('--port', type=int, default=2050)
    args = ap.parse_args()

    for p in rdpcap(args.pcap):
        if not (p.haslayer(TCP) and p.haslayer(IP) and p.haslayer(Raw)):
            continue
        t = p[TCP]
        if args.port not in (t.sport, t.dport):
            continue
        load = bytes(p[Raw].load)
        if not load:
            continue
        d = 'C>S' if t.dport == args.port else 'S>C'
        for body in frame_messages(load):
            dst, src, op, cmd, val = decode(body)
            ctrl = '' if op in ('GET', 'SET', 'RPL') else f'  raw={body.hex()}'
            print(f"{d} {op:5} cmd={str(cmd):<6} dst={dst} src={src}  {val}{ctrl}")


if __name__ == '__main__':
    main()
