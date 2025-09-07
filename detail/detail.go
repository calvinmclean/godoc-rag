package detail

import "strings"

type Detail struct {
	Type     string // package, function, struct, interface
	Symbol   string // type name like Person or method like Person.Greet
	Comment  string
	Package  string
	Filename string

	Children []Detail
}

func (d Detail) StringIndent(indent string) string {
	var sb strings.Builder
	sb.WriteString(indent)
	sb.WriteString(d.Type)
	sb.WriteRune(' ')
	sb.WriteString(d.Symbol)
	sb.WriteRune(':')
	sb.WriteRune(' ')
	sb.WriteString(strings.TrimSpace(strings.ReplaceAll(d.Comment, "\n", " ")))

	for _, child := range d.Children {
		sb.WriteRune('\n')
		sb.WriteString(child.StringIndent(indent + "  "))
	}

	return sb.String()
}

func (d Detail) String() string {
	return d.StringIndent("")
}
