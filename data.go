package godocrag

import "strings"

// Data is output from the embedded data
type Data struct {
	Type     string // package, function, struct, interface
	Symbol   string // type name like Person or method like Person.Greet
	Data     string
	Package  string
	Filename string

	// children is just used during parsing in order to construct nested symbol names
	children []Data
}

func (d *Data) AddChild(child Data) {
	d.children = append(d.children, child)
}

func (d Data) StringIndent(indent string) string {
	var sb strings.Builder
	sb.WriteString(indent)
	sb.WriteString(d.Type)
	sb.WriteRune(' ')
	sb.WriteString(d.Symbol)
	sb.WriteRune(':')
	sb.WriteRune(' ')
	sb.WriteString(strings.TrimSpace(strings.ReplaceAll(d.Data, "\n", " ")))

	for _, child := range d.children {
		sb.WriteRune('\n')
		sb.WriteString(child.StringIndent(indent + "  "))
	}

	return sb.String()
}

func (d Data) String() string {
	return d.StringIndent("")
}
