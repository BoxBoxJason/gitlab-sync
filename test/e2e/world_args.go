//go:build e2e

package e2e

import (
	"strings"
)

// defaultFlags are the flags every run starts from, in this order. A scenario
// overrides one by passing the same flag, or drops it with `without`.
func defaultFlags() []string {
	return []string{flagSourceURL, flagSourceToken, flagDestinationURL, flagDestinationToken, flagMirrorMapping, flagNoPrompt}
}

// flagsFor builds the command line of a run: the default flags (minus the
// overridden and the dropped ones), then the extra flags.
func (w *world) flagsFor(extra, without []string) []string {
	skipped := make(map[string]bool)

	for _, name := range append(append([]string(nil), without...), flagNames(extra)...) {
		skipped[canonicalFlag(name)] = true
	}

	values := map[string]string{
		flagSourceURL:        w.url(roleSource),
		flagSourceToken:      sourceToken,
		flagDestinationURL:   w.url(roleDestination),
		flagDestinationToken: destinationToken,
		flagMirrorMapping:    w.mappingPath,
	}

	var args []string

	for _, name := range defaultFlags() {
		if skipped[name] {
			continue
		}

		if name == flagNoPrompt {
			args = append(args, name)

			continue
		}

		if values[name] != "" {
			args = append(args, name, values[name])
		}
	}

	return append(args, extra...)
}

func canonicalFlag(name string) string {
	// -n is the short form of --no-prompt
	if name == "-n" {
		return flagNoPrompt
	}

	return name
}

// flagNames lists the flags (not their values) among args.
func flagNames(args []string) []string {
	var names []string

	for _, arg := range args {
		if name, _, _ := strings.Cut(arg, "="); strings.HasPrefix(name, "-") {
			names = append(names, name)
		}
	}

	return names
}

// splitArgs splits a command line the way a shell would for plain words and
// double quoted strings.
func splitArgs(line string) []string {
	var (
		args    []string
		current []rune
		quoted  bool
		started bool
	)

	for _, char := range line {
		switch {
		case char == '"':
			quoted, started = !quoted, true
		case (char == ' ' || char == '\t') && !quoted:
			if started {
				args = append(args, string(current))
				current = current[:0]

				started = false
			}
		default:
			current = append(current, char)

			started = true
		}
	}

	if started {
		args = append(args, string(current))
	}

	return args
}
