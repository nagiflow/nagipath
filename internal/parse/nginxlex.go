package parse

import (
	"fmt"
	"strings"
)

// directive is one node of the concrete syntax tree. Start/End bracket the whole
// directive including its block, in bytes, in the file named by Path. Offsets are
// byte offsets and not rune offsets, so provenance stays exact under UTF-8 and
// CRLF alike.
type directive struct {
	Name  string
	Args  []string
	Block []directive
	Path  string
	Start int
	End   int
	// HeadEnd is the end of the directive's own line, before the block. Used when
	// a Rule wants to point at `location /x` rather than at the whole body.
	HeadEnd int
}

func (d directive) raw(src []byte) string {
	if d.Start < 0 || d.End > len(src) || d.Start >= d.End {
		return d.Name
	}
	return string(src[d.Start:d.End])
}

func (d directive) arg(i int) string {
	if i < len(d.Args) {
		return d.Args[i]
	}
	return ""
}

func (d directive) argsJoined() string { return strings.Join(d.Args, " ") }

type nginxLexer struct {
	src  []byte
	path string
	pos  int
}

// lexNginx parses NGINX-syntax configuration. The same block/directive/semicolon
// grammar covers every NGINX-derived config we care about.
func lexNginx(path string, src []byte) ([]directive, error) {
	l := &nginxLexer{src: src, path: path}
	ds, err := l.block(0)
	if err != nil {
		return ds, err
	}
	return ds, nil
}

func (l *nginxLexer) block(depth int) ([]directive, error) {
	if depth > 32 {
		return nil, fmt.Errorf("%s: block nesting deeper than 32", l.path)
	}
	var out []directive
	for {
		l.skipSpace()
		if l.pos >= len(l.src) {
			if depth > 0 {
				return out, fmt.Errorf("%s: unclosed block at end of file", l.path)
			}
			return out, nil
		}
		if l.src[l.pos] == '}' {
			if depth == 0 {
				return out, fmt.Errorf("%s: unexpected '}' at byte %d", l.path, l.pos)
			}
			l.pos++
			return out, nil
		}

		start := l.pos
		var words []string
		terminator := byte(0)
		for {
			l.skipSpaceSameDirective()
			if l.pos >= len(l.src) {
				terminator = 0
				break
			}
			c := l.src[l.pos]
			if c == ';' || c == '{' {
				terminator = c
				l.pos++
				break
			}
			if c == '}' {
				terminator = '}'
				break
			}
			w, ok := l.word()
			if !ok {
				break
			}
			words = append(words, w)
		}
		if len(words) == 0 {
			if terminator == '}' {
				continue
			}
			if l.pos >= len(l.src) {
				return out, nil
			}
			continue
		}

		d := directive{
			Name:    words[0],
			Args:    words[1:],
			Path:    l.path,
			Start:   start,
			HeadEnd: l.pos,
		}
		if terminator == '{' {
			blk, err := l.block(depth + 1)
			d.Block = blk
			d.End = l.pos
			out = append(out, d)
			if err != nil {
				return out, err
			}
			continue
		}
		d.End = l.pos
		out = append(out, d)
		if terminator == 0 && l.pos >= len(l.src) {
			return out, nil
		}
	}
}

func (l *nginxLexer) skipSpace() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			l.pos++
		case c == '#':
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		default:
			return
		}
	}
}

// skipSpaceSameDirective is the same, but comments inside a directive's argument
// list are skipped too — nginx allows `listen 80; # comment` and, rarely,
// comments between arguments.
func (l *nginxLexer) skipSpaceSameDirective() { l.skipSpace() }

func (l *nginxLexer) word() (string, bool) {
	if l.pos >= len(l.src) {
		return "", false
	}
	c := l.src[l.pos]
	if c == '"' || c == '\'' {
		quote := c
		l.pos++
		var b strings.Builder
		for l.pos < len(l.src) {
			ch := l.src[l.pos]
			if ch == '\\' && l.pos+1 < len(l.src) {
				b.WriteByte(l.src[l.pos+1])
				l.pos += 2
				continue
			}
			if ch == quote {
				l.pos++
				return b.String(), true
			}
			b.WriteByte(ch)
			l.pos++
		}
		return b.String(), true // unterminated quote: take what we have
	}
	start := l.pos
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' ||
			ch == ';' || ch == '{' || ch == '}' || ch == '#' {
			break
		}
		l.pos++
	}
	if start == l.pos {
		l.pos++ // never stall
		return "", false
	}
	return string(l.src[start:l.pos]), true
}
