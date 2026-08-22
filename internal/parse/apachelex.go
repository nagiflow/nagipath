package parse

import (
	"fmt"
	"strings"
)

// Apache's grammar is line-oriented with `<Section args>` … `</Section>` blocks,
// so it gets its own lexer rather than a shoehorn into the NGINX one. Offsets are
// byte offsets in the file, same contract.
func lexApache(path string, src []byte) ([]directive, error) {
	l := &apacheLexer{src: src, path: path}
	return l.block("", 0)
}

type apacheLexer struct {
	src  []byte
	path string
	pos  int
}

func (l *apacheLexer) block(closing string, depth int) ([]directive, error) {
	if depth > 32 {
		return nil, fmt.Errorf("%s: section nesting deeper than 32", l.path)
	}
	var out []directive
	for l.pos < len(l.src) {
		start, line, end := l.logicalLine()
		if line == "" {
			if l.pos >= len(l.src) {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "</") {
			name := strings.TrimSuffix(strings.TrimPrefix(line, "</"), ">")
			if closing != "" && !strings.EqualFold(strings.TrimSpace(name), closing) {
				return out, fmt.Errorf("%s: </%s> closes %s", l.path, name, closing)
			}
			return out, nil
		}
		if strings.HasPrefix(line, "<") {
			inner := strings.TrimSuffix(strings.TrimPrefix(line, "<"), ">")
			name, rest, _ := strings.Cut(inner, " ")
			d := directive{
				Name:    name,
				Args:    splitApacheArgs(rest),
				Path:    l.path,
				Start:   start,
				HeadEnd: end,
			}
			blk, err := l.block(name, depth+1)
			d.Block = blk
			d.End = l.pos
			out = append(out, d)
			if err != nil {
				return out, err
			}
			continue
		}
		name, rest, _ := strings.Cut(line, " ")
		out = append(out, directive{
			Name:    name,
			Args:    splitApacheArgs(rest),
			Path:    l.path,
			Start:   start,
			End:     end,
			HeadEnd: end,
		})
	}
	if closing != "" {
		return out, fmt.Errorf("%s: <%s> is never closed", l.path, closing)
	}
	return out, nil
}

// logicalLine returns one directive's worth of text, joining backslash
// continuations and stripping comments. start/end bracket the raw bytes.
func (l *apacheLexer) logicalLine() (start int, text string, end int) {
	// Skip blank lines and comments.
	for l.pos < len(l.src) {
		lineStart := l.pos
		lineEnd := l.pos
		for lineEnd < len(l.src) && l.src[lineEnd] != '\n' {
			lineEnd++
		}
		raw := strings.TrimSpace(strings.TrimRight(string(l.src[lineStart:lineEnd]), "\r"))
		l.pos = lineEnd
		if l.pos < len(l.src) {
			l.pos++ // consume the newline
		}
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		var b strings.Builder
		b.WriteString(strings.TrimSuffix(raw, "\\"))
		for strings.HasSuffix(raw, "\\") && l.pos < len(l.src) {
			nextEnd := l.pos
			for nextEnd < len(l.src) && l.src[nextEnd] != '\n' {
				nextEnd++
			}
			raw = strings.TrimSpace(strings.TrimRight(string(l.src[l.pos:nextEnd]), "\r"))
			lineEnd = nextEnd
			l.pos = nextEnd
			if l.pos < len(l.src) {
				l.pos++
			}
			b.WriteString(" ")
			b.WriteString(strings.TrimSuffix(raw, "\\"))
		}
		return lineStart, strings.TrimSpace(b.String()), lineEnd
	}
	return l.pos, "", l.pos
}

// splitApacheArgs honours double quotes, which Apache uses for paths with spaces
// and for regex patterns containing them.
func splitApacheArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case (c == ' ' || c == '\t') && !inQuote:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
