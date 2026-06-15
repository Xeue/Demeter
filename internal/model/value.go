// Package model holds Demeter's domain types (frames, slots, groups) and the
// single Value type + comparison used throughout the scan/blast logic.
//
// The legacy TypeScript app stored loosely-typed values (a scanned "active"
// value is a JS number or string; a desired value is a string from the UI or
// the expression evaluator) and compared them with JS loose `==`. Getting that
// comparison right is correctness-critical: if it reports unequal when the
// device already holds the desired value, Demeter re-blasts every poll cycle.
package model

import (
	"encoding/json"
	"strconv"
	"strings"
)

// ValueKind is the discriminant for Value.
type ValueKind uint8

const (
	// KindNone is an absent/null value.
	KindNone ValueKind = iota
	// KindInt is an integer value.
	KindInt
	// KindStr is a string value.
	KindStr
)

// Value is a loosely-typed parameter value. On the wire/disk it (un)marshals as
// a JSON number, string, or null; internally it is a tagged union so comparisons
// are explicit.
type Value struct {
	Kind ValueKind
	Int  int64
	Str  string
}

// None returns an absent Value.
func None() Value { return Value{Kind: KindNone} }

// IntVal returns an integer Value.
func IntVal(i int64) Value { return Value{Kind: KindInt, Int: i} }

// StrVal returns a string Value.
func StrVal(s string) Value { return Value{Kind: KindStr, Str: s} }

// IsNone reports whether the value is absent.
func (v Value) IsNone() bool { return v.Kind == KindNone }

// String renders the value for display/logging and for building wire command
// strings (mirrors how the legacy code stringified values).
func (v Value) String() string {
	switch v.Kind {
	case KindInt:
		return strconv.FormatInt(v.Int, 10)
	case KindStr:
		return v.Str
	default:
		return ""
	}
}

// MarshalJSON renders Int as a JSON number, Str as a JSON string, None as null.
func (v Value) MarshalJSON() ([]byte, error) {
	switch v.Kind {
	case KindInt:
		return json.Marshal(v.Int)
	case KindStr:
		return json.Marshal(v.Str)
	default:
		return []byte("null"), nil
	}
}

// UnmarshalJSON accepts number | string | bool | null. Integral numbers become
// Int; fractional numbers are kept as their textual form (Demeter never does
// arithmetic on scanned values, only equality, and ValuesEqualLoose coerces);
// booleans become Int 0/1 (the legacy active map declared bool as a possible
// type, treated numerically by the UI).
func (v *Value) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "" || s == "null" {
		*v = Value{Kind: KindNone}
		return nil
	}
	switch b[0] {
	case '"':
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*v = Value{Kind: KindStr, Str: str}
	case 't', 'f':
		var bl bool
		if err := json.Unmarshal(b, &bl); err != nil {
			return err
		}
		if bl {
			*v = Value{Kind: KindInt, Int: 1}
		} else {
			*v = Value{Kind: KindInt, Int: 0}
		}
	default:
		num := json.Number(s)
		if i, err := num.Int64(); err == nil {
			*v = Value{Kind: KindInt, Int: i}
		} else {
			// fractional or out-of-int64-range: keep the textual form
			*v = Value{Kind: KindStr, Str: num.String()}
		}
	}
	return nil
}

// ValuesEqualLoose reports whether a and b are equal under the same rules the
// legacy JS `==` used for the value pairs Demeter actually compares:
//   - both None  -> equal
//   - one None   -> not equal
//   - same kind  -> exact compare
//   - Int vs Str -> coerce the string with JS Number() semantics and compare
//
// The shuffle index comparison (indexOf of fixed labels) is NOT routed through
// here — it is a plain integer compare in the scan layer.
func ValuesEqualLoose(a, b Value) bool {
	if a.Kind == KindNone || b.Kind == KindNone {
		return a.Kind == KindNone && b.Kind == KindNone
	}
	if a.Kind == KindInt && b.Kind == KindInt {
		return a.Int == b.Int
	}
	if a.Kind == KindStr && b.Kind == KindStr {
		return a.Str == b.Str
	}
	var iv int64
	var sv string
	if a.Kind == KindInt {
		iv, sv = a.Int, b.Str
	} else {
		iv, sv = b.Int, a.Str
	}
	f, ok := jsNumber(sv)
	if !ok {
		return false
	}
	return f == float64(iv)
}

// jsNumber mimics JS Number(s) for the inputs we hit: trimmed empty string is 0,
// otherwise a base-10 numeric parse; non-numeric is NaN (ok=false). It does not
// reproduce JS hex/`0x` parsing (Demeter never stores 0x values in compared
// fields — frame unit addresses are parsed separately).
func jsNumber(s string) (float64, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, true
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// Num is a lenient integer used for catalogue/config fields (take, increment)
// that the legacy app stored as either JSON numbers or numeric strings. It
// marshals as a JSON number.
type Num int64

// UnmarshalJSON accepts a JSON number or a numeric string; empty/null/garbage
// becomes 0 (matching JS Number() coercion of those fields).
func (n *Num) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	if b[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		str = strings.TrimSpace(str)
		if str == "" {
			*n = 0
			return nil
		}
		f, err := strconv.ParseFloat(str, 64)
		if err != nil {
			*n = 0
			return nil
		}
		*n = Num(int64(f))
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		*n = 0
		return nil
	}
	*n = Num(int64(f))
	return nil
}

// Uint32 returns the value as a uint32 command id.
func (n Num) Uint32() uint32 { return uint32(n) }
