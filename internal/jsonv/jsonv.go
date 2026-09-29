// Package jsonv is an ordered JSON value whose encoder reproduces
// `JSON.stringify(value, null, 2)` byte for byte.
//
// Why not encoding/json: every `--json` output in this CLI is the server's
// data, reshaped — a source with `entityName` appended, a report's rows
// wrapped in an envelope — and then printed by JSON.stringify. Decoding into
// a struct drops fields this client doesn't declare. Decoding into a
// map[string]any loses key order. And encoding/json escapes differently
// (`<`, `>`, `&`, U+2028) and formats numbers differently. Any of those
// would make `grubless … --json | jq` produce a different document from the
// Node build for the same server response, which is the one thing a port
// must not do.
//
// So this keeps what JSON.parse keeps and nothing more: values, key order
// (with JS's rule that integer-like keys sort first), and numbers as float64.
package jsonv

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/grubless/grubless-cli/internal/jsstr"
)

// Value is one of: nil (null), bool, float64, string, *Object, []Value.
type Value = any

// Object is a JSON object that remembers insertion order, as JS objects do.
type Object struct {
	keys []string
	vals map[string]Value
}

func NewObject() *Object { return &Object{vals: map[string]Value{}} }

// Obj builds an object from alternating keys and values, in order.
func Obj(kv ...any) *Object {
	o := NewObject()
	for i := 0; i+1 < len(kv); i += 2 {
		o.Set(kv[i].(string), kv[i+1])
	}
	return o
}

// Set assigns a key. An existing key keeps its position, as in JS.
func (o *Object) Set(key string, v Value) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

func (o *Object) Get(key string) (Value, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// Str returns a string field, and whether it was one.
func (o *Object) Str(key string) (string, bool) {
	s, ok := o.vals[key].(string)
	return s, ok
}

// Keys in JS own-property order: array-index keys ascending, then the rest in
// insertion order. That rule is why a CSV column named "2025" comes first in
// Node's output, and so it must here too.
func (o *Object) Keys() []string {
	var index, rest []string
	for _, k := range o.keys {
		if isArrayIndex(k) {
			index = append(index, k)
		} else {
			rest = append(rest, k)
		}
	}
	sort.Slice(index, func(i, j int) bool {
		a, _ := strconv.ParseUint(index[i], 10, 64)
		b, _ := strconv.ParseUint(index[j], 10, 64)
		return a < b
	})
	return append(index, rest...)
}

func isArrayIndex(k string) bool {
	if k == "0" {
		return true
	}
	if k == "" || k[0] < '1' || k[0] > '9' || len(k) > 10 {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return false
		}
	}
	n, err := strconv.ParseUint(k, 10, 64)
	return err == nil && n < math.MaxUint32
}

// Undefined, as a field value, omits the field — JSON.stringify's treatment
// of `undefined`. In an array it becomes null, also as in JS.
type undefined struct{}

var Undefined = undefined{}

// ---------- stringify ----------

// Stringify is `JSON.stringify(v, null, 2)`.
func Stringify(v Value) string {
	var b strings.Builder
	write(&b, v, "")
	return b.String()
}

func write(b *strings.Builder, v Value, indent string) {
	switch x := v.(type) {
	case nil, undefined:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			b.WriteString("null")
		} else {
			b.WriteString(jsstr.Number(x))
		}
	case int:
		b.WriteString(strconv.Itoa(x))
	case string:
		quote(b, x)
	case []string:
		vals := make([]Value, len(x))
		for i, s := range x {
			vals[i] = s
		}
		write(b, vals, indent)
	case []Value:
		if len(x) == 0 {
			b.WriteString("[]")
			return
		}
		inner := indent + "  "
		b.WriteString("[\n")
		for i, item := range x {
			if i > 0 {
				b.WriteString(",\n")
			}
			b.WriteString(inner)
			write(b, item, inner)
		}
		b.WriteString("\n" + indent + "]")
	case *Object:
		inner := indent + "  "
		first := true
		for _, k := range x.Keys() {
			val := x.vals[k]
			if _, skip := val.(undefined); skip {
				continue
			}
			if first {
				b.WriteString("{\n")
				first = false
			} else {
				b.WriteString(",\n")
			}
			b.WriteString(inner)
			quote(b, k)
			b.WriteString(": ")
			write(b, val, inner)
		}
		if first {
			b.WriteString("{}")
			return
		}
		b.WriteString("\n" + indent + "}")
	default:
		panic(fmt.Sprintf("jsonv: cannot encode %T", v))
	}
}

// quote is JSON.stringify's QuoteJSONString: only `"`, `\` and C0 controls
// are escaped. Unlike encoding/json, `<>&` and U+2028/9 go through as-is.
func quote(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// ---------- parse ----------

// Parse is JSON.parse. Numbers become float64 (so `1e400` is +Inf, and
// prints as null, exactly as in JS).
//
// One knowing divergence: a lone surrogate escape (`"\ud800"`) survives
// JSON.parse in JS but cannot exist in a Go string, so it becomes U+FFFD.
func Parse(data []byte) (Value, error) {
	p := &parser{s: data}
	p.space()
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	p.space()
	if p.i != len(p.s) {
		return nil, p.errorf("unexpected data after JSON value")
	}
	return v, nil
}

type parser struct {
	s []byte
	i int
}

func (p *parser) errorf(format string, a ...any) error {
	return fmt.Errorf("invalid JSON at position %d: %s", p.i, fmt.Sprintf(format, a...))
}

func (p *parser) space() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) value() (Value, error) {
	if p.i >= len(p.s) {
		return nil, p.errorf("unexpected end of input")
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"':
		return p.str()
	case c == 't':
		return p.literal("true", true)
	case c == 'f':
		return p.literal("false", false)
	case c == 'n':
		return p.literal("null", nil)
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	default:
		return nil, p.errorf("unexpected character %q", c)
	}
}

func (p *parser) literal(word string, v Value) (Value, error) {
	if !strings.HasPrefix(string(p.s[p.i:min(len(p.s), p.i+len(word))]), word) {
		return nil, p.errorf("expected %s", word)
	}
	p.i += len(word)
	return v, nil
}

func (p *parser) object() (Value, error) {
	p.i++ // {
	o := NewObject()
	p.space()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
		return o, nil
	}
	for {
		p.space()
		if p.i >= len(p.s) || p.s[p.i] != '"' {
			return nil, p.errorf("expected a key")
		}
		k, err := p.str()
		if err != nil {
			return nil, err
		}
		p.space()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, p.errorf("expected ':'")
		}
		p.i++
		p.space()
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		o.Set(k, v)
		p.space()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
			continue
		}
		if p.i < len(p.s) && p.s[p.i] == '}' {
			p.i++
			return o, nil
		}
		return nil, p.errorf("expected ',' or '}'")
	}
}

func (p *parser) array() (Value, error) {
	p.i++ // [
	arr := []Value{}
	p.space()
	if p.i < len(p.s) && p.s[p.i] == ']' {
		p.i++
		return arr, nil
	}
	for {
		p.space()
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
		p.space()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
			continue
		}
		if p.i < len(p.s) && p.s[p.i] == ']' {
			p.i++
			return arr, nil
		}
		return nil, p.errorf("expected ',' or ']'")
	}
}

func (p *parser) number() (Value, error) {
	start := p.i
	if p.s[p.i] == '-' {
		p.i++
	}
	digits := func() int {
		n := 0
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
			n++
		}
		return n
	}
	if p.i < len(p.s) && p.s[p.i] == '0' {
		p.i++
	} else if digits() == 0 {
		return nil, p.errorf("invalid number")
	}
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		if digits() == 0 {
			return nil, p.errorf("invalid number")
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		if digits() == 0 {
			return nil, p.errorf("invalid number")
		}
	}
	f, err := strconv.ParseFloat(string(p.s[start:p.i]), 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); !ok || ne.Err != strconv.ErrRange {
			return nil, p.errorf("invalid number")
		}
	}
	return f, nil
}

func (p *parser) str() (string, error) {
	p.i++ // opening quote
	var b strings.Builder
	for {
		if p.i >= len(p.s) {
			return "", p.errorf("unterminated string")
		}
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			return b.String(), nil
		case c < 0x20:
			return "", p.errorf("control character in string")
		case c != '\\':
			r, size := utf8.DecodeRune(p.s[p.i:])
			b.WriteRune(r)
			p.i += size
			continue
		}
		// Escape.
		p.i++
		if p.i >= len(p.s) {
			return "", p.errorf("unterminated escape")
		}
		esc := p.s[p.i]
		p.i++
		switch esc {
		case '"', '\\', '/':
			b.WriteByte(esc)
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'u':
			r, err := p.hex4()
			if err != nil {
				return "", err
			}
			if utf16.IsSurrogate(r) && r < 0xDC00 && p.i+6 <= len(p.s) && p.s[p.i] == '\\' && p.s[p.i+1] == 'u' {
				save := p.i
				p.i += 2
				lo, err := p.hex4()
				if err == nil && lo >= 0xDC00 && lo <= 0xDFFF {
					b.WriteRune(utf16.DecodeRune(r, lo))
					continue
				}
				p.i = save
			}
			b.WriteRune(r) // a lone surrogate encodes as U+FFFD
		default:
			return "", p.errorf("invalid escape")
		}
	}
}

func (p *parser) hex4() (rune, error) {
	if p.i+4 > len(p.s) {
		return 0, p.errorf("short \\u escape")
	}
	n, err := strconv.ParseUint(string(p.s[p.i:p.i+4]), 16, 32)
	if err != nil {
		return 0, p.errorf("invalid \\u escape")
	}
	p.i += 4
	return rune(n), nil
}
