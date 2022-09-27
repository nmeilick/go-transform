package transform

import (
	"os"
	"regexp"
	"strings"

	"github.com/pkg/errors"
)

const (
	// ShellVar matches ${KEY}
	ShellVar = `(?i)\${\s*(?P<key>[A-Z0-9_]+)\s*}`
)

// TransformFunc takes a string and applies a transformation.
type TransformFunc func(string) (string, error)

// Handlers indexes transformation functions by a string tag.
type Handlers map[string]TransformFunc

// TransformOption is an option func that is applied to the Transform struct
// on instantiation.
type TransformOption func(*Transform)

// Lookup returns an option func that adds default lookup functions.
func Lookup(ff ...LookupFunc) TransformOption {
	return func(t *Transform) {
		t.Lookups = append(t.Lookups, ff...)
	}
}

// Handler returns an option func that registers a new transformation handler.
func Handler(tag string, f TransformFunc) TransformOption {
	return func(t *Transform) {
		if tag != "" {
			tag = strings.ToLower(tag)
			if t.Handlers == nil {
				t.Handlers = Handlers{}
			}

			if f == nil {
				delete(t.Handlers, tag)
			} else {
				t.Handlers[tag] = f
			}
		}
	}
}

// Rule adds a default transformation rule for use with Transform().
func Rule(ff ...TransformFunc) TransformOption {
	return func(t *Transform) {
		t.Rules = append(t.Rules, ff...)
	}
}

// ExpandEnv adds options to expand environment variables.
func ExpandEnv() TransformOption {
	return func(t *Transform) {
		f, err := t.ParseStringRule("expand:" + ShellVar)
		if err != nil {
			panic(err)
		}
		t.Rules = append(t.Rules, f)
		t.Lookups = append(t.Lookups, LookupEnv())
	}
}

// Transform holds transformation configuration.
type Transform struct {
	Handlers Handlers
	Lookups  []LookupFunc
	Rules    []TransformFunc
}

// New returns a new transformation configuration.
func New(ff ...TransformOption) *Transform {
	t := &Transform{}
	t.Reset(ff...)
	return t
}

// Reset resets a transformation configuration to its default state.
func (t *Transform) Reset(ff ...TransformOption) *Transform {
	t.ResetHandlers()
	t.ResetLookups()
	t.ResetRules()
	for _, f := range ff {
		f(t)
	}
	return t
}

// Reset resets registered transformation handlers to their default state.
func (t *Transform) ResetHandlers() *Transform {
	t.Handlers = Handlers{
		"":           t.NOP,
		"nop":        t.NOP,
		"trim":       t.Trim,
		"downcase":   t.Downcase,
		"upcase":     t.Upcase,
		"capitalize": t.Capitalize,
	}
	return t
}

// Reset resets lookup functions to defaults.
func (t *Transform) ResetLookups(ff ...LookupFunc) *Transform {
	t.Lookups = ff
	return t
}

// Reset resets transformation rules to defaults.
func (t *Transform) ResetRules(ff ...TransformFunc) *Transform {
	t.Rules = ff
	return t
}

// ParseStringRule parses a string transformation rule and returns the
// corresponding transformation func, or an error if there is none.
func (t *Transform) ParseStringRule(rule string) (TransformFunc, error) {
	parts := strings.SplitN(rule, ":", 2)
	tag := strings.ToLower(strings.TrimSpace(parts[0]))

	h := t.Handlers
	if h == nil {
		h = Handlers{}
	}
	f := h[tag]

	if f == nil {
		switch tag {
		case "expand":
			if len(parts) == 1 {
				return nil, errors.New("expand: missing regex")
			}
			re, err := regexp.Compile(parts[1])
			if err != nil {
				return nil, errors.Wrap(err, "regexp: "+parts[1])
			}
			f, err := t.Expand(re)
			if err != nil {
				return nil, err
			}
			return f, nil
		}
	}

	if f == nil {
		return nil, errors.New("unknown transform: " + tag)
	}
	return f, nil
}

// AddStringRules parses the given string transformation rules and adds the
// corresponding transformation functions.
func (t *Transform) AddStringRules(rules ...string) error {
	for _, r := range rules {
		for _, s := range strings.Split(r, ",") {
			if s = strings.TrimSpace(s); s != "" {
				f, err := t.ParseStringRule(s)
				if err != nil {
					return err
				}
				t.Rules = append(t.Rules, f)
			}
		}
	}
	return nil
}

// NOP returns the given string unchanged.
func (*Transform) NOP(s string) (string, error) {
	return s, nil
}

// Trim returns the string with leading and trailing whitespace removed.
func (*Transform) Trim(s string) (string, error) {
	return strings.TrimSpace(s), nil
}

// Downcase returns a lowercased version of the given string.
func (*Transform) Downcase(s string) (string, error) {
	return strings.ToLower(s), nil
}

// Downcase returns an uppercased version of the given string.
func (*Transform) Upcase(s string) (string, error) {
	return strings.ToUpper(s), nil
}

// Capitalized returns a capitalized version of the given string, i.e., the
// first character is uppercased and the others lowercased.
func (*Transform) Capitalize(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:]), nil
}

// Expand returns a function that replaces patterns by looking up a named key
// using the given lookup functions. The regular expression must have a
// parenthesized subexpression called "key" that identifies the key string to
// look up.
func (t *Transform) Expand(re *regexp.Regexp, ff ...LookupFunc) (TransformFunc, error) {
	idx := re.SubexpIndex("key")
	if idx == -1 {
		return nil, errors.New("regexp is missing named parenthesized subexpression (?P<key>...): " + re.String())
	}
	return func(s string) (string, error) {
		matches := re.FindAllStringSubmatchIndex(s, -1)
		if len(matches) == 0 {
			return s, nil
		}

		if len(ff) == 0 {
			ff = t.Lookups
		}

		var s2 string
		pos := 0
		for _, m := range matches {
			var val string
			key := string(s[m[idx*2]:m[idx*2+1]])
			for _, f := range ff {
				if v, ok := f(key); ok {
					val = v
					break
				}
			}
			if val == "" {
				return "", errors.New("could not resolve variable: " + key)
			}
			s2 += s[pos:m[0]] + val
			pos = m[1]
		}
		s2 += s[pos:]
		return s2, nil
	}, nil
}

type LookupFunc func(string) (string, bool)

// LookupHandlers returns a lookup function that uses the given map as data source.
func LookupHandlers(m map[string]string) LookupFunc {
	if m == nil {
		m = map[string]string{}
	}
	return func(name string) (string, bool) {
		val, found := m[name]
		return val, found
	}
}

// LookupEnv returns a lookup function that uses the current environment as data source.
func LookupEnv() LookupFunc {
	return func(name string) (string, bool) {
		if val := os.Getenv(name); val != "" {
			return val, true
		}
		return "", false
	}
}

// LookupStatic returns a lookup function that returns the given value.
// If the value contains %s, all occurences will be replaced by the
// name that is looked up.
func LookupStatic(val string) LookupFunc {
	return func(name string) (string, bool) {
		if strings.Contains(val, "%s") {
			return strings.ReplaceAll(val, "%s", name), true
		}
		return val, true
	}
}

// Transform takes a string and applies the given transformation functions to
// it. If no transformation functions are given, it uses the configured default
// rules (see Transform.Rules).
func (t *Transform) Transform(s string, ff ...TransformFunc) (string, error) {
	if len(ff) == 0 {
		ff = t.Rules
	}
	var err error
	for _, f := range ff {
		if f != nil {
			if s, err = f(s); err != nil {
				return "", errors.Wrap(err, "rule")
			}
		}
	}
	return s, nil
}
