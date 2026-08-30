// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// wire writes down the ZAP wire of the messages this node's RPC answers with.
//
// A field on that wire IS an offset, and an id is [32]byte — bytes_fixed[32] in
// the IDL — which the reflective encoder refuses outright. It refuses on purpose:
// the layout knows the shape, so a schema states an id correctly, and the type is
// expected to state its own wire rather than quietly acquiring one by reflection.
// Until it does, a reply carrying an id cannot cross the op-call plane at all,
// and most of what a P-Chain or X-Chain call answers with carries one.
//
// Two passes, because the two halves know different things.
//
//	go run ./cmd/wire -scan     reads the SOURCE for every gorilla/rpc handler
//	                            and writes messages.go, the list of what those
//	                            handlers take and return
//	go run ./cmd/wire           reads those TYPES and writes each package's
//	                            zap_gen.go from [zip.LayoutOf]
//
// The second pass reads the types by importing them, so the module has to
// compile for it to run. Across a zip upgrade that changes what is emitted, the
// files on disk are the thing stopping it: delete them first.
//
//	find . -name zap_gen.go -delete && go run ./cmd/wire
//
// Neither pass derives a layout of its own. The offsets a codec states are the
// ones the plane encodes against, which is what lets a codec go out one node at
// a time: the node that has it and the node that has not are speaking one wire.
//
// A message the derivation refuses is REPORTED, never guessed at. Those are the
// ones whose Go type has no layout to state — a map, an interface, a fixed array
// of anything but bytes — and the answer is to change the type, which is not
// something a generator gets to do.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/zap-proto/zip"
)

// mod is this module, and the only tree wire writes into without being told to.
const mod = "github.com/luxfi/node"

func main() {
	var (
		root = flag.String("root", ".", "the module's root directory")
		scan = flag.Bool("scan", false, "re-read the handlers and write messages.go")
		at   = mapping{}
	)
	flag.Var(&at, "at", "where another module's tree is, as importpath=dir; repeatable")
	flag.Parse()

	var err error
	if *scan {
		err = handlers(*root)
	} else {
		err = codecs(*root, at)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type mapping map[string]string

func (m mapping) String() string { return fmt.Sprint(map[string]string(m)) }

func (m mapping) Set(v string) error {
	path, dir, ok := strings.Cut(v, "=")
	if !ok {
		return fmt.Errorf("want importpath=dir, got %q", v)
	}
	m[path] = dir
	return nil
}

// ---- the codecs ------------------------------------------------------------

func codecs(root string, at mapping) error {
	var (
		roots   []reflect.Type
		refused []string
	)
	for _, m := range messages {
		t := reflect.TypeOf(m).Elem()
		if _, err := zip.LayoutOf(t); err != nil {
			refused = append(refused, fmt.Sprintf("%s.%s: %v", t.PkgPath(), t.Name(), err))
			continue
		}
		roots = append(roots, t)
	}
	written, err := zip.Codecs(roots...)
	if err != nil {
		return err
	}

	var stated, elsewhere []string
	for _, c := range written {
		dir, ok := where(root, c.Path, at)
		if !ok {
			// Another module's types, and this command is not writing into a
			// module cache. Silent ones are what stop a message crossing; ones
			// that already answer for themselves need nothing said about them.
			if !c.Stated {
				elsewhere = append(elsewhere, fmt.Sprintf("%s (%d types)", c.Path, len(c.Types)))
			}
			continue
		}
		file := filepath.Join(dir, "zap_gen.go")
		old, _ := os.ReadFile(file)
		if !bytes.Equal(old, c.Source) {
			if err := os.WriteFile(file, c.Source, 0o644); err != nil {
				return err
			}
		}
		stated = append(stated, fmt.Sprintf("%s: %s", c.Path, strings.Join(c.Types, " ")))
	}

	sort.Strings(stated)
	for _, s := range stated {
		fmt.Println(s)
	}
	if len(refused) > 0 {
		sort.Strings(refused)
		fmt.Printf("\n%d messages have no layout to state; the TYPE has to change:\n", len(refused))
		for _, r := range dedup(refused) {
			fmt.Println("  " + r)
		}
	}
	if len(elsewhere) > 0 {
		sort.Strings(elsewhere)
		return fmt.Errorf("these packages are in another module and state no wire yet;\n"+
			"write them there with -at importpath=dir, then release and bump:\n  %s",
			strings.Join(elsewhere, "\n  "))
	}
	return nil
}

// where is the directory a package's source lives in, or false when this
// invocation was not told. Writing into a module cache would produce a codec
// that exists on one machine and in no release.
func where(root, path string, at mapping) (string, bool) {
	if rest, ok := strings.CutPrefix(path, mod); ok {
		return filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(rest, "/"))), true
	}
	for m, dir := range at {
		if rest, ok := strings.CutPrefix(path, m); ok {
			return filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(rest, "/"))), true
		}
	}
	return "", false
}

func dedup(s []string) []string {
	out := s[:0]
	var last string
	for _, v := range s {
		if v != last {
			out = append(out, v)
		}
		last = v
	}
	return out
}

// ---- the handlers ----------------------------------------------------------

// op is one registered method, by the types it takes and returns.
type op struct{ in, out string } // each "<importpath>.<Name>"

// handlers reads every gorilla/rpc-shaped method in the tree and writes the
// message list. The shape gorilla registers is exactly
//
//	func (r *T) M(*http.Request, *In, *Out) error
//
// so that is the whole predicate, and reading it from the source is what keeps
// the list from being something somebody has to remember to add to.
func handlers(root string) error {
	seen := map[string]bool{}
	var found []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", "testdata", ".git", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		pkg := mod
		if rel != "." {
			pkg = mod + "/" + filepath.ToSlash(rel)
		}
		imports := importsOf(f)
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || !fn.Name.IsExported() {
				continue
			}
			params, ok := gorilla(fn)
			if !ok {
				continue
			}
			for _, p := range params {
				name := spell(p, imports, pkg)
				if name == "" || seen[name] {
					continue
				}
				seen[name] = true
				found = append(found, name)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(found)
	return write(filepath.Join(root, "cmd", "wire", "messages.go"), found)
}

// gorilla reports the (*http.Request, *In, *Out) error shape and returns the two
// payload types.
func gorilla(fn *ast.FuncDecl) ([]ast.Expr, bool) {
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return nil, false
	}
	if id, ok := fn.Type.Results.List[0].Type.(*ast.Ident); !ok || id.Name != "error" {
		return nil, false
	}
	var params []ast.Expr
	for _, f := range fn.Type.Params.List {
		n := max(len(f.Names), 1)
		for range n {
			params = append(params, f.Type)
		}
	}
	if len(params) != 3 {
		return nil, false
	}
	star, ok := params[0].(*ast.StarExpr)
	if !ok {
		return nil, false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "http" || sel.Sel.Name != "Request" {
		return nil, false
	}
	return params[1:], true
}

func importsOf(f *ast.File) map[string]string {
	m := map[string]string{}
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		name := p[strings.LastIndex(p, "/")+1:]
		if im.Name != nil {
			name = im.Name.Name
		}
		m[name] = p
	}
	return m
}

// spell renders a payload as <importpath>.<Name>, and empty for the shapes that
// have no name to state: an empty struct carries nothing, and an interface is
// what the derivation refuses.
func spell(e ast.Expr, imports map[string]string, self string) string {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	switch t := e.(type) {
	case *ast.Ident:
		if t.Name == "any" || !t.IsExported() {
			return ""
		}
		return self + "." + t.Name
	case *ast.SelectorExpr:
		x, ok := t.X.(*ast.Ident)
		if !ok || !t.Sel.IsExported() {
			return ""
		}
		if p, ok := imports[x.Name]; ok {
			return p + "." + t.Sel.Name
		}
	}
	return ""
}

func write(file string, found []string) error {
	alias := map[string]string{}
	taken := map[string]bool{}
	var body bytes.Buffer
	for _, name := range found {
		path, ident, _ := cut(name)
		a, ok := alias[path]
		if !ok {
			a = path[strings.LastIndex(path, "/")+1:]
			for n := 2; taken[a]; n++ {
				a = path[strings.LastIndex(path, "/")+1:] + strconv.Itoa(n)
			}
			taken[a], alias[path] = true, a
		}
		fmt.Fprintf(&body, "\t&%s.%s{},\n", a, ident)
	}

	var out bytes.Buffer
	fmt.Fprint(&out, "// Code generated by cmd/wire -scan; DO NOT EDIT.\n\npackage main\n\nimport (\n")
	paths := make([]string, 0, len(alias))
	for p := range alias {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		fmt.Fprintf(&out, "\t%s %q\n", alias[p], p)
	}
	fmt.Fprint(&out, ")\n\n")
	fmt.Fprint(&out, "// messages is what this node's registered RPC methods take and return.\n")
	fmt.Fprint(&out, "var messages = []any{\n")
	out.Write(body.Bytes())
	fmt.Fprint(&out, "}\n")

	src, err := format.Source(out.Bytes())
	if err != nil {
		return fmt.Errorf("%s does not parse: %w", file, err)
	}
	return os.WriteFile(file, src, 0o644)
}

func cut(name string) (path, ident string, ok bool) {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return "", "", false
	}
	return name[:i], name[i+1:], true
}
