package main

// nftModel is a private model of the one nftables table the launcher owns,
// for the recorded-command host (D1): the commands the adapter issues are
// applied to it — never to the host — and `nft -a list table` answers its
// state in nft's listing form (each rule with its handle, interface names
// quoted). Like nft: `add table` and `add chain` are idempotent, `create
// chain` fails on an existing chain, a rule needs its chain, `insert`
// prepends and `add` appends, deleting a rule needs its handle in that
// chain, and deleting a chain fails while a rule jumps to it (its own rules
// go with it). skip names commands that report success but change nothing
// (a rule the host never got), and rewrite replaces a rule's text as it is
// stored (the host holding something else than was asked).

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type nftModelRule struct {
	handle int
	text   string
}

type nftModelChain struct {
	name, spec string
	handle     int
	rules      []nftModelRule
}

type nftModel struct {
	mu      sync.Mutex
	table   string
	exists  bool
	handle  int
	chains  []*nftModelChain
	skip    func(cmd string) bool
	rewrite func(text string) string
}

func newNFTModel(table string) *nftModel { return &nftModel{table: table} }

func (m *nftModel) next() int { m.handle++; return m.handle }

func (m *nftModel) find(name string) *nftModelChain {
	for _, c := range m.chains {
		if c.name == name {
			return c
		}
	}
	return nil
}

// render is a rule's text as nft lists it: interface names quoted.
func render(args []string) string {
	out := slices.Clone(args)
	for i := 0; i+1 < len(out); i++ {
		if out[i] == "iifname" || out[i] == "oifname" {
			out[i+1] = strconv.Quote(out[i+1])
		}
	}
	return strings.Join(out, " ")
}

func fail(msg string) (string, bool, error) {
	return "Error: " + msg + "\n", true, errors.New("exit status 1")
}

// apply runs one recorded nft command (its arguments joined by spaces, as
// the recorder keeps them) on the model.
func (m *nftModel) apply(cmd string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := strings.Fields(cmd)
	if len(f) < 2 || f[0] != "nft" {
		return "", true, nil
	}
	if f[1] == "-a" && len(f) == 6 && f[2] == "list" && f[3] == "table" {
		if !m.exists || f[5] != m.table {
			return fail("No such file or directory\nlist table inet " + f[5])
		}
		return m.list(), true, nil
	}
	if m.skip != nil && m.skip(cmd) {
		return "", true, nil
	}
	if len(f) < 5 || f[3] != "inet" || f[4] != m.table {
		return fail("unsupported by the model: " + cmd)
	}
	verb, obj, rest := f[1], f[2], f[5:]
	if obj == "table" {
		m.exists = true
		return "", true, nil
	}
	if !m.exists {
		return fail("No such file or directory")
	}
	switch {
	case obj == "chain" && (verb == "add" || verb == "create"):
		if c := m.find(rest[0]); c != nil {
			if verb == "create" {
				return fail("Could not process rule: File exists")
			}
			return "", true, nil
		}
		spec := ""
		if len(rest) > 1 {
			spec = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.Join(rest[1:], " "), "{"), "}"))
		}
		m.chains = append(m.chains, &nftModelChain{name: rest[0], spec: spec, handle: m.next()})
	case obj == "chain" && verb == "delete":
		c := m.find(rest[0])
		if c == nil {
			return fail("Could not process rule: No such file or directory")
		}
		for _, o := range m.chains {
			for _, r := range o.rules {
				if slices.Contains(strings.Fields(r.text), "jump") && slices.Contains(strings.Fields(r.text), c.name) {
					return fail("Could not process rule: Device or resource busy")
				}
			}
		}
		m.chains = slices.DeleteFunc(m.chains, func(o *nftModelChain) bool { return o == c })
	case obj == "rule" && (verb == "add" || verb == "insert"):
		c := m.find(rest[0])
		if c == nil {
			return fail("Could not process rule: No such file or directory")
		}
		text := render(rest[1:])
		if m.rewrite != nil {
			text = m.rewrite(text)
		}
		r := nftModelRule{handle: m.next(), text: text}
		if verb == "insert" {
			c.rules = append([]nftModelRule{r}, c.rules...)
		} else {
			c.rules = append(c.rules, r)
		}
	case obj == "rule" && verb == "delete" && len(rest) == 3 && rest[1] == "handle":
		c := m.find(rest[0])
		h, _ := strconv.Atoi(rest[2])
		if c == nil || !slices.ContainsFunc(c.rules, func(r nftModelRule) bool { return r.handle == h }) {
			return fail("Could not process rule: No such file or directory")
		}
		c.rules = slices.DeleteFunc(c.rules, func(r nftModelRule) bool { return r.handle == h })
	default:
		return fail("unsupported by the model: " + cmd)
	}
	return "", true, nil
}

// list is the table in nft's `-a` listing form; the lock is held.
func (m *nftModel) list() string {
	var b strings.Builder
	fmt.Fprintf(&b, "table inet %s { # handle 1\n", m.table)
	for _, c := range m.chains {
		fmt.Fprintf(&b, "\tchain %s { # handle %d\n", c.name, c.handle)
		if c.spec != "" {
			fmt.Fprintf(&b, "\t\t%s\n", c.spec)
		}
		for _, r := range c.rules {
			fmt.Fprintf(&b, "\t\t%s # handle %d\n", r.text, r.handle)
		}
		b.WriteString("\t}\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// listing is list under the lock (for tests that read the model).
func (m *nftModel) listing() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.list()
}

// dropRule removes, behind the adapter's back, every rule of chain whose
// text contains all of words (what an operator or another tool might do).
func (m *nftModel) dropRule(chain string, words ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c := m.find(chain); c != nil {
		c.rules = slices.DeleteFunc(c.rules, func(r nftModelRule) bool {
			return !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(r.text, w) })
		})
	}
}

// dropChain removes, behind the adapter's back, a chain and every rule
// jumping to it.
func (m *nftModel) dropChain(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chains = slices.DeleteFunc(m.chains, func(c *nftModelChain) bool { return c.name == name })
	for _, c := range m.chains {
		c.rules = slices.DeleteFunc(c.rules, func(r nftModelRule) bool {
			f := strings.Fields(r.text)
			return slices.Contains(f, "jump") && slices.Contains(f, name)
		})
	}
}
