package scan

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/Xeue/Demeter/internal/device"
	"github.com/Xeue/Demeter/internal/model"
)

// rebootReasons returns one human-readable reason per restart-flagged command in
// `sent` (the commands actually blasted this cycle), e.g.
// "IP (4101): 10.40.44.12 -> 10.40.44.20". Order is stable for the UI and tests.
func rebootReasons(sent map[string]sendCmd, active map[string]model.Value, restart map[uint32]string) []string {
	if len(restart) == 0 || len(sent) == 0 {
		return nil
	}
	var reasons []string
	for command, c := range sent {
		name, ok := restart[uint32(mustAtoi(command))]
		if !ok {
			continue
		}
		from := "unset"
		if av, has := active[command]; has && !av.IsNone() {
			from = av.String()
		}
		reasons = append(reasons, fmt.Sprintf("%s (%s): %s → %s", name, command, from, c.value.String()))
	}
	sort.Strings(reasons)
	return reasons
}

// sendCmd is a command queued to send to a device.
type sendCmd struct {
	value model.Value
	typ   string
}

// buildCommands ports the blast-diff (main.ts:655-723): a group pass then a
// prefered pass that overrides/cancels, routing each command to the frame, a
// shuffle, or the card, and accumulating take ids. checkNull (set when the card
// could not be read) inverts the skip rule.
func buildCommands(sl *model.Slot, checkNull bool) (frameCommands, cardCommands map[string]sendCmd, frameTakes, cardTakes map[uint32]bool) {
	frameCommands = map[string]sendCmd{}
	cardCommands = map[string]sendCmd{}
	frameTakes = map[uint32]bool{}
	cardTakes = map[uint32]bool{}

	// Pass 1: group commands.
	for command, cmd := range sl.Group {
		if !cmd.Enabled || cmd.Value.IsNone() {
			continue
		}
		active, has := sl.Active[command]
		activePresent := has && !active.IsNone()
		if checkNull {
			// only skip when we DO have an active value that already matches
			if activePresent && model.ValuesEqualLoose(cmd.Value, active) {
				continue
			}
		} else {
			if !activePresent {
				continue // can't compare, skip
			}
			if model.ValuesEqualLoose(cmd.Value, active) {
				continue // already correct
			}
		}

		cmdNum := uint32(mustAtoi(command))
		if frameCommandsList[cmdNum] {
			frameCommands[command] = sendCmd{cmd.Value, cmd.Type}
			if cmd.Take != 0 {
				frameTakes[cmd.Take.Uint32()] = true
			}
			continue
		}
		if shufflesList[cmdNum] {
			if !model.ValuesEqualLoose(cmd.Value, model.IntVal(int64(shuffleIndex(active)))) {
				cardCommands[command] = sendCmd{cmd.Value, "shuffle"}
			}
		} else {
			cardCommands[command] = sendCmd{cmd.Value, cmd.Type}
		}
		if cmd.Take != 0 {
			cardTakes[cmd.Take.Uint32()] = true
		}
	}

	// Pass 2: prefered commands (override group, or cancel a queued frame write).
	for command, cmd := range sl.Prefered {
		if !cmd.Enabled || cmd.Value.IsNone() {
			continue
		}
		active := sl.Active[command]
		if model.ValuesEqualLoose(cmd.Value, active) {
			delete(frameCommands, command) // already correct, drop any group-queued frame write
			continue
		}
		cmdNum := uint32(mustAtoi(command))
		if frameCommandsList[cmdNum] {
			frameCommands[command] = sendCmd{cmd.Value, cmd.Type}
			if cmd.Take != 0 {
				frameTakes[cmd.Take.Uint32()] = true
			}
		} else if shufflesList[cmdNum] {
			if !model.ValuesEqualLoose(cmd.Value, model.IntVal(int64(shuffleIndex(active)))) {
				cardCommands[command] = sendCmd{cmd.Value, "shuffle"}
			}
		} else {
			cardCommands[command] = sendCmd{cmd.Value, cmd.Type}
		}
		// Faithful quirk (main.ts:722): in the prefered pass the card-take is
		// accumulated for ALL branches, including frame commands.
		if cmd.Take != 0 {
			cardTakes[cmd.Take.Uint32()] = true
		}
	}
	return
}

// shuffleIndex returns the index of the active shuffle label, or -1.
func shuffleIndex(active model.Value) int {
	s := active.String()
	for i, l := range shuffleLabels {
		if l == s {
			return i
		}
	}
	return -1
}

// doCommands ports main.ts:751-810: send the SET batch, fire takes (only if there
// were sets), then run each shuffle as the 8500/8501 pair.
// It returns the commands that never took effect (command id -> reason) after
// the immediate verify-and-retry. Takes are momentary triggers and shuffle
// selects use a 2-step 8500/8501 sequence whose echo semantics aren't confirmed,
// so both are best-effort (not verified); the background poll loop reconciles.
// It returns (applied, failed): `applied` maps each command actually SET this
// cycle to the device's resulting value (from the echo) so the caller can
// reconcile the slot's active map immediately; `failed` maps commands that never
// took to a reason. Takes are momentary (no entry) and shuffles use the 2-step
// 8500/8501 pair whose echo doesn't map back to the aggregate value, so they get
// no applied entry (left to the next scan).
func (s *Scanner) doCommands(ctx context.Context, conns Conns, cmds map[string]sendCmd, takes map[uint32]bool, ip, addr, slot string) (map[string]model.Value, map[string]string) {
	applied := map[string]model.Value{}
	failed := map[string]string{}
	dev, err := conns.Device(ctx, ip)
	if err != nil {
		return applied, failed
	}

	type setOp struct {
		command string
		cmd     uint32
		v       model.Value
	}
	var sets []setOp
	shuffles := map[string]model.Value{}

	for command, c := range cmds {
		switch c.typ {
		case "shuffle":
			shuffles[command] = c.value
		case "text", "smartip":
			sets = append(sets, setOp{command, uint32(mustAtoi(command)), model.StrVal(c.value.String())})
		default:
			sets = append(sets, setOp{command, uint32(mustAtoi(command)), coerceDefault(c.value)})
		}
	}

	if len(sets) > 0 {
		if err := s.Pool.Acquire(ctx); err != nil {
			return applied, failed
		}
		hasTake := false
		for take := range takes {
			if take != 0 {
				hasTake = true
				break
			}
		}

		// 1. Stage the SETs. With a take, the written value is only staged (not
		//    yet active) so its echo can't confirm anything - defer the verdict to
		//    the post-take read-back. With no take it applies immediately, so the
		//    echo check (with retries) is authoritative.
		for _, op := range sets {
			if ctx.Err() != nil {
				s.Pool.Release()
				return applied, failed
			}
			if hasTake {
				if _, err := dev.Set(ctx, addr, slot, op.cmd, op.v); err != nil {
					slog.Warn("staged SET failed", "frame", ip, "addr", addr, "slot", slot, "cmd", op.cmd, "err", err)
				}
				continue
			}
			ok, got, reason := s.setVerified(ctx, dev, addr, slot, op.cmd, op.v)
			if ok {
				applied[op.command] = got
			} else {
				failed[op.command] = reason
				if !got.IsNone() { // rejected SET: record the device-actual value (stays pending)
					applied[op.command] = got
				}
			}
		}

		// 2. Fire the take(s) to commit the staged values. A take is a momentary
		//    SET of the take command to 1; capture its error so a take the device
		//    rejects is visible instead of silently swallowed.
		var takeErr error
		for take := range takes {
			if take == 0 {
				continue
			}
			if err := dev.Take(ctx, addr, slot, take); err != nil {
				takeErr = err
				slog.Warn("take/commit failed", "frame", ip, "addr", addr, "slot", slot, "take", take, "err", err)
			}
		}

		// 3. Verify by read-back: a take-required setting is only ACTIVE once the
		//    take commits it, so confirm each staged command actually holds the new
		//    value now. This is the only reliable check that "set then take"
		//    worked, and it corrects a SET echo that reported a staged-but-not-yet-
		//    active value as applied (which would wrongly clear the pending flag).
		if hasTake {
			for _, op := range sets {
				if ctx.Err() != nil {
					s.Pool.Release()
					return applied, failed
				}
				ok, got := s.verifyActive(ctx, dev, addr, slot, op.cmd, op.v)
				switch {
				case ok:
					applied[op.command] = got
					delete(failed, op.command)
				case !got.IsNone():
					applied[op.command] = got // device-actual; the row correctly stays pending
					failed[op.command] = takeFailReason(takeErr, op.v, got)
				default:
					delete(applied, op.command)
					failed[op.command] = takeFailReason(takeErr, op.v, model.None())
				}
			}
		}
		s.Pool.Release()
	}

	for command, val := range shuffles {
		if ctx.Err() != nil {
			return applied, failed
		}
		spigot := (mustAtoi(command) - 50265) / 300
		if err := s.Pool.Acquire(ctx); err != nil {
			return applied, failed
		}
		_, _ = dev.Set(ctx, addr, slot, 8500, model.IntVal(int64(spigot)))
		_, _ = dev.Set(ctx, addr, slot, 8501, val)
		s.Pool.Release()
	}
	return applied, failed
}

// setVerified sends a SET and confirms the device's echoed value matches what we
// sent, retrying up to VerifyAttempts. It returns (ok, applied, reason): `applied`
// is the device's actual current value (its echo) to write back into the slot's
// active map so the UI reflects reality immediately - on success it's the new
// value (row clears), on a rejected SET it's the unchanged device value (row
// correctly stays pending), and on a transport error it's None (caller must not
// touch active). This is the immediate, within-cycle retry that complements the
// slower background poll-loop reconciliation.
func (s *Scanner) setVerified(ctx context.Context, dev device.Device, addr, slot string, cmd uint32, v model.Value) (bool, model.Value, string) {
	attempts := s.verifyAttempts()
	var got model.Value
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if ctx.Err() != nil {
			return false, model.None(), "scan cancelled"
		}
		got, err = dev.Set(ctx, addr, slot, cmd, v)
		if err == nil && model.ValuesEqualLoose(v, got) {
			return true, got, "" // applied: active becomes the device's confirmed value
		}
		if attempt < attempts {
			select {
			case <-time.After(s.verifyDelay()):
			case <-ctx.Done():
				return false, model.None(), "scan cancelled"
			}
		}
	}
	if err != nil {
		// No trustworthy device value, leave active as the pre-blast read.
		return false, model.None(), fmt.Sprintf("device error after %d attempts: %v", attempts, err)
	}
	// Rejected: `got` is the device's actual value (still != target), so active
	// becomes reality and the row correctly stays pending.
	return false, got, fmt.Sprintf("not applied after %d attempts (sent %s, device reports %s)", attempts, v.String(), got.String())
}

// verifyActive reads cmd back (retried up to VerifyAttempts) and reports whether
// it now holds `want`. Unlike the SET echo, this confirms the value is ACTIVE on
// the card - the only reliable check for a setting that only goes active after a
// take/commit. Returns the last value read (None if every read errored).
func (s *Scanner) verifyActive(ctx context.Context, dev device.Device, addr, slot string, cmd uint32, want model.Value) (bool, model.Value) {
	attempts := s.verifyAttempts()
	got := model.None()
	for attempt := 1; attempt <= attempts; attempt++ {
		if ctx.Err() != nil {
			return false, got
		}
		if v, err := dev.Get(ctx, addr, slot, cmd); err == nil {
			got = v
			if model.ValuesEqualLoose(want, got) {
				return true, got
			}
		}
		if attempt < attempts {
			select {
			case <-time.After(s.verifyDelay()):
			case <-ctx.Done():
				return false, got
			}
		}
	}
	return false, got
}

// takeFailReason explains why a take-required SET is not active after the take,
// distinguishing a take the device rejected from one that ran but didn't commit
// the value (or a read-back that failed). Surfaced in slot.Failed for the UI.
func takeFailReason(takeErr error, want, got model.Value) string {
	if takeErr != nil {
		return fmt.Sprintf("take/commit was rejected (%v); %s not applied", takeErr, want.String())
	}
	if got.IsNone() {
		return fmt.Sprintf("could not read %s back after take to confirm it applied", want.String())
	}
	return fmt.Sprintf("set but not active after take (sent %s, device still reports %s)", want.String(), got.String())
}

// coerceDefault mirrors main.ts's `isNaN(parseFloat(value)) ? "value" : value`:
// a numeric (integer) value is sent as an int; anything else as a string.
func coerceDefault(v model.Value) model.Value {
	if v.Kind == model.KindInt {
		return v
	}
	if i, err := strconv.ParseInt(v.String(), 10, 64); err == nil {
		return model.IntVal(i)
	}
	return model.StrVal(v.String())
}
