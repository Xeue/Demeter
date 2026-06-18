# RollCall protocol guide

How Demeter talks to Grass Valley / Snell UCP and IQ cards over RollCall, as
implemented in the `rollcall/` package and the `internal/device/` seam. This
replaces the old `RollTrak.exe` shell-outs with one native TCP client per frame.

The wire format was reverse-engineered from two packet captures (a RollCall
Control Panel session and a RollTrak session) and, for the parts noted below,
confirmed against hardware. The codec is byte-exact tested in
`rollcall/message_test.go` and `rollcall/unconnected_test.go`.

## Transport

- TCP. The frame/gateway listens on port 2050 (`config.RollcallPort`, 0 means
  2050). Some gateways also expose 2051 for the local chassis only.
- One long-lived connection per frame IP multiplexes every unit (card) behind
  the frame; each message carries its own source and destination address. This
  matches the old `rolltrak -a <frameIP>` model: one socket per frame.
- All multi-byte integers are big-endian.

## Connected vs unconnected mode

RollCall has two dialects on the same TCP port. Both are implemented
(`rollcall.Mode`, selected per dial); Demeter defaults to unconnected.

| | Connected (Control Panel) | Unconnected (RollTrak) |
|---|---|---|
| Session | Persistent. Client announces itself, may OPEN units, keepalive. | None. One broadcast login per connection. |
| Login / announce | IDENTITY_SELF `0x21` then IDENTITY_UNIT `0x14` | broadcast login `0x15` then IDENTITY_UNIT `0x14` |
| GET / SET / REPLY | `0x45` / `0x46` / `0x47` | request `0x0b` / reply `0x0c` |
| Command id and dataType width | uint32 | uint16 |
| Address port | non-zero session handle | 0 |
| Error | not yet observed | NACK `0x00` (carries no command id) |
| Unsolicited push (notify) | yes (`0x47` with flag `0x80`) | no |

Demeter uses unconnected mode (`config.RollcallMode: "unconnected"`) for two
reasons: its addressing model matches it (port 0, `unit=(addr<<8)|slot`, and the
frame-controller read at `addr=00 slot=00` only makes sense unconnected), and
RollTrak, the legacy client that worked in the field, used it. Connected mode is
implemented and selectable (`"connected"`) but its session-handle addressing and
connect handshake are not fully confirmed on hardware, and the only benefit it
adds is push, which is not yet wired in (see Further work).

In unconnected mode the client sends the `0x15` login itself inside `Dial`
(`client.go`, `sendLogin`). In connected mode an optional IDENTITY_SELF login is
sent on connect, gated by `config.RollcallHandshake` (default off).

## Message framing

```
00 0C | outerLen (u16) | dst (6 bytes) | src (6 bytes) | innerLen (u16) | inner
\_ magic _/
```

- `outerLen` is the number of bytes after the 4-byte `00 0C <outerLen>` header,
  i.e. `12 + 2 + innerLen`.
- Address (6 bytes) is `net(u16) : unit(u16) : port(u16)`. On a reply the device
  swaps dst and src (request `dst=unit src=client`, reply `dst=client src=unit`).
- `Encode`/`Decode` in `rollcall/message.go` implement this. `Decode` returns
  `ErrShort` on a partial buffer and `ErrMalformed`/`ErrMagic` on a bad frame.

## Addressing

A unit's wire address is derived from the RollTrak CLI form `cmd@<net>:<addr>:<slot>`:

```
unit = (addr << 8) | slot
net  = 0
port = 0          (unconnected; connected uses a session-handle port)
```

`addr` is the controller's hex address, `slot` is the card slot.
`rollcall.UnitAddr(addr, slot uint8)` builds it. Examples:

- A card in slot 5 behind a frame controller at `0x12`: `UnitAddr(0x12, 0x05)` =
  unit `0x1205`.
- A card addressed directly on its own IP uses controller `0x30`:
  `UnitAddr(0x30, 0x00)` = unit `0x3000`.

This mapping is confirmed against a second capture (`@0000:12:00` to `0x1200`,
`@0000:12:05` to `0x1205`, `@0000:30:00` to `0x3000`). The `internal/device`
seam owns the conversion (`addr.go`, `AddrMapper`); the scan layer speaks hex
`addr`/`slot` strings and never sees a `rollcall.Addr`. Note the slot is hex on
the wire but decimal (`%02d`) in `frame.Slots`.

## Inner payload and opcodes

```
byte 0    : opcode
byte 1    : flags        (0x00 normal, 0x80 unsolicited notify)
byte 2..  : command id   (uint32 connected, uint16 unconnected) for data ops
...       : value         for SET and REPLY
```

| Opcode | Name | Mode | Direction | Meaning |
|---|---|---|---|---|
| `0x45` | GET | connected | C to S | read a parameter |
| `0x46` | SET | connected | C to S | write a parameter |
| `0x47` | REPLY / NOTIFY | connected | S to C | current value (solicited or, with flag `0x80`, a push) |
| `0x1c` | OPEN | connected | C to S | attach to / subscribe a unit |
| `0x01` | ACK | connected | S to C | acknowledges an OPEN |
| `0x14` | IDENTITY_UNIT | both | S to C | unit announces its name |
| `0x21` | IDENTITY_SELF | connected | C to S | client announces its name |
| `0x0b` | request | unconnected | C to S | read (and, best-guess, write) |
| `0x0c` | reply | unconnected | S to C | current value |
| `0x15` | login | unconnected | C to S | broadcast login |
| `0x00` | NACK | unconnected | S to C | error; no command id |

## Value encoding

Connected (`decodeConnectedValue`):

```
dataType (u32): 1 = int, 2 = string
  int   : value (u32)
  string: reserved (u32, =0) then NUL-terminated ASCII
```

Unconnected (`decodeUnconnectedValue`):

```
dataType (u16): 1 = int, 2/3 = string
  int   : value follows the dataType directly, no reserved word (u32, or u16 for a 2-byte body)
  string: reserved (u32) then NUL-terminated ASCII
```

The missing reserved word on an unconnected int is load-bearing: parsing it as
if it had one made enum/select parameters read as "undefined". dataType 3 is the
"No Unit Fitted" status string for an empty slot.

Only int and string have been seen. Any other dataType decodes to `KindUnknown`
(raw type and bytes preserved) rather than being dropped, so a float/enum is
detectable instead of looking like "no value".

## Take / commit

Some writes need a follow-up "take" to apply. `Client.Take(unit, takeCmd)` is
`Set(takeCmd, 1)`. Which commands need a take is policy that already lives in
Demeter's command DB. In captures SETs applied directly without a take; confirm
the take path on the first real write.

## Worked examples

From the connected-mode capture:

```
GET 4101  -> REPLY 4101 str "10.100.44.12"      # Ethernet-1 IP
SET 4101 str "10.100.44.11" -> REPLY 4101 str "10.100.44.11"
GET 4108  -> REPLY 4108 int 1                    # mode = Static
GET 4128  -> REPLY 4128 str "UP"                 # link state
GET 38003 -> REPLY 38003 str "Ethernet3/13/1"    # LLDP neighbour port
```

Raw bytes for `GET 4108` (cmd `0x100c`), connected mode:

```
00 0c | 00 14 | 00 00 10 05 00 01 | 00 00 00 00 00 05 | 00 06 | 45 00 00 00 10 0c
magic | outer | dst net:unit:port  | src net:unit:port  | inner | GET pad cmd
```

## How the code is laid out

- `rollcall/` is the protocol client, with no Demeter dependencies: framing and
  codec (`message.go`), and the persistent per-frame `Client` (`client.go`) with
  `Get`/`BatchGet`/`Set`/`Take`/`Open`/`Send`/`Notify`/`Close`. A reader
  goroutine routes each reply to its waiter by `(src addr, command id)`;
  unmatched replies go to the `Notify` channel. Requests default to one
  in-flight (`DefaultMaxInFlight = 1`), matching the reference client.
- `internal/device/` is the seam between the scan/blast logic and `rollcall`.
  The scan layer depends only on `Device`/`Dialer` and Demeter's hex
  `addr`/`slot` tokens. `rollcall_device.go` is the real dialer (per-GET timeout,
  batch concurrency cap, dial timeout, mode/opcode/port from config);
  `addr.go` is the address mapping; `session.go` is the gated connect handshake;
  `offline.go` classifies GET errors into `ErrUnitOffline` /
  `ErrFrameUnreachable` / `ErrFrameNoResponse`.
- Config knobs (`internal/config/config.go`): `RollcallMode` (`"unconnected"`
  default / `"connected"`), `RollcallPort` (0 means 2050), `RollcallHandshake`
  (connected-mode login, default off), `RollcallTimeoutMs` (per-GET timeout, 0
  means 2000), `RollcallSetOpcode` (unconnected SET opcode hex, default `0b`),
  `ScanIntervalSeconds`.

## What is confirmed vs open

Confirmed (capture and/or hardware):

- Framing and the GET/SET/REPLY codec, both modes (byte-exact tests).
- Addressing `unit=(addr<<8)|slot`, port 0 (second capture).
- Unconnected reads, including the int-with-no-reserved layout and the "No Unit
  Fitted" status.
- The connected-mode persistent client and reply routing (in-memory tests).

Open (see Further work):

- The unconnected SET opcode. No write was ever captured. The value body is
  grounded (it mirrors the captured `0x0c` reply), but the opcode is a best
  guess (`0x0b`), configurable via `RollcallSetOpcode`.
- The offline wire signal. A `0x00` NACK and the "No Unit Fitted" string were
  observed, but `offline.go` still infers offline from the per-GET timeout.
- Connected-mode connect handshake and per-unit OPEN + keepalive. The login is
  coded and gated; `Client.Open` exists but nothing calls it on a cadence.
- Push/notify coverage. `Client.Notify()` exists but nothing drains it, and
  `Device` has no `Notify()`. The only parameter seen pushing was PTP offset
  (cmd 37139), which Demeter does not manage; managed params were not seen to
  push.
- Take/commit, error/NACK frames in connected mode, and value kinds beyond
  int/string (currently `KindUnknown`).

# Further work

Ordered by dependency. The push feature and final confidence in the handshake
and offline signal all rest on a real frame; do the probe first.

## Probe real hardware (gating)

Confirm on a non-air frame, ideally with a second controller making changes:

1. Which managed params push an unsolicited `0x47`+flag-`0x80` (IP 4101, link/IO
   4108/4128, a shuffle/route), and whether the direct-to-card unit (`0x3000`)
   also pushes. `cmd/rcprobe` (`make probe`) dials a frame and logs notifies.
2. The unconnected SET opcode: set a param while sniffing TCP/2050 and match the
   bytes, or try candidates against the frame and let the blast verify-and-retry
   report which one applies.
3. Whether connected mode needs the IDENTITY login and per-unit OPEN before GETs
   are answered, and the OPEN cadence that keeps notifies flowing.

Decision gate for push: if managed params do not push, push is near-zero benefit
and polling stays as-is. If they do, build the push integration below.

## Push / notify (hybrid, only if the probe passes)

Push is for freshness; a slower reconcile poll stays the source of truth. Do not
remove polling: notifies only fire on change (initial and post-reconnect state
is unknown), never-pushing params are invisible, `notifyCh` is lossy with no
gap detection, and the client does not auto-reconnect. A realistic win is
raising the scan interval from 3s to 30 to 60s with push covering the gap
(`ScanIntervalSeconds` is already runtime-settable).

Route every notify through the per-frame actor mailbox so `model.Slot.Active` is
only ever written on the actor goroutine (this preserves the scan-loop-race
fix). Outline:

1. Add `Notify() <-chan Update` to `device.Device` in Demeter terms (no
   `rollcall.Addr` past the seam), plus `ReverseAddr` (inverse of `UnitAddr`).
2. Per device, drain `Client.Notify()`, reverse-map the source address, and
   forward `device.Update{IP, addr, slot, cmd, value}` on a lossy channel.
3. Fan per-IP notify channels into one per-actor channel tagged with source IP
   (needed for the direct-to-card path); stop forwarders on prune/close.
4. In the actor, on a notify, resolve the slot (hex to decimal for a frame conn;
   for a `0x3000` own-IP notify, look the slot up by card IP, drop if not yet
   learned), set `sl.Active`, emit one `SlotInfo`, mark changed. Do not blast
   inline; leave any re-SET to the next poll so all writes share one diff/verify
   path.

## Finish the connected session (OPEN + keepalive)

`session.go` only replays IDENTITY_SELF on connect. Add per-unit OPEN for the
units an actor watches, a keepalive ticker re-OPENing them, and reconnect with
backoff (the client closes on the first read error and does not redial; that is
the owner's job). Keep it gated by `RollcallHandshake` and validate on a non-air
frame.

## Wire the offline signal into the device

Surface the `0x00` NACK as a typed error from `rollcall.Client`, map it to
`ErrUnitOffline` in `classifyGetErr`, and rely on the "No Unit Fitted" sentinel
in `IsAbsent`, so an absent card fails fast instead of eating the per-GET timeout
across a 298-parameter batch. Confirm the exact bytes on hardware.

## Protocol completeness

Confirm on hardware: the take path (`Set(takeCmd, 1)`), connected-mode
error/NACK frames, and value kinds beyond int/string (float/enum currently
decode to `KindUnknown`). Capture an unconnected write if that path is ever
needed.

## Invariants (do not regress)

- Single-writer actor. `model.Slot.Active` and frame state are mutated only on
  the actor goroutine. A notify consumer posts a message to the actor; it never
  mutates from the reader goroutine.
- Seam discipline. `rollcall.Addr` and `rollcall.Value` do not leak past
  `internal/device`. The scan and actor layers see only `model.Value` and hex
  `addr`/`slot`.
- Slot key is hex on the wire, decimal `%02d` in `frame.Slots`.
- A direct-to-card notify (unit `0x3000`) does not carry the slot in its address;
  the slot is implied by which card IP delivered it, so always carry the source
  IP on an update.
- On-air safety. The OPEN/keepalive path talks to live hardware; keep it gated
  and validate on a non-air frame.

## Tools

- `tools/rollcall-decode.py` decodes a `.pcapng` into a readable transcript
  (needs scapy). Grep for `[notify]` to find pushes.
- `cmd/rcprobe` (`make probe`) dials a specific frame to confirm reachability and
  log notifies.
- `rollcall/README.md` is the package-level status and usage reference.
