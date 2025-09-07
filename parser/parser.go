package parser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"godoc-rag/detail"
	"os"
	"path/filepath"
	"strings"
)

type Parser struct {
	out  chan detail.Detail
	err  error
	path string
}

func New(path string) *Parser {
	return &Parser{
		out:  make(chan detail.Detail),
		err:  nil,
		path: path,
	}
}

func (p Parser) Error() error {
	return p.err
}

func (p *Parser) Parse() <-chan detail.Detail {
	go func() {
		fset := token.NewFileSet()
		p.err = filepath.Walk(p.path, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return nil
			}

			// Parse all Go files in this directory as a package
			pkgs, err := parser.ParseDir(fset, path, nil, parser.ParseComments)
			if err != nil {
				return nil
			}

			// Process each package
			for pkgName, pkg := range pkgs {
				// Process each file in the package
				for filename, file := range pkg.Files {
					p.parseAstFile(file, pkgName, filename)
				}
			}
			return nil
		})
		close(p.out)
	}()
	return p.out
}

func (p Parser) parseAstFile(node *ast.File, packageName, filename string) {
	// Package doc
	if node.Doc != nil {
		p.out <- detail.Detail{
			Type:     "package",
			Symbol:   packageName,
			Comment:  node.Doc.Text(),
			Package:  packageName,
			Filename: filename,
		}
	}

	// Walk through declarations
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				s, ok := spec.(*ast.TypeSpec)
				if !ok || !s.Name.IsExported() {
					continue
				}
				details := getTypeDetail(s, d)
				details.Package = packageName
				details.Filename = filename
				p.out <- details
			}

		case *ast.FuncDecl:
			if d.Doc == nil || !d.Name.IsExported() {
				continue
			}
			symbol := d.Name.Name
			if d.Recv != nil {
				recName := getTypeName(d.Recv.List[0].Type)
				symbol = recName + "." + symbol
			}
			p.out <- detail.Detail{
				Type:     "function",
				Symbol:   symbol,
				Comment:  d.Doc.Text(),
				Package:  packageName,
				Filename: filename,
			}
		}
	}
}

func getTypeName(t ast.Expr) string {
	switch r := t.(type) {
	case *ast.Ident:
		return r.Name
	case *ast.StarExpr:
		if typeIdent, ok := r.X.(*ast.Ident); ok {
			return "*" + typeIdent.Name
		}
	}
	return ""
}

func getTypeDetail(s *ast.TypeSpec, g *ast.GenDecl) detail.Detail {
	var details detail.Detail

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

			details.Children = append(details.Children, detail.Detail{
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

			details.Children = append(details.Children, detail.Detail{
				Type:    "method",
				Symbol:  fmt.Sprintf("%s.%s", details.Symbol, methodName),
				Comment: comment,
			})
		}
	}

	return details
}
