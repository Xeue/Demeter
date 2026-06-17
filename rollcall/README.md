# rollcall

A native Go client for the Grass Valley / Snell **RollCall** protocol — a drop-in
replacement for shelling out to the Windows-only `RollTrak.exe`.

Import path: `github.com/Xeue/Demeter/rollcall`

**Picking up driver dev?** Start with [`../docs/ROLLCALL_HANDOVER.md`](../docs/ROLLCALL_HANDOVER.md) — current status, prioritized tasks (incl. the gating hardware probe for push), invariants, and how to test.

This package is self-contained (its own `go.mod`) so it can be tested in isolation.
To fold it into the single-module Go rewrite, delete `rollcall/go.mod`; the import
path stays the same.

## Status

| Layer | Confidence | Basis |
|---|---|---|
| Message framing + GET/SET/REPLY codec | **Verified** | byte-exact against a real capture (`message_test.go`) |
| Client request/reply routing, notifies, concurrency, pacing | **Verified** | in-memory pipe tests, `-race` clean |
| TCP transport / port 2050 | **Verified** | capture + independent sources |
| Value kinds = int / string only | **Observed** | all 57 typed messages in capture; unknown kinds now surfaced, not dropped |
| OPEN not required before GET/SET | **Observed** | a SET succeeded before its unit was OPENed |
| Keepalive: re-OPEN ~10s, IDENTITY ~15s | **Observed** | periodic in capture; necessity for staying attached unconfirmed |
| `Open` / ACK round-trip | **Implemented** | OPEN bytes match capture; ACK routed |
| Reference client serialises requests (1 in-flight) | **Observed** | drives `DefaultMaxInFlight = 1` |
| `cmd@<net>:<addr>:<slot>` → `Addr` mapping | **Solved** | `unit=(addr<<8)\|slot`, port 0 — `UnitAddr()`, `TestUnitAddr` |
| Offline/absent-card signal | **Observed** | `0x00` NACK / timeout / `"No Unit Fitted"` |
| "take"/commit semantics | **Assumed** | implemented as `Set(takeCmd, 1)`; confirm on first live write |

See [`../docs/ROLLCALL_GAPS.md`](../docs/ROLLCALL_GAPS.md) for the prioritised
open-questions/next-steps list.

See [`../docs/ROLLCALL_PROTOCOL.md`](../docs/ROLLCALL_PROTOCOL.md) for the full
reverse-engineering notes and [`../tools/rollcall-decode.py`](../tools/rollcall-decode.py)
to decode further captures.

## Usage

```go
ctx := context.Background()
c, err := rollcall.Dial(ctx, "10.40.44.10") // frame IP, port 2050
if err != nil { log.Fatal(err) }
defer c.Close()

unit := rollcall.Addr{Net: 0, Unit: 0x1005, Port: 0x0001} // a card behind the frame

// Read Ethernet-1 IP (command 4101)
v, err := c.Get(ctx, unit, 4101)
fmt.Println(v.Str) // "10.100.44.12"

// Read mode (command 4108): integer
mode, _ := c.Get(ctx, unit, 4108)
fmt.Println(mode.Int) // 1 = Static

// Write a new IP, then commit with the take command
c.Set(ctx, unit, 4101, rollcall.Str("10.100.44.20"))
c.Take(ctx, unit, 4051)

// Read many params from one card at once (respects the in-flight limit; one
// unreadable param won't fail the batch)
vals, errs := c.BatchGet(ctx, unit, []uint32{4101, 4103, 4105, 4108, 4128})

// Attach to a card for keepalive/subscription (not required for GET/SET)
c.Open(ctx, unit)

// Unsolicited updates (e.g. PTP offset) arrive here
go func() {
    for m := range c.Notify() {
        fmt.Printf("update cmd=%d %s\n", m.CmdID, m.Value)
    }
}()
```

Concurrency is paced by `WithMaxInFlight` (default 1, matching the reference
client). The natural port of Demeter's `getInfo()` batch is `BatchGet`; raise the
limit once hardware testing confirms the frame tolerates pipelining.

## How this replaces Demeter's current device layer

In the existing TS app, all device I/O goes through `getInfo()` (GET) and
`doCommands()` (SET + take) which build `rolltrak` command strings and parse the
tab-separated stdout. The Go equivalents map 1:1:

| Demeter (main.ts) | rollcall (Go) |
|---|---|
| `rolltrak -a IP cmd@0000:addr:slot?` | `Client.Get(ctx, unit, cmd)` |
| `rolltrak -a IP cmd@0000:addr:slot=val` | `Client.Set(ctx, unit, cmd, val)` |
| `rolltrak -a IP take@...=1` | `Client.Take(ctx, unit, takeCmd)` |
| `parseTrackData()` (col 5/6/7) | `Decode()` (CmdID / int / string) — gone, native |
| one `rolltrak` process per command | one persistent `Client` per frame |

The command IDs in `commandsDB.json` are used unchanged (e.g. 4101=IP, 4108=Mode,
4128="UP", 38003=LLDP port) — they are the same IDs that appear on the wire.

## Addressing (solved)

The RollTrak CLI form `cmd@<net>:<addr>:<slot>` maps to the wire address as:

```
net  -> Addr.Net   (passthrough; only 0x0000 seen)
unit  = (addr << 8) | slot
port  = 0           (unconnected; a session handle in connected mode)
```

Use `UnitAddr(addr, slot)` to build it:

```go
unit := rollcall.UnitAddr(0x12, 0x05) // card in slot 5 behind a frame (controller addr 0x12) -> 0x1205
card := rollcall.UnitAddr(0x30, 0x00) // a card addressed on its own IP                        -> 0x3000
```

Confirmed against capture: `@12:00`→`0x1200`, `@12:05`→`0x1205`, `@30:00`→`0x3000`.

## Confirm on first live run (no capture needed)

- **Offline/absent card:** the wire signals are now known — `0x00` NACK
  (present-but-unreadable), a bare timeout (absent unit), and `"No Unit Fitted"` from the
  slot-type query. The reader still only resolves on REPLY today, so to avoid an offline
  param blocking to its context deadline, fail fast on a NACK once you've seen how it
  behaves under the connected session (the NACK carries no command id, so with
  `MaxInFlight=1` attribute it to the in-flight request).
- **Keepalive:** OPEN re-sent ~10s / IDENTITY ~15s were *observed*; whether the frame
  drops an idle connection without them is unconfirmed. `Open` is provided — drive it on a
  ticker from the per-frame owner if needed.
- **"take":** implemented as `Set(takeCmd, 1)`; confirm a real write applies (the take
  *policy* — which commands need it — is already in Demeter's code).
- **Value kinds:** only int/string seen; unknown kinds are surfaced as `KindUnknown`
  (not dropped) so a float/enum would show up rather than vanish.

## Test

```sh
cd rollcall
go test -race ./...
```
