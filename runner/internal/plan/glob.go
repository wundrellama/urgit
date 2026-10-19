package plan

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// MaxPatternBytes bounds one filter pattern; MaxPatterns one filter list
// (Delivery 1C, native bounds: the reference documents none).
const (
	MaxPatternBytes = 256
	MaxPatterns     = 100
)

// Token is one compiled element of a branches/tags/paths filter pattern
// (Delivery 1C). The ship matches these tokens, bytewise, and never reads
// the pattern text, which travels beside them for display only:
//
//	lit       the bytes of C, in order: a run of literal text, always whole
//	          UTF-8 (a quantifier applies to one ASCII byte only)
//	class     one byte of Set (the reference allows a-z, A-Z, 0-9 and ranges)
//	star      `*`: zero or more bytes, none of them `/`
//	globstar  `**`: zero or more bytes
//	dirs      `**/`: zero or more whole directories, so `**/README.md`
//	          matches `README.md` as the reference's cheat sheet says
//	opt       `x?`: zero or one of the literal or class Of
//	many      zero or more of Of; `x+` compiles to x followed by many x
type Token struct {
	T   string `json:"t"`
	C   string `json:"c,omitempty"`
	Set string `json:"set,omitempty"`
	Of  *Token `json:"of,omitempty"`
}

// Pattern is one entry of a filter list, compiled: Negated is a leading
// `!`, which has no meaning anywhere else in the pattern.
type Pattern struct {
	Text    string  `json:"text"`
	Negated bool    `json:"negated"`
	Tokens  []Token `json:"tokens"`
}

// CompileGlob compiles one filter pattern of the captured reference's
// cheat sheet. A form the reference does not define is refused with the
// reason, never guessed: a quantifier with nothing before it, after a
// wildcard or after another quantifier, or on a non-ASCII byte; a class
// that is empty, unterminated, descending or lists anything but letters and
// digits; a trailing escape; an empty pattern.
func CompileGlob(text string) (Pattern, error) {
	p := Pattern{Text: text, Tokens: []Token{}}
	if len(text) > MaxPatternBytes {
		return p, fmt.Errorf("the pattern is %d bytes; the bound is %d bytes", len(text), MaxPatternBytes)
	}
	body := text
	if strings.HasPrefix(body, "!") {
		p.Negated = true
		body = body[1:]
	}
	if body == "" {
		return p, errors.New("the pattern is empty")
	}
	toks := []Token{}
	for i := 0; i < len(body); {
		c := body[i]
		switch c {
		case '\\':
			if i+1 >= len(body) {
				return p, errors.New("a trailing \\ escape escapes nothing")
			}
			toks = appendLiteral(toks, body[i+1])
			i += 2
		case '*':
			switch {
			case strings.HasPrefix(body[i:], "**/"):
				toks = append(toks, Token{T: "dirs"})
				i += 3
			case strings.HasPrefix(body[i:], "**"):
				toks = append(toks, Token{T: "globstar"})
				i += 2
			default:
				toks = append(toks, Token{T: "star"})
				i++
			}
		case '?', '+':
			if len(toks) == 0 {
				return p, fmt.Errorf("%c has nothing before it to repeat", c)
			}
			prev := toks[len(toks)-1]
			switch prev.T {
			case "star", "globstar", "dirs":
				return p, fmt.Errorf("%c after a wildcard is not defined by the reference", c)
			case "opt", "many":
				return p, fmt.Errorf("%c after another quantifier is not defined by the reference", c)
			}
			if prev.T == "lit" {
				last := prev.C[len(prev.C)-1]
				if last >= 0x80 {
					return p, fmt.Errorf("%c on a non-ASCII character is not supported: patterns match bytes", c)
				}
				// ? takes the run's last byte alone (+ keeps it and adds
				// many of it)
				if c == '?' && len(prev.C) > 1 {
					toks[len(toks)-1] = Token{T: "lit", C: prev.C[:len(prev.C)-1]}
					toks = append(toks, Token{T: "lit", C: string([]byte{last})})
				}
				prev = Token{T: "lit", C: string([]byte{last})}
			}
			of := prev
			if c == '?' {
				toks[len(toks)-1] = Token{T: "opt", Of: &of}
			} else {
				toks = append(toks, Token{T: "many", Of: &of})
			}
			i++
		case '[':
			set, width, err := compileClass(body[i:])
			if err != nil {
				return p, err
			}
			toks = append(toks, Token{T: "class", Set: set})
			i += width
		default:
			toks = appendLiteral(toks, c)
			i++
		}
	}
	p.Tokens = toks
	return p, nil
}

// appendLiteral extends a trailing literal run by one byte, or starts one
// (a quantified literal is closed: `a?b` is opt a, then lit b)
func appendLiteral(toks []Token, c byte) []Token {
	if n := len(toks); n > 0 && toks[n-1].T == "lit" {
		toks[n-1].C += string([]byte{c})
		return toks
	}
	return append(toks, Token{T: "lit", C: string([]byte{c})})
}

func alnumKind(c byte) int {
	switch {
	case c >= 'a' && c <= 'z':
		return 1
	case c >= 'A' && c <= 'Z':
		return 2
	case c >= '0' && c <= '9':
		return 3
	}
	return 0
}

// compileClass reads `[...]` at the start of s: its byte set, sorted, and
// how many bytes of s it spans
func compileClass(s string) (string, int, error) {
	end := strings.IndexByte(s, ']')
	if end < 0 {
		return "", 0, errors.New("a [ class is not closed with ]")
	}
	inner := s[1:end]
	if inner == "" {
		return "", 0, errors.New("a [] class lists nothing")
	}
	seen := map[byte]bool{}
	for i := 0; i < len(inner); {
		a := inner[i]
		if alnumKind(a) == 0 {
			return "", 0, fmt.Errorf("a [ class lists %q; a class holds only letters, digits and their ranges", a)
		}
		if i+2 < len(inner) && inner[i+1] == '-' {
			b := inner[i+2]
			if alnumKind(b) != alnumKind(a) || b < a {
				return "", 0, fmt.Errorf("a [ class range %c-%c is not an ascending range of a-z, A-Z or 0-9", a, b)
			}
			for x := a; ; x++ {
				seen[x] = true
				if x == b {
					break
				}
			}
			i += 3
			continue
		}
		if i+1 < len(inner) && inner[i+1] == '-' {
			return "", 0, errors.New("a [ class range has no end")
		}
		seen[a] = true
		i++
	}
	set := make([]byte, 0, len(seen))
	for b := range seen {
		set = append(set, b)
	}
	sort.Slice(set, func(i, j int) bool { return set[i] < set[j] })
	return string(set), end + 1, nil
}

// one state of the byte-level NFA a pattern compiles to: a literal run
// is one state per byte; `**/` is an entry state, which may be skipped, and
// an inside state, which leaves only after a `/`
type nfaState struct {
	kind byte // b byte, c class, s star, g globstar, e dirs entry, i dirs inside, o opt, m many
	b    byte
	set  string
}

func (p Pattern) program() []nfaState {
	var prog []nfaState
	atom := func(t Token) nfaState {
		if t.T == "class" {
			return nfaState{kind: 'c', set: t.Set}
		}
		return nfaState{kind: 'b', b: t.C[0]}
	}
	for _, t := range p.Tokens {
		switch t.T {
		case "lit":
			for i := 0; i < len(t.C); i++ {
				prog = append(prog, nfaState{kind: 'b', b: t.C[i]})
			}
		case "class":
			prog = append(prog, nfaState{kind: 'c', set: t.Set})
		case "star":
			prog = append(prog, nfaState{kind: 's'})
		case "globstar":
			prog = append(prog, nfaState{kind: 'g'})
		case "dirs":
			prog = append(prog, nfaState{kind: 'e'}, nfaState{kind: 'i'})
		case "opt", "many":
			a := atom(*t.Of)
			k := byte('o')
			if t.T == "many" {
				k = 'm'
			}
			prog = append(prog, nfaState{kind: k, b: a.b, set: a.set})
		}
	}
	return prog
}

// whether an atom state (a byte or a class, or the atom an opt/many
// quantifies) takes c
func (s nfaState) takes(c byte) bool {
	if s.set != "" {
		return strings.IndexByte(s.set, c) >= 0
	}
	return s.b == c
}

// Match reports whether the compiled pattern matches the whole name: the
// NFA of its tokens, read one byte at a time, as the ship's matcher reads
// it (lib/ci-trigger). The negation is the caller's.
func (p Pattern) Match(name string) bool {
	prog := p.program()
	n := len(prog)
	closure := func(states []bool) {
		for k := 0; k < n; k++ {
			if !states[k] {
				continue
			}
			switch prog[k].kind {
			case 's', 'g', 'o', 'm':
				states[k+1] = true
			case 'e':
				states[k+2] = true
			}
		}
	}
	cur := make([]bool, n+1)
	cur[0] = true
	closure(cur)
	for i := 0; i < len(name); i++ {
		c := name[i]
		next := make([]bool, n+1)
		for k := 0; k < n; k++ {
			if !cur[k] {
				continue
			}
			st := prog[k]
			switch st.kind {
			case 'b', 'c':
				if st.takes(c) {
					next[k+1] = true
				}
			case 's':
				if c != '/' {
					next[k] = true
				}
			case 'g':
				next[k] = true
			case 'e':
				next[k+1] = true
				if c == '/' {
					next[k+2] = true
				}
			case 'i':
				next[k] = true
				if c == '/' {
					next[k+1] = true
				}
			case 'o':
				if st.takes(c) {
					next[k+1] = true
				}
			case 'm':
				if st.takes(c) {
					next[k] = true
				}
			}
		}
		closure(next)
		alive := false
		for _, v := range next {
			alive = alive || v
		}
		if !alive {
			return false
		}
		cur = next
	}
	return cur[n]
}
