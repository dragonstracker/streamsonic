package config

import (
	"fmt"
	"strings"
)

type token struct {
	text string
	kind byte
	line int
}

func tokenize(src string) ([]token, error) {
	out := make([]token, 0, 256)
	line := 1
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '{' || c == '}' || c == ';':
			out = append(out, token{text: string(c), kind: c, line: line})
			i++
		case c == '"':
			i++
			var b strings.Builder
			closed := false
			for i < len(src) {
				if src[i] == '\\' && i+1 < len(src) {
					i++
					b.WriteByte(src[i])
					i++
					continue
				}
				if src[i] == '"' {
					i++
					closed = true
					break
				}
				if src[i] == '\n' {
					line++
				}
				b.WriteByte(src[i])
				i++
			}
			if !closed {
				return nil, fmt.Errorf("line %d: unterminated string", line)
			}
			out = append(out, token{text: b.String(), kind: 's', line: line})
		default:
			start := i
			for i < len(src) {
				d := src[i]
				if d == ' ' || d == '\t' || d == '\r' || d == '\n' || d == '{' || d == '}' || d == ';' || d == '"' {
					break
				}
				i++
			}
			if i > start {
				out = append(out, token{text: src[start:i], kind: 'w', line: line})
			}
		}
	}
	return out, nil
}

type Node struct {
	Name     string   `json:"name"`
	Args     []string `json:"args,omitempty"`
	Children []*Node  `json:"children,omitempty"`
	Block    bool     `json:"block,omitempty"`
	Line     int      `json:"-"`
}

func (n *Node) Arg(i int) string {
	if i < len(n.Args) {
		return n.Args[i]
	}
	return ""
}

func (n *Node) Child(name string) *Node {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (n *Node) ChildrenNamed(name string) []*Node {
	var out []*Node
	for _, c := range n.Children {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

type parser struct {
	toks []token
	pos  int
}

func Parse(src string) ([]*Node, error) {
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	nodes, err := p.parseNodes(false)
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

func (p *parser) peek() (token, bool) {
	if p.pos >= len(p.toks) {
		return token{}, false
	}
	return p.toks[p.pos], true
}

func (p *parser) parseNodes(inBlock bool) ([]*Node, error) {
	out := make([]*Node, 0, 8)
	for {
		t, ok := p.peek()
		if !ok {
			if inBlock {
				return nil, fmt.Errorf("unexpected end of file: missing '}'")
			}
			return out, nil
		}
		if t.kind == '}' {
			if !inBlock {
				return nil, fmt.Errorf("line %d: unexpected '}'", t.line)
			}
			p.pos++
			return out, nil
		}
		if t.kind == ';' {
			p.pos++
			continue
		}
		if t.kind != 'w' && t.kind != 's' {
			return nil, fmt.Errorf("line %d: unexpected %q", t.line, t.text)
		}
		p.pos++
		n := &Node{Name: t.text, Line: t.line}
		for {
			a, ok := p.peek()
			if !ok || (a.kind != 'w' && a.kind != 's') {
				break
			}
			if k := len(n.Args); k > 0 && a.kind == 's' && strings.HasSuffix(n.Args[k-1], "=") {
				n.Args[k-1] += a.text
			} else {
				n.Args = append(n.Args, a.text)
			}
			p.pos++
		}
		next, ok := p.peek()
		switch {
		case ok && next.kind == '{':
			p.pos++
			n.Block = true
			children, err := p.parseNodes(true)
			if err != nil {
				return nil, err
			}
			n.Children = children
		case ok && next.kind == ';':
			p.pos++
		case !ok:
			return nil, fmt.Errorf("line %d: '%s' is missing ';'", t.line, n.Name)
		default:
			return nil, fmt.Errorf("line %d: unexpected %q after '%s'", next.line, next.text, n.Name)
		}
		out = append(out, n)
	}
}

func quote(s string) string {
	if s == "" {
		return `""`
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '"' || c == ';' || c == '{' || c == '}' || c == '#' {
			return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
		}
	}
	return s
}

func Render(nodes []*Node) string {
	var b strings.Builder
	renderNodes(&b, nodes, 0)
	return b.String()
}

func renderNodes(b *strings.Builder, nodes []*Node, depth int) {
	pad := strings.Repeat("  ", depth)
	for _, n := range nodes {
		b.WriteString(pad)
		b.WriteString(n.Name)
		for _, a := range n.Args {
			b.WriteString(" ")
			b.WriteString(quote(a))
		}
		if !n.Block {
			b.WriteString(";\n")
			continue
		}
		b.WriteString(" {\n")
		renderNodes(b, n.Children, depth+1)
		b.WriteString(pad)
		b.WriteString("}\n")
	}
}
