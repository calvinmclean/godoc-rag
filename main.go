package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

type Detail struct {
	Type    string // package, function, struct, interface
	Symbol  string // type name like Person or method like Person.Greet
	Comment string
	Package string

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

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <file.go>")
		return
	}

	filename := os.Args[1]
	fset := token.NewFileSet()

	// Parse with comments
	node, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		fmt.Println("Error parsing file:", err)
		return
	}

	var result []Detail

	packageName := node.Name.Name

	// Package doc
	if node.Doc != nil {
		result = append(result, Detail{
			Type:    "package",
			Symbol:  packageName,
			Comment: node.Doc.Text(),
			Package: packageName,
		})
	}

	// Walk through declarations
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					details := getTypeDetail(s, d)
					details.Package = packageName
					result = append(result, details)
				}
			}

		case *ast.FuncDecl:
			if d.Doc != nil {
				symbol := d.Name.Name
				if d.Recv != nil {
					recName := getTypeName(d.Recv.List[0].Type)
					symbol = recName + "." + symbol
				}
				result = append(result, Detail{
					Type:    "function",
					Symbol:  symbol,
					Comment: d.Doc.Text(),
					Package: packageName,
				})
			}
		}
	}

	for _, detail := range result {
		fmt.Println("-----")
		fmt.Println(detail)
	}
}

func getTypeName(t ast.Expr) string {
	var recvType string
	if typeIdent, ok := t.(*ast.Ident); ok {
		recvType = typeIdent.Name
	} else if starExpr, ok := t.(*ast.StarExpr); ok {
		if typeIdent, ok := starExpr.X.(*ast.Ident); ok {
			recvType = "*" + typeIdent.Name
		}
	}
	return recvType
}

func getTypeDetail(s *ast.TypeSpec, g *ast.GenDecl) Detail {
	var details Detail

	typeKind := "type"
	switch t := s.Type.(type) {
	case *ast.StructType:
		typeKind = "struct"
	case *ast.InterfaceType:
		typeKind = "interface"
	case *ast.ArrayType:
		typeKind = "array"
	case *ast.MapType:
		typeKind = "map"
	case *ast.ChanType:
		typeKind = "channel"
	case *ast.FuncType:
		typeKind = "function"
	case *ast.Ident:
		typeKind = fmt.Sprintf("alias for %s", t.Name)
	}
	details.Type = typeKind
	details.Symbol = s.Name.Name

	if g.Doc != nil {
		details.Comment = g.Doc.Text()
	}

	switch t := s.Type.(type) {
	case *ast.StructType:
		for _, field := range t.Fields.List {
			names := []string{}
			for _, n := range field.Names {
				names = append(names, n.Name)
			}
			fieldName := strings.Join(names, ", ")
			if fieldName == "" { // embedded struct
				fieldName = fmt.Sprintf("embedded %s", fmt.Sprint(field.Type))
			}

			// Print field type
			fieldType := fmt.Sprint(field.Type)

			var comment string
			if field.Doc != nil {
				comment += field.Doc.Text()
			}
			if field.Comment != nil {
				comment += field.Comment.Text()
			}

			details.Children = append(details.Children, Detail{
				Type:    fieldType,
				Symbol:  fmt.Sprintf("%s.%s", details.Symbol, fieldName),
				Comment: comment,
			})
		}
	case *ast.InterfaceType:
		for _, method := range t.Methods.List {
			names := []string{}
			for _, n := range method.Names {
				names = append(names, n.Name)
			}
			methodName := strings.Join(names, ", ")
			if methodName == "" { // embedded interface
				methodName = fmt.Sprintf("embedded %s", fmt.Sprint(method.Type))
			}
			var comment string
			if method.Doc != nil {
				comment += method.Doc.Text()
			}
			if method.Comment != nil {
				comment += method.Comment.Text()
			}

			details.Children = append(details.Children, Detail{
				Type:    "method",
				Symbol:  fmt.Sprintf("%s.%s", details.Symbol, methodName),
				Comment: comment,
			})
		}
	}

	return details
}
