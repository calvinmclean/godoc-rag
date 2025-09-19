package parser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	godocrag "godoc-rag"

	"golang.org/x/tools/go/packages"
)

type Parser struct {
	out  chan godocrag.Data
	err  error
	path string
}

func New(path string) *Parser {
	return &Parser{
		out:  make(chan godocrag.Data),
		err:  nil,
		path: path,
	}
}

func (p Parser) Error() error {
	return p.err
}

func (p *Parser) Parse() <-chan godocrag.Data {
	go func() {
		pkgs, err := packages.Load(&packages.Config{
			Mode: packages.NeedName | packages.NeedFiles,
		}, p.path)
		if err != nil {
			p.err = err
			return
		}

		fset := token.NewFileSet()
		for _, pkg := range pkgs {
			for _, fname := range pkg.GoFiles {
				f, err := parser.ParseFile(fset, fname, nil, parser.ParseComments)
				if err != nil {
					panic(err)
				}
				p.parseAstFile(f, pkg.PkgPath, fname)
			}
		}

		close(p.out)
	}()
	return p.out
}

func (p Parser) parseAstFile(node *ast.File, packageName, filename string) {
	// Package doc
	if node.Doc != nil {
		p.out <- godocrag.Data{
			Type:     "package",
			Symbol:   packageName,
			Data:     node.Doc.Text(),
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
				data := getTypeData(s, d)
				data.Package = packageName
				data.Filename = filename
				p.out <- data
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
			p.out <- godocrag.Data{
				Type:     "function",
				Symbol:   symbol,
				Data:     d.Doc.Text(),
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

func getTypeData(s *ast.TypeSpec, g *ast.GenDecl) godocrag.Data {
	var data godocrag.Data

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
	data.Type = typeKind
	data.Symbol = s.Name.Name

	if g.Doc != nil {
		data.Data = g.Doc.Text()
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

			data.AddChild(godocrag.Data{
				Type:   fieldType,
				Symbol: fmt.Sprintf("%s.%s", data.Symbol, fieldName),
				Data:   comment,
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

			data.AddChild(godocrag.Data{
				Type:   "method",
				Symbol: fmt.Sprintf("%s.%s", data.Symbol, methodName),
				Data:   comment,
			})
		}
	}

	return data
}
