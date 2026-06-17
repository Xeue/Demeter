# RollCall driver — handover

Everything you need to pick up development of Demeter's native RollCall driver.
Read this, then [`ROLLCALL_PROTOCOL.md`](ROLLCALL_PROTOCOL.md) (wire format) and
[`../rollcall/README.md`](../rollcall/README.md) (package status table).

## Mission

Demeter v2 talks to Grass Valley / Snell **UCP/IQ** broadcast cards over the
**RollCall** protocol (TCP/2050), replacing the old `RollTrak.exe` shell-outs.
The "driver" is two layers:

- **`rollcall/`** — the pure protocol client (framing, GET/SET/REPLY codec,
  connection, notifies). Self-contained, its own tests, no Demeter deps.
- **`internal/device/`** — the **seam** between Demeter's scan/blast logic and
  `rollcall`. The scan layer depends only on `device.Device`/`device.Dialer`
  and speaks Demeter's hex `addr`/`slot` tokens, never `rollcall.Addr`.

The driver works for **polling** today (the app scans every frame on a timer).
The open frontier is **push** (receive card changes as they happen) plus three
seams that are coded but only **partially hardware-confirmed**.

## Orient fast (key files)

| File | What it is |
|---|---|
| `rollcall/message.go` | Wire codec: `Encode`/`Decode`, `Addr{Net,Unit,Port}`, `UnitAddr(addr,slot uint8)`, `Value{Kind,Int,Str}`, opcodes, `FlagNotify=0x80`. **Byte-exact tested** (`message_test.go`). |
| `rollcall/client.go` | Persistent per-frame TCP client. `Get`/`BatchGet`/`Set`/`Take`/`Open`/`Send`/`Notify`/`Close`. `readLoop` routes replies to waiters; unmatched replies → `notifyCh`. |
| `internal/device/device.go` | `Device`/`Dialer` interfaces (the seam). `fromRollcall`/`toRollcall` value conversion. **No `Notify()` yet.** |
| `internal/device/rollcall_device.go` | `RollcallDialer` (real impl): per-GET timeout, batch concurrency cap, dial timeout. |
| `internal/device/addr.go` | **SEAM #1** — `AddrMapper` (CLI `addr:slot` → `rollcall.Addr`). **Resolved.** |
| `internal/device/session.go` | **SEAM #2** — connect handshake (IDENTITY replay). Gated by `Handshake`. **No per-unit OPEN/keepalive yet.** |
| `internal/device/offline.go` | **SEAM #3** — offline detection (`classifyGetErr`, `ErrUnitOffline`/`ErrFrameUnreachable`/`ErrFrameNoResponse`, `IsAbsent`). Heuristic; wire signal unconfirmed. |
| `internal/device/fake_device.go` | In-memory `Device`/`Dialer` for tests (seed GETs, record SETs, offline, concurrency peak). |
| `internal/scan/scan.go` | Uses the device to scan/blast: `getFrameAddress` (params 17044/16482), `scanSlot`, `checkCard`. |
| `internal/frame/{actor,conns}.go` | One actor goroutine per frame owns its state + a `connCache` of persistent devices. |
| `tools/rollcall-decode.py` | Decodes a `.pcapng` into a readable transcript (needs scapy). |
| `Rollcall.pcapng` | The one capture we have (gitignored — local only). One Control-Panel session. |

## Status (what's solid vs open)

| Area | Status |
|---|---|
| Framing + GET/SET/REPLY codec | **Verified** byte-exact vs capture (`message_test.go`) |
| Persistent connected-mode client, reply routing, `-race` clean | **Verified** (in-memory pipe tests) |
| SEAM #1 addressing `unit=(addr<<8)\|slot` | **Confirmed** vs a 2nd capture |
| SEAM #2 connect handshake (IDENTITY_SELF replay) | **Coded, gated** (`RollcallHandshake`), best-effort from capture — unproven live |
| SEAM #2 per-unit OPEN + keepalive | **Not built** — `Client.Open()` exists, nothing calls it on a cadence |
| SEAM #3 offline wire signal | Signal now **observed** (`0x00` NACK / `"No Unit Fitted"`, per `rollcall/README.md`), but `offline.go` still uses the per-GET-timeout heuristic — wire the real signal in (Task C) |
| Push / notify (receive changes) | **Half-built**: `Client.Notify()` exists, nothing drains it; `Device` has no `Notify()` |
| "take"/commit, error/NACK frames, float/enum value kinds | **Assumed/unseen** — confirm on hardware |

## ⚠ Open blocker — connectivity ("Cannot reach frame" on real hardware)

Field report: a frame that worked with the old `RollTrak.exe` reports "Cannot reach
frame" on Demeter v2. Re-dissecting `Rollcall.pcapng` exposes a **mode/addressing
mismatch**, not a missing handshake:

- The Go client speaks **connected** mode (opcodes `0x45/0x46/0x47`). In the
  capture, connected-mode GETs address the card as `0000:1005:`**`0001`** — a
  **non-zero** port — and the client `OPEN`s each unit first. Our `UnitAddr`
  produces **port 0**.
- Demeter's addressing is **unconnected-mode** throughout: `unit=(addr<<8)|slot`,
  **port 0**, and `getFrameAddress` reads at `addr=00 slot=00` → unit `0x0000`,
  which in *connected* mode is the client's own address space (not the frame
  controller, which lives at `0x10xx`). That `00:00` only makes sense unconnected.
- rolltrak (which worked) used **unconnected** mode (`0x0b/0x0c`, uint16, port 0).

So we're sending connected-mode opcodes with unconnected-mode addressing → the
frame answers nothing → "Cannot reach frame". The client was only ever verified
against `FakeDevice`, never real hardware.

**RESOLVED (pending a live retest):** the second capture (`Capture for sam.pcapng`,
RollTrak/unconnected) gave the exact unconnected wire format, so **both modes are
now implemented** in `rollcall` and selectable:

- `rollcall.WithMode(Unconnected)` — `0x15` login (auto-sent on Dial), GET `0x0b`
  with uint16 cmd, REPLY `0x0c` (cmd/dtype uint16 + reserved u32 + value), `0x00`
  NACK, **port-0 addressing** (`unit=(addr<<8)|slot`). All byte-exact tested
  against the capture (`rollcall/unconnected_test.go`).
- `rollcall.WithMode(Connected)` — the original Control-Panel dialect (unchanged).
- App default is **unconnected** (`config.RollcallMode`, via `device.ParseMode`),
  because that is what Demeter's addressing matches and what rolltrak used.
- dataType **3 = "No Unit Fitted"** (absent slot) is decoded and added to the
  offline sentinels (`offline.go`) — SEAM #3 partly resolved.

**Still open — unconnected WRITE (best-guess, configurable):** no unconnected
write was captured. The value *body* is well-grounded (it mirrors the captured
`0x0c` reply: `dataType u16 + reserved u32 + value`); the *opcode* is the unknown.
Default best guess is `0x0b` (the read-request opcode reused with a value body);
the opcode is **configurable without a rebuild** via `config.RollcallSetOpcode`
(hex, e.g. `"0d"`) → `device.ParseSetOpcode` → `rollcall.WithUnconnectedSetOpcode`.
Try candidates on a **non-air** frame and let the blast verify-and-retry tell you
which applies. To settle it for good, capture one `RollTrak.exe` *write* (set a
param while sniffing TCP/2050) and match the bytes. `cmd/rcprobe` (`make probe`)
confirms any specific frame.

## Tasks, prioritized

### Task 0 — GATING hardware probe (do this FIRST, ~½ day)

The whole push feature, and final confidence in seams #2/#3, rest on real
hardware. Our only capture starts mid-session (no TCP SYN) and the **only**
parameter ever seen pushing was cmd **37139 (PTP offset)** — self-jittering
telemetry Demeter does *not* manage. The one managed param that changed during
the capture (cmd 4101, IP) got an ordinary **solicited** reply, not a notify. So
"push exists" is proven; "push covers the params we manage" is **not**.

Build a tiny debug mode (suggest `cmd/demeter --rollcall-probe <frameIP>` or a
throwaway `cmd/rcprobe`) that: dials the frame, runs the SEAM #2 handshake,
`Open`s a real card unit (e.g. `UnitAddr(0x12,0x05)`), then `slog`s every
`Client.Notify()` message. Run it against a **non-air** frame and, from a
**separate** controller / front panel, change managed params (IP 4101, link/IO
4108/4128, a shuffle/route). Record, per cmd id, **whether it pushes flag-0x80
unsolicited**, the OPEN cadence needed to keep notifies flowing, and whether the
card's own-IP connection (unit `0x3000`) also pushes.

**Decision gate:** if managed params don't push → push is near-zero benefit;
stop and keep polling. If they do → build Task A.

### Task A — push/notify integration (hybrid; only if Task 0 passes, ~4–7 days)

Architecture: **push for freshness + a slow reconcile poll that stays the source
of truth** (do NOT remove polling — see "Why poll stays"). Funnel every notify
through the per-frame **actor mailbox** so `model.Slot.Active` is mutated only on
the actor goroutine (preserves the scan-loop-race fix).

1. `device.go`: add `type Update struct { IP, Addr, Slot string; Cmd uint32; Value model.Value }` and `Notify() <-chan Update` to `Device` (Demeter terms — no `rollcall.Addr` leaks past the seam).
2. `addr.go`: add `ReverseAddr(rollcall.Addr) (addr, slot string)` — inverse of `UnitAddr` (`addr=Unit>>8` as `%02x`, `slot=Unit&0xff`). **Trap:** wire slot is **hex** but `frame.Slots` is keyed **decimal `%02d`** — convert, or you silently miss the slot.
3. `rollcall_device.go`: one goroutine per device drains `c.Notify()`, reverse-maps `m.Src`, forwards `device.Update{IP: frameIP, …}` on an owned buffered (lossy + dropped-counter) channel; stop on `Close()`.
4. `conns.go`: fan every per-IP device's `Notify()` into one per-actor channel **tagged with source IP** (needed for the direct-to-card path); stop forwarders on `Prune`/`closeAll` (no goroutine leaks); don't prune subscribed conns.
5. `actor.go`: add `case u := <-a.conns.Notifies():` → resolve slot (hex→decimal for frame-conn; for own-IP `0x3000` notifies look up slot by IP from `sl.IPA`/`sl.IPB` learned during scan, **drop if not yet learned**), set `sl.Active[strconv(cmd)]=value`, emit **one** `Events.SlotInfo` per touched slot, `a.changed()`. Drain-and-coalesce bursts. **Do not blast inline** — leave any re-SET to the next poll/`PollNow` so all writes stay on one diff/verify path.
6. `session.go` (also Task B): `Open` each discovered UCP unit after scan, re-`Open` on a ~10s keepalive ticker — this is what makes the frame push. Keep gated by `Handshake`/`Keepalive`.

**Why poll stays (each independently fatal to pure-push):** notifies only fire on *change* (initial + post-reconnect state is unknown); never-pushing params are invisible; `notifyCh` is buffered and **drops silently** with no protocol gap-detection; `Client` does **not** auto-reconnect. Realistic win: raise scan interval 3s → 30–60s with push covering the gap (interval is already runtime-settable via `SetScanInterval`).

### Task B — finish SEAM #2 (per-unit OPEN + keepalive)

`session.go` currently only replays IDENTITY_SELF on connect. Add: `Open` each
unit the actor watches; a ~10s keepalive ticker re-`Open`ing them; reconnect +
backoff (the client closes on first read error and does not auto-redial — owner's
job). Validate on a non-air frame. Keep gated by `RollcallHandshake` until proven.

### Task C — wire SEAM #3 offline signal into the device

The wire signal is now **observed** (per `rollcall/README.md`: a `0x00` NACK and/or
a `"No Unit Fitted"` string), but `offline.go` still infers offline only from the
per-GET timeout. Surface the NACK as a typed error from `rollcall.Client`, map it
to `ErrUnitOffline` in `classifyGetErr`, and add `"No Unit Fitted"` to
`legacySentinels`/`IsAbsent`, so absent cards fail fast instead of eating the
per-GET timeout ×298. Confirm the exact bytes/string on hardware.

### Task D — protocol completeness

Confirm on hardware: "take" really is `Set(takeCmd,1)`; error/negative replies
(connected mode NACK not yet seen); value kinds beyond int/string (float/enum →
currently `KindUnknown`). The unconnected-mode **write** form was never captured
(only reads) if you ever need that path.

## How to test / work

- **Decode a capture:** `python3 -m venv /tmp/rcv && /tmp/rcv/bin/pip install scapy && /tmp/rcv/bin/python tools/rollcall-decode.py Rollcall.pcapng`. Grep the output for `[notify]` to find pushes.
- **Unit tests:** `go test -race ./rollcall/... ./internal/device/... ./internal/scan/...`. Use `device.FakeDevice`/`FakeDialer` to drive scan/actor logic with no hardware. Add a `Notify()` to the fake when you add it to the interface.
- **Golden tests:** keep the byte-exact codec tests (`message_test.go`) green for any framing change; capture real `rolltrak`/Control-Panel bytes as fixtures when you confirm new opcodes.
- **Run against a frame:** set `rollcallHandshake: true` (and `rollcallPort`/`rollcallTimeoutMs` if needed) in `~/Documents/DemeterData/config.json`, then `go run ./cmd/demeter --log-level D`.

## Invariants / gotchas (don't regress these)

- **Single-writer actor.** `model.Slot.Active` and frame state are mutated only on
  the actor goroutine. A notify consumer must post a message to `actor.in`, never
  mutate from the reader goroutine — that reintroduces the documented scan-loop race.
- **Seam discipline.** `rollcall.Addr`/`rollcall.Value` must not leak past
  `internal/device`. Scan/actor see only `model.Value` + hex `addr`/`slot`.
- **Slot key:** hex on the wire, **decimal `%02d`** in `frame.Slots` (`scan.go`).
- **Reverse-addr ambiguity:** a frame-conn notify carries the slot in the address;
  a **direct-to-card** notify (unit `0x3000`) does not — the slot is implied by
  *which card IP* delivered it. Always carry the source IP on an `Update`.
- **On-air safety.** SEAM #2 OPEN/keepalive talks to live hardware — keep it gated
  (`RollcallHandshake`/`Keepalive` default off) and validate on a non-air frame.
- **Lossy by design:** `notifyCh` and the hub `SlotInfo` channel drop under load.
  That's why the reconcile poll is mandatory, not optional.

## Pointers

- Wire format + capture notes: [`ROLLCALL_PROTOCOL.md`](ROLLCALL_PROTOCOL.md)
- Package status + usage: [`../rollcall/README.md`](../rollcall/README.md)
- Decoder: [`../tools/rollcall-decode.py`](../tools/rollcall-decode.py)
- Config knobs: `internal/config/config.go` (`RollcallPort`, `RollcallHandshake`, `RollcallTimeoutMs`, `ScanIntervalSeconds`)
