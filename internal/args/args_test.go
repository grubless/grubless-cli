package args

import (
	"reflect"
	"strings"
	"testing"
)

var opts = map[string]Option{
	"entity":  {Kind: String},
	"api-url": {Kind: String},
	"json":    {Kind: Bool},
	"help":    {Kind: Bool, Short: 'h'},
	"version": {Kind: Bool, Short: 'v'},
}

func TestFlagsAfterPositionals(t *testing.T) {
	p, err := Parse([]string{"entities", "list", "--json", "--entity", "acme"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Positionals, []string{"entities", "list"}) {
		t.Errorf("positionals = %v", p.Positionals)
	}
	if !p.Bools["json"] {
		t.Error("--json not seen after positionals")
	}
	if v, _ := p.Str("entity"); v != "acme" {
		t.Errorf("entity = %q", v)
	}
}

func TestEqualsForm(t *testing.T) {
	p, err := Parse([]string{"--entity=Acme Trading", "--api-url="}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := p.Str("entity"); v != "Acme Trading" {
		t.Errorf("entity = %q", v)
	}
	// Given-but-empty is distinct from absent.
	if v, ok := p.Str("api-url"); !ok || v != "" {
		t.Errorf("api-url = %q, %v", v, ok)
	}
	if _, ok := p.Str("nope"); ok {
		t.Error("absent option reported as present")
	}
}

func TestStrictRejections(t *testing.T) {
	cases := map[string][]string{
		"unknown long":     {"entities", "list", "--nope"},
		"unknown short":    {"-x"},
		"missing value":    {"--entity"},
		"ambiguous value":  {"--entity", "--json"},
		"value on boolean": {"--json=yes"},
		"unknown in group": {"-hx"},
	}
	for name, argv := range cases {
		if _, err := Parse(argv, opts); err == nil {
			t.Errorf("%s: %v parsed without error", name, argv)
		}
	}
}

func TestShortGroupAndDoubleDash(t *testing.T) {
	p, err := Parse([]string{"-hv", "--", "--json", "-"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Bools["help"] || !p.Bools["version"] {
		t.Error("grouped shorts not both set")
	}
	if p.Bools["json"] {
		t.Error("--json after -- was treated as a flag")
	}
	if strings.Join(p.Positionals, " ") != "--json -" {
		t.Errorf("positionals = %v", p.Positionals)
	}
}
