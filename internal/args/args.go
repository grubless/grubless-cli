// Package args is a strict argument parser shaped like Node's util.parseArgs.
//
// Not the standard library's `flag`: that stops at the first positional, so
// `grubless entities list --json` would treat `--json` as a positional and
// never see it. Every command in this CLI puts its flags after the
// subcommand, so `flag` can't parse a single real invocation. What's here is
// the subset of parseArgs the CLI uses — long options, `--name=value`, a few
// short aliases, `--`, and strict rejection of anything unknown.
package args

import (
	"fmt"
	"strings"
)

type Kind int

const (
	Bool Kind = iota
	String
)

type Option struct {
	Kind  Kind
	Short byte // 0 for none
}

type Parsed struct {
	Bools       map[string]bool
	Strings     map[string]string
	Positionals []string
}

// Str returns a string option and whether it was given at all — the
// distinction parseArgs makes with `undefined`, which `--api-url ""` relies on.
func (p *Parsed) Str(name string) (string, bool) {
	v, ok := p.Strings[name]
	return v, ok
}

func Parse(argv []string, options map[string]Option) (*Parsed, error) {
	p := &Parsed{Bools: map[string]bool{}, Strings: map[string]string{}}

	byShort := map[byte]string{}
	for name, o := range options {
		if o.Short != 0 {
			byShort[o.Short] = name
		}
	}

	for i := 0; i < len(argv); i++ {
		arg := argv[i]

		switch {
		case arg == "--":
			p.Positionals = append(p.Positionals, argv[i+1:]...)
			return p, nil

		case strings.HasPrefix(arg, "--"):
			name, value, hasValue := strings.Cut(arg[2:], "=")
			opt, ok := options[name]
			if !ok {
				return nil, unknown("--" + name)
			}
			if opt.Kind == Bool {
				if hasValue {
					return nil, fmt.Errorf("Option '--%s' does not take an argument", name)
				}
				p.Bools[name] = true
				continue
			}
			if !hasValue {
				if i+1 >= len(argv) {
					return nil, fmt.Errorf("Option '--%s <value>' argument missing", name)
				}
				// parseArgs refuses `--entity --json` rather than taking
				// "--json" as the entity name, and so do we.
				if strings.HasPrefix(argv[i+1], "-") {
					return nil, fmt.Errorf("Option '--%s' argument is ambiguous.\nDid you forget to specify the option argument for '--%s'?\nTo specify an option argument starting with a dash use '--%s=-XYZ'.", name, name, name)
				}
				i++
				value = argv[i]
			}
			p.Strings[name] = value

		case strings.HasPrefix(arg, "-") && arg != "-":
			// Short options: only -h and -v exist, both boolean, so grouping
			// (`-hv`) is supported and short string values are not needed.
			for j := 1; j < len(arg); j++ {
				name, ok := byShort[arg[j]]
				if !ok {
					return nil, unknown("-" + string(arg[j]))
				}
				if options[name].Kind != Bool {
					return nil, fmt.Errorf("Option '-%c' needs a value; use '--%s <value>'", arg[j], name)
				}
				p.Bools[name] = true
			}

		default:
			p.Positionals = append(p.Positionals, arg)
		}
	}
	return p, nil
}

func unknown(flag string) error {
	return fmt.Errorf("Unknown option '%s'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"%s\"", flag, flag)
}
