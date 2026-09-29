package jsonv

import "testing"

// Each want is what `JSON.stringify(JSON.parse(in), null, 2)` prints in Node.
func TestRoundTripMatchesJSONStringify(t *testing.T) {
	cases := []struct{ in, want string }{
		{`[]`, `[]`},
		{`{}`, `{}`},
		{`{"b":1,"a":[1,{"c":null}]}`, "{\n  \"b\": 1,\n  \"a\": [\n    1,\n    {\n      \"c\": null\n    }\n  ]\n}"},
		// Integer-like keys first, ascending; the rest keep insertion order.
		{`{"name":"x","2025":"a","10":"b","01":"c"}`, "{\n  \"10\": \"b\",\n  \"2025\": \"a\",\n  \"name\": \"x\",\n  \"01\": \"c\"\n}"},
		// A duplicate key keeps its first position and its last value.
		{`{"a":1,"b":2,"a":3}`, "{\n  \"a\": 3,\n  \"b\": 2\n}"},
		// Escapes are normalised: é and \/ print literally, as do <>& and U+2028.
		{`"é\/<>& "`, "\"é/<>& \""},
		{`"tab\there\u0001"`, `"tab\there\u0001"`},
		// Numbers print as JS prints them.
		{`[1.0, 1e21, 1e-7, 123456789012345678901, -0, 1e400, 0.1]`, "[\n  1,\n  1e+21,\n  1e-7,\n  123456789012345680000,\n  0,\n  null,\n  0.1\n]"},
		{`"😀"`, `"😀"`},
	}
	for _, c := range cases {
		v, err := Parse([]byte(c.in))
		if err != nil {
			t.Fatalf("Parse(%s): %v", c.in, err)
		}
		if got := Stringify(v); got != c.want {
			t.Errorf("Stringify(Parse(%s))\n got: %q\nwant: %q", c.in, got, c.want)
		}
	}
}

func TestUndefinedFieldsAreOmitted(t *testing.T) {
	o := Obj("a", 1, "gone", Undefined, "b", []Value{Undefined})
	if got, want := Stringify(o), "{\n  \"a\": 1,\n  \"b\": [\n    null\n  ]\n}"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := Stringify(Obj("only", Undefined)); got != "{}" {
		t.Errorf("all-undefined object = %q, want {}", got)
	}
}

func TestRejectsMalformedInput(t *testing.T) {
	for _, in := range []string{``, `{`, `[1,]`, `{"a" 1}`, `01`, `"unterminated`, `nul`, `[1] x`, "\"a\nb\""} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("Parse(%q) succeeded", in)
		}
	}
}
