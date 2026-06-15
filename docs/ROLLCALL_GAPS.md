# RollCall integration — open questions & next steps

Status of the native Go `rollcall` package vs. the gaps raised in review. Updated
after mining `Rollcall.pcapng` (a RollCall Control Panel session) harder.

Legend: ✅ resolved · 🟡 partially resolved (evidence, needs a hardware check) ·
🔴 needs hardware.

## Resolved from the existing capture (no hardware needed)

| # | Question | Answer | Where |
|---|---|---|---|
| B.5 | Value kinds beyond int/string? | Only int(1)/string(2) across 57 typed msgs. Unknown kinds now surfaced as `KindUnknown` instead of dropped. | `message.go`, `TestDecodeUnknownKind` |
| D | Is the string "reserved u32" always 0? | Yes — all 42 string replies. | mining |
| A.2a | Must a unit be OPENed before GET/SET? | **No** — a `SET 48729` succeeded before its unit was OPENed. | mining |
| C.7 | How many requests in-flight? | Reference client was strictly serial (max 1). Drove `DefaultMaxInFlight = 1` + `BatchGet` pacing. | `client.go` |
| — | OPEN/ACK round-trip | Implemented (`Open`); OPEN bytes match capture, ACK routed. | `client.go`, `TestClientOpen` |
| **A.1** | CLI→wire address mapping | **SOLVED** from a 2nd capture: `unit = (addr<<8)\|slot`, net passthrough, port 0. Both forms (direct addr `30`, via-frame addr `12`). | `message.go` `UnitAddr()`, `TestUnitAddr` |
| **B.3** | Offline/absent-card signal | `0x00` NACK (present-but-unreadable), bare timeout (absent), `"No Unit Fitted"` from the slot-type query. | 2nd capture |
| — | Unconnected (RollTrak) vs connected mode | RollTrak uses one-shot `0x0b`/`0x0c` (uint16 fields, port 0); Control Panel uses connected `0x45`/`0x47` (uint32). Library implements connected. | `ROLLCALL_PROTOCOL.md` |

## Partially resolved — evidence, but verify on hardware

| # | Question | Evidence so far | The check to run |
|---|---|---|---|
| A.2b | Is keepalive required? | OPEN re-sent ~10s, IDENTITY ~15s, per unit. | Hold a connection idle >30s with no Opens; see if the frame drops it. |
| B.4 | take/commit semantics | No take in capture; SETs to 4101/48729 applied directly. `4114` seen only as a GET. | Capture a real Demeter "blast" that changes a param needing a take. |
| C.6 | Can you subscribe instead of poll? | Despite 7 OPENs, only PTP (37139) pushed; IP/mode/link never notified. So push is **partial**. | OPEN a unit, change a param with another tool, watch for a notify. |

## Needs hardware — gating

**None remaining.** Both former blockers (A.1 addressing, B.3 offline signal) were
resolved from the second capture (`Capture for sam.pcapng`). No further captures are
required to build the library.

## Confirm on first live run (no capture needed)

These don't block the build and don't need a capture — they're validated the first time
the Go app talks to a real frame:

- **take/commit (B.4)** — `Take` = `Set(takeCmd, 1)`; SET encoding is fully observed and
  the take *policy* is already in Demeter's code. Confirm a real write applies.
- **connect handshake (A.2b)** — whether a bare connection needs an IDENTITY/login before
  GET works (I/O without a per-unit OPEN was observed). Captured login bytes can be
  replayed via `Client.Send` if needed.
- **push/subscribe (C.6)** — only PTP pushed in captures, so the port keeps polling; treat
  notifies as a bonus.

## Decisions already taken (documented, not bugs)

- **Reconnect** is the per-frame owner's job. The `Client` closes on the first read
  error and does not auto-redial; the actor that owns a frame should redial with
  backoff and re-Open units.
- **Pacing** defaults to 1 in-flight (safest, matches the reference). Raise with
  `WithMaxInFlight` after #C.7 is confirmed on hardware.

## Minor / sharp edges (low priority)

- Two outstanding GETs for the *same* `(unit, cmd)` are matched FIFO, so a reply or
  notify could pair to the "wrong" caller. Harmless in Demeter's scan pattern (it
  reads each param once per cycle); revisit only if that assumption changes.
- `Decode` rejects malformed framing (`ErrMalformed`) and partial buffers
  (`ErrShort`); `readMessage` resyncs only on the 4-byte header, not mid-stream.
