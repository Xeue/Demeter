# RollCall wire protocol — reverse-engineered notes

Goal: replace the Windows-only `RollTrak.exe` that Demeter shells out to with a native
TypeScript client speaking RollCall directly over TCP.

These notes were reverse-engineered from `Rollcall.pcapng` (a single RollCall **Control
Panel** session, client `10.40.40.190` ↔ frame `10.40.44.10`). Confidence is marked per
item. Decode/replay with [`tools/rollcall-decode.py`](../tools/rollcall-decode.py).

## Transport (confidence: HIGH)

- **TCP**, the frame/gateway listens on **port 2050**.
- One long-lived connection **multiplexes many units** (cards) behind the frame — each
  message carries its own source/destination address, so Demeter only needs **one socket
  per frame IP**, exactly like the current `rolltrak -a <frameIP> …` model.
- All multi-byte integers are **big-endian**.

## Message framing (confidence: HIGH)

```
+--------+--------+----------------+------------------+------------------+----------+----------------------+
| 0x00   | 0x0C   | outerLen (u16) | dstAddr (6 bytes)| srcAddr (6 bytes)| innerLen | inner payload        |
|        |        |                |                  |                  | (u16)    | (innerLen bytes)     |
+--------+--------+----------------+------------------+------------------+----------+----------------------+
\___ 2-byte magic ___/  \_ bytes after this field _/
```

- `outerLen` = number of bytes following the 4-byte `00 0C <outerLen>` header
  (i.e. `12 + 2 + innerLen`).
- **Address (6 bytes)** = `net(u16) : unit(u16) : port(u16)`.
  - On a reply the **dst and src are swapped** (request `dst=unit src=client`,
    reply `dst=client src=unit`).
  - In the capture: units are `0x1000 / 0x1004 / 0x1005` with ports `0x0001 / 0x0004 /
    0x0005`; the client uses `net=0 unit=0` with a small handle (`0x0002…0x0005`).
  - **RESOLVED** — the RollTrak CLI form `cmd@<net>:<addr>:<slot>` packs as:

        net  -> Net  (passthrough; only 0x0000 seen)
        unit = (addr << 8) | slot
        port = 0x0000   (unconnected mode)

    Confirmed against a second capture: `@0000:12:00`→unit `0x1200`, `@0000:12:05`→
    `0x1205`, direct-to-card `@0000:30:00`→`0x3000`. (This also explains the first
    capture: `0x1005` = addr `0x10`, slot `0x05`.) In connected/session mode the port
    is a non-zero session handle; in unconnected (RollTrak) mode it is 0. See
    `UnitAddr()` in the Go package.

## Inner payload (confidence: HIGH for data ops)

```
byte 0 : opcode
byte 1 : flags        (0x00 = normal/solicited, 0x80 = unsolicited notify)
byte 2..5 : command ID (u32)        ── for data opcodes only
... value (see below) ──            ── for SET / REPLY only
```

### Opcodes seen

| Opcode | Name             | Direction | Meaning                                             |
|-------:|------------------|-----------|-----------------------------------------------------|
| `0x45` | **GET**          | C→S       | read a parameter (no value body)                    |
| `0x46` | **SET**          | C→S       | write a parameter (value body follows)              |
| `0x47` | **REPLY/NOTIFY** | S→C       | current value of a parameter (value body follows)   |
| `0x1c` | **OPEN/attach**  | C→S       | attach to a unit; body is just `opcode,flags`       |
| `0x01` | **ACK/OK**       | S→C       | acknowledges an OPEN                                 |
| `0x14` | **IDENTITY**     | S→C       | unit announces its name (e.g. `MWA22SVENT02`)        |
| `0x21` | **IDENTITY**     | C→S       | client announces its name (capture: `ControlPanel`) |

### Value encoding (SET / REPLY) (confidence: HIGH)

```
dataType (u32):  1 = integer,  2 = string
  if integer:  value = next u32
  if string :  next u32 (reserved, =0), then NUL-terminated ASCII
```

## Worked examples (straight from the capture)

```
GET 4101  →  REPLY 4101 str "10.100.44.12"     # read Ethernet-1 IP
SET 4101 str "10.100.44.11"  →  REPLY 4101 str "10.100.44.11"   # write it
GET 4108  →  REPLY 4108 int 1                   # Mode = Static  (matches commandsDB.json)
GET 4106  →  REPLY 4106 str "00:23:70:00:85:F9" # MAC
GET 4128  →  REPLY 4128 str "UP"                # link state Demeter reads
GET 38003 →  REPLY 38003 str "Ethernet3/13/1"   # LLDP neighbour switch-port
```

Every command ID above is one Demeter already uses in `commandsDB.json` / `main.ts`.

Raw bytes for `GET 4108` (cmd `0x100c`):
```
000c 0014  0000 100500 01  0000 000005  000e? ...   ← request (outerLen 0x14)
00 0c | 00 14 | 00 00 10 05 00 01 | 00 00 00 00 00 05 | 00 06 | 45 00 00 00 10 0c
magic | outer | dst net:unit:port  | src net:unit:port  | inner| GET pad  cmd=0x100c
```
Reply (`0x47`, int value 1):
```
00 0c | 00 1c | <dst/src swapped> | 00 0e | 47 00 00 00 10 0c 00 00 00 01 00 00 00 01
                                            RPL pad cmd=0x100c  dtype=1   value=1
```

## Session flow (confidence: MEDIUM — capture starts mid-connection)

1. TCP connect to `frame:2050`.
2. Client `0x21` IDENTITY (announce our name) ; units `0x14` IDENTITY back.
3. Per unit: client `0x1c` OPEN → server `0x01` ACK.
4. `0x45 GET` / `0x46 SET` → `0x47 REPLY`. Unsolicited `0x47` (flags `0x80`) arrive as
   values change (e.g. PTP offset on cmd 37139).
5. Periodic `0x1c`/`0x01` per unit (keepalive / re-attach).

## Two transport modes

There are two ways to talk to a frame, both confirmed by capture:

| | Connected (Control Panel) | Unconnected (RollTrak.exe) |
|---|---|---|
| Session | persistent; OPEN/IDENTITY, keepalive ~10s/~15s | one-shot; login per connection |
| GET / SET / REPLY | `0x45` / `0x46` / `0x47` | `0x0b` req / `0x0c` reply |
| Login / announce | `0x21` IDENT_SELF / `0x14` IDENT_UNIT | `0x15` broadcast / `0x14` announce |
| Error | (not yet seen) | `0x00` NACK (no command id) |
| command id & dataType width | **uint32** | **uint16** |
| address port | session handle (non-zero) | `0x0000` |
| pushes (notify) | yes (`0x47` + flag `0x80`) | no |

The Go `Client` implements **connected** mode (persistent + notifies — the better fit
for Demeter's per-frame polling, and the mode with a fully-observed SET). Unconnected
mode is what RollTrak uses; it's fully decoded except the *write* form (only reads were
captured) and is a documented alternative.

Unconnected framing (from the RollTrak capture):

    req  : 0b 00 <cmd u16>
    reply: 0c 00 <cmd u16> <dataType u16> <reserved u32> <value>   (value as in connected mode)
    nack : 00 00            (bare; the in-flight command is implied)
    login: 15 ...  -> unit answers 14 ... with its name

## Still best confirmed live (no capture needed — verify on first run)

- **The "take/commit" step.** Demeter fires a take command (`4051`, `35636`, `50002`)
  after a SET. SETs were seen applying directly; `take` is implemented as `Set(takeCmd,1)`
  and the *policy* (which commands need it) is already in Demeter's code — confirm on the
  first real write.
- **Connect handshake for connected mode** — whether a bare TCP connection accepts a GET
  with no prior IDENTITY/OPEN. I/O without a per-unit OPEN was observed; a login on
  connect may still be needed. The captured login/identity bytes are available to replay.
- Float/enum data types beyond int/string (none seen; surfaced as `KindUnknown` if they occur).

## Sketch of a native client (Node `net`)

```ts
import net from 'net';
// frame an outgoing message
function msg(op: number, dst: Addr, src: Addr, cmdId?: number, value?: number|string): Buffer {
  const inner: number[] = [op, 0x00];
  if (cmdId !== undefined) { inner.push(...u32(cmdId)); }
  if (typeof value === 'number') inner.push(...u32(1), ...u32(value));
  else if (typeof value === 'string') inner.push(...u32(2), ...u32(0), ...Buffer.from(value+'\0'));
  const body = [...addr(dst), ...addr(src), ...u16(inner.length), ...inner];
  return Buffer.from([0x00, 0x0c, ...u16(body.length), ...body]);
}
// connect once per frame IP, route replies by (cmdId) / src addr, expose get()/set() promises.
```

This replaces the entire `getInfo()` / `doCommands()` / `parseTrackData()` shell-out layer
in `main.ts` and removes the Windows dependency for device I/O.
