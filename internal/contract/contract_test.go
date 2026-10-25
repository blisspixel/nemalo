package contract

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/fields.txt after a reviewed field addition")

const module = "github.com/blisspixel/nemalo/"

// unversioned are user-facing configuration and CLI/MCP results without a
// schema_version field of their own.
var unversioned = []string{
	"internal/config.Config",
	"internal/app.Doctor", "internal/assessment.Report", "internal/inventory.Report",
	"internal/discovery.Evaluation", "internal/discovery.Page",
	"internal/library.Audit", "internal/library.Page", "internal/library.ManagedAudit", "internal/library.ExportResult",
	"internal/library.State", "internal/library.Bookmark", "internal/library.BookmarkState",
	"internal/jobs.Job", "internal/mcpserver.Envelope", "internal/mcpserver.Capabilities",
}

type declared struct {
	pkg    string
	fields *ast.StructType
	file   *ast.File
}

// index parses every non-test Go file under internal and returns its struct
// types by "internal/pkg.Type", with the versioned ones.
func index(t *testing.T) (map[string]declared, []string) {
	t.Helper()
	all := map[string]declared{}
	var versioned []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(filepath.Join("..", "..", "internal"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Join("..", ".."), filepath.Dir(p))
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(rel)
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts := spec.(*ast.TypeSpec)
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				key := pkg + "." + ts.Name.Name
				all[key] = declared{pkg, st, file}
				for _, f := range st.Fields.List {
					if name, _ := jsonName(f); name == "schema_version" {
						versioned = append(versioned, key)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return all, versioned
}

// jsonName is the encoded name and options of a field, as encoding/json uses.
func jsonName(f *ast.Field) (string, string) {
	if f.Tag == nil {
		return "", ""
	}
	tag, err := strconv.Unquote(f.Tag.Value)
	if err != nil {
		return "", ""
	}
	name, options, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
	return name, options
}

// resolve finds the struct a field's element type names, in this module.
func resolve(all map[string]declared, d declared, expr ast.Expr) (string, bool) {
	for {
		switch e := expr.(type) {
		case *ast.StarExpr:
			expr = e.X
			continue
		case *ast.ArrayType:
			expr = e.Elt
			continue
		case *ast.MapType:
			expr = e.Value
			continue
		case *ast.Ident:
			key := d.pkg + "." + e.Name
			_, ok := all[key]
			return key, ok
		case *ast.SelectorExpr:
			alias, ok := e.X.(*ast.Ident)
			if !ok {
				return "", false
			}
			for _, imp := range d.file.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				name := path.Base(p)
				if imp.Name != nil {
					name = imp.Name.Name
				}
				if name == alias.Name && strings.HasPrefix(p, module) {
					key := strings.TrimPrefix(p, module) + "." + e.Sel.Name
					_, ok := all[key]
					return key, ok
				}
			}
		}
		return "", false
	}
}

func expand(all map[string]declared, key, prefix string, seen []string, out *[]string) {
	d := all[key]
	if slices.Contains(seen, key) || len(seen) > 16 {
		*out = append(*out, prefix+" (recursive "+key+")")
		return
	}
	seen = append(seen, key)
	for _, f := range d.fields.Fields.List {
		name, options := jsonName(f)
		if name == "-" {
			continue
		}
		if len(f.Names) == 0 { // Embedded: inlined unless it has its own name.
			if inner, ok := resolve(all, d, f.Type); ok && name == "" {
				expand(all, inner, prefix, seen, out)
				continue
			}
			if name == "" {
				name = types.ExprString(f.Type)
			}
		}
		for i := 0; i < max(1, len(f.Names)); i++ {
			field := name
			if len(f.Names) > 0 {
				if !ast.IsExported(f.Names[i].Name) {
					continue
				}
				if field == "" {
					field = f.Names[i].Name
				}
			}
			at := strings.TrimPrefix(prefix+"."+field, ".")
			line := at + " " + types.ExprString(f.Type)
			if options != "" {
				line += " ," + options
			}
			*out = append(*out, line)
			if inner, ok := resolve(all, d, f.Type); ok {
				expand(all, inner, at+elements(f.Type), seen, out)
			}
		}
	}
}

// elements marks the collection levels between a field and its struct.
func elements(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return elements(e.X)
	case *ast.ArrayType:
		return "[]" + elements(e.Elt)
	case *ast.MapType:
		return "{}" + elements(e.Value)
	}
	return ""
}

func current(t *testing.T) []string {
	all, versioned := index(t)
	roots := append(slices.Clone(versioned), unversioned...)
	slices.Sort(roots)
	var lines []string
	for _, root := range slices.Compact(roots) {
		if _, ok := all[root]; !ok {
			t.Fatalf("contract root %s not found", root)
		}
		var fields []string
		expand(all, root, "", nil, &fields)
		for _, f := range fields {
			lines = append(lines, root+" "+f)
		}
	}
	slices.Sort(lines)
	return lines
}

// Every versioned record and result keeps the fields it has: within a
// schema version, fields are only added, deliberately (decision 0097).
func TestVersionedFieldsOnlyGrow(t *testing.T) {
	lines := current(t)
	golden := filepath.Join("testdata", "fields.txt")
	if *update {
		if err := os.WriteFile(golden, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	b, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n")
	var removed, added []string
	for _, l := range want {
		if _, found := slices.BinarySearch(lines, l); !found {
			removed = append(removed, l)
		}
	}
	for _, l := range lines {
		if _, found := slices.BinarySearch(want, l); !found {
			added = append(added, l)
		}
	}
	if len(removed) > 0 {
		t.Errorf("fields removed, renamed or retyped; that needs a new schema version and a decision, not -update:\n%s", strings.Join(removed, "\n"))
	}
	if len(added) > 0 {
		t.Errorf("new fields; review them, then run go test ./internal/contract -update:\n%s", strings.Join(added, "\n"))
	}
}

// The contract must see real versioned types, nested fields and other
// packages, or an empty snapshot would pass.
func TestContractCoversKnownFormats(t *testing.T) {
	lines := strings.Join(current(t), "\n")
	for _, want := range []string{
		"internal/library.Holding schema_version int",
		"internal/library.Holding sources []Source",
		"internal/cli.Envelope data any",
		"internal/config.Config library string",
		"internal/assessment.Report antivirus.status string",
		"internal/content.Result mp4_pcm.full_pcm_sha256 string",
		"internal/mcpserver.Envelope data T",
		"internal/library.ImportResult plan string ,omitempty",
	} {
		if !strings.Contains(lines, "\n"+want) && !strings.HasPrefix(lines, want) {
			t.Errorf("contract misses %q", want)
		}
	}
}
