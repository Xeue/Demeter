package model

import (
	"encoding/json"
	"testing"
)

func TestValuesEqualLoose(t *testing.T) {
	cases := []struct {
		name string
		a, b Value
		want bool
	}{
		{"int eq", IntVal(1), IntVal(1), true},
		{"int neq", IntVal(1), IntVal(2), false},
		{"str eq", StrVal("UP"), StrVal("UP"), true},
		{"str neq", StrVal("UP"), StrVal("DOWN"), false},
		{`"1" vs 1 (select)`, StrVal("1"), IntVal(1), true},
		{`"01" vs 1 (leading zero)`, StrVal("01"), IntVal(1), true},
		{`"1.0" vs 1`, StrVal("1.0"), IntVal(1), true},
		{`"" vs 0 (empty coerces to 0)`, StrVal(""), IntVal(0), true},
		{`"0" vs 0`, StrVal("0"), IntVal(0), true},
		{`"UP" vs int (non-numeric)`, StrVal("UP"), IntVal(1), false},
		{`"10" vs 16 (no hex)`, StrVal("10"), IntVal(16), false},
		{"ip string vs int", StrVal("10.40.44.10"), IntVal(10), false},
		{"none vs none", None(), None(), true},
		{"none vs int", None(), IntVal(0), false},
		{"int vs none", IntVal(0), None(), false},
	}
	for _, c := range cases {
		if got := ValuesEqualLoose(c.a, c.b); got != c.want {
			t.Errorf("%s: ValuesEqualLoose(%v,%v)=%v want %v", c.name, c.a, c.b, got, c.want)
		}
		// commutative
		if got := ValuesEqualLoose(c.b, c.a); got != c.want {
			t.Errorf("%s (rev): ValuesEqualLoose(%v,%v)=%v want %v", c.name, c.b, c.a, got, c.want)
		}
	}
}

func TestValueJSONRoundTrip(t *testing.T) {
	cases := []struct {
		in   string
		kind ValueKind
	}{
		{`5`, KindInt},
		{`"10.0.0.1"`, KindStr},
		{`null`, KindNone},
		{`true`, KindInt},
		{`false`, KindInt},
		{`1.5`, KindStr}, // fractional kept as text
	}
	for _, c := range cases {
		var v Value
		if err := json.Unmarshal([]byte(c.in), &v); err != nil {
			t.Fatalf("unmarshal %s: %v", c.in, err)
		}
		if v.Kind != c.kind {
			t.Errorf("%s: kind=%d want %d", c.in, v.Kind, c.kind)
		}
		if _, err := json.Marshal(v); err != nil {
			t.Errorf("%s: marshal: %v", c.in, err)
		}
	}
}

func TestNumLenient(t *testing.T) {
	cases := []struct {
		in   string
		want Num
	}{
		{`300`, 300},
		{`"300"`, 300},
		{`""`, 0},
		{`null`, 0},
		{`"50002"`, 50002},
		{`"abc"`, 0},
	}
	for _, c := range cases {
		var n Num
		if err := json.Unmarshal([]byte(c.in), &n); err != nil {
			t.Fatalf("unmarshal %s: %v", c.in, err)
		}
		if n != c.want {
			t.Errorf("%s: Num=%d want %d", c.in, n, c.want)
		}
	}
}
