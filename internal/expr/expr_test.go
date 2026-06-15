package expr

import (
	"testing"

	"github.com/Xeue/Demeter/internal/model"
)

func TestEvalValue(t *testing.T) {
	cases := []struct {
		in   string
		kind model.ValueKind
		i    int64
		s    string
	}{
		{"1", model.KindInt, 1, ""},
		{"42", model.KindInt, 42, ""},
		{"40+2", model.KindInt, 42, ""},
		{"2110-30", model.KindInt, 2080, ""}, // bug-for-bug: legacy eval subtracts
		{"2*3+4", model.KindInt, 10, ""},
		{"2+3*4", model.KindInt, 14, ""}, // precedence
		{"(2+3)*4", model.KindInt, 20, ""},
		{"-5+8", model.KindInt, 3, ""},
		{"10/3", model.KindInt, 3, ""}, // integer division (documented)
		{"01", model.KindInt, 1, ""},   // leading zero -> decimal (documented divergence)
		{"Static", model.KindStr, 0, "Static"},
		{"PTP", model.KindStr, 0, "PTP"},
		{"", model.KindStr, 0, ""},
		{"1.5", model.KindStr, 0, "1.5"}, // '.' is not in the grammar -> literal fallback
		{"2110-", model.KindStr, 0, "2110-"},
		{"foo+1", model.KindStr, 0, "foo+1"},
	}
	for _, c := range cases {
		v := EvalValue(c.in)
		if v.Kind != c.kind {
			t.Errorf("EvalValue(%q).Kind=%d want %d", c.in, v.Kind, c.kind)
			continue
		}
		if c.kind == model.KindInt && v.Int != c.i {
			t.Errorf("EvalValue(%q).Int=%d want %d", c.in, v.Int, c.i)
		}
		if c.kind == model.KindStr && v.Str != c.s {
			t.Errorf("EvalValue(%q).Str=%q want %q", c.in, v.Str, c.s)
		}
	}
}

func TestEvalSmartIP(t *testing.T) {
	ok := []struct{ in, want string }{
		{"10.40.44.10", "10.40.44.10"},
		{"192.168.0.1", "192.168.0.1"},
		{"10.40.40+4.10", "10.40.44.10"}, // arithmetic in an octet (post-substitution)
		{"10.40.44.10+5", "10.40.44.15"},
	}
	for _, c := range ok {
		got, err := EvalSmartIP(c.in)
		if err != nil {
			t.Errorf("EvalSmartIP(%q) err=%v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("EvalSmartIP(%q)=%q want %q", c.in, got, c.want)
		}
	}
	// strict: a non-numeric octet errors (no per-octet fallback)
	if _, err := EvalSmartIP("10.40.x.1"); err == nil {
		t.Error("EvalSmartIP with non-numeric octet should error")
	}
}
