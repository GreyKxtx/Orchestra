// Package wiregen generates the client-side copies of the wire contract
// from protocol/wire's Go source: a JSON Schema, TypeScript for the VS Code
// extension, and the version and name constants the web page needs.
//
// It reads the package's source with go/parser rather than reflecting over
// a registry, so every exported struct in the package is in the output with
// its doc comments, and nothing has to be listed twice. A test in
// protocol/wire fails when the generated files on disk are stale.
package wiregen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/wire"
)

// Paths of the generated files, relative to the repository root.
const (
	SchemaPath = "protocol/wire/wire.schema.json"
	TSPath     = "ui/vscode/src/protocol/wire.generated.ts"
	JSPath     = "ui/web/src/01-wire.generated.js"
)

// Files is what Generate produces, keyed by path relative to the repository root.
type Files map[string][]byte

// Generate reads the wire package in wireDir (and protocol beside it) and
// renders every output.
func Generate(wireDir string) (Files, error) {
	m, err := load(wireDir)
	if err != nil {
		return nil, err
	}
	schema, err := renderSchema(m)
	if err != nil {
		return nil, err
	}
	return Files{
		SchemaPath: schema,
		TSPath:     renderTS(m),
		JSPath:     renderJS(),
	}, nil
}

// Write puts the generated files under repoRoot.
func Write(repoRoot string, files Files) error {
	for rel, data := range files {
		p := filepath.Join(repoRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ── the model ────────────────────────────────────────────────────────────────

type kind int

const (
	kString kind = iota
	kBool
	kInteger
	kNumber
	kAny
	kArray
	kMap
	kRef
	kEmptyObject
)

type typeRef struct {
	kind kind
	elem *typeRef // array items, map values
	ref  string   // kRef: the named type
}

type fieldDef struct {
	name     string // JSON name
	doc      string
	optional bool
	typ      typeRef
}

type typeDef struct {
	name   string
	doc    string
	file   string
	fields []fieldDef
	alias  *typeRef // a named non-struct type: `type Foo string`
}

type model struct {
	types  []*typeDef // declaration order, files sorted by name
	byName map[string]*typeDef
}

// load parses the wire package and the protocol types it refers to.
func load(wireDir string) (*model, error) {
	m := &model{byName: map[string]*typeDef{}}
	if err := m.loadDir(wireDir, "wire"); err != nil {
		return nil, err
	}
	if err := m.loadDir(filepath.Join(wireDir, ".."), "protocol"); err != nil {
		return nil, err
	}
	// Only the protocol types the wire reaches are part of the contract.
	reached := map[string]bool{}
	var walk func(t typeRef)
	walk = func(t typeRef) {
		switch t.kind {
		case kArray, kMap:
			walk(*t.elem)
		case kRef:
			if reached[t.ref] {
				return
			}
			reached[t.ref] = true
			if d := m.byName[t.ref]; d != nil {
				if d.alias != nil {
					walk(*d.alias)
				}
				for _, f := range d.fields {
					walk(f.typ)
				}
			}
		}
	}
	var kept []*typeDef
	for _, d := range m.types {
		if d.file != "protocol" {
			kept = append(kept, d)
			walk(typeRef{kind: kRef, ref: d.name})
		}
	}
	for _, d := range m.types {
		if d.file == "protocol" && reached[d.name] {
			kept = append(kept, d)
		}
	}
	m.types = kept
	for _, d := range m.types {
		for _, f := range d.fields {
			if err := m.check(f.typ, d.name+"."+f.name); err != nil {
				return nil, err
			}
		}
	}
	for _, s := range wire.Signatures() {
		for _, name := range []string{s.Params, s.Result} {
			if name != "" && m.byName[name] == nil {
				return nil, fmt.Errorf("signature of %s names %s, which is not a wire type", s.Method, name)
			}
		}
	}
	return m, nil
}

func (m *model) check(t typeRef, where string) error {
	switch t.kind {
	case kArray, kMap:
		return m.check(*t.elem, where)
	case kRef:
		if m.byName[t.ref] == nil {
			return fmt.Errorf("%s: type %s is not in protocol/wire or protocol", where, t.ref)
		}
	}
	return nil
}

func (m *model) loadDir(dir, label string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	fset := token.NewFileSet()
	for _, n := range names {
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, parser.ParseComments)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts := spec.(*ast.TypeSpec)
				if !ts.Name.IsExported() {
					continue
				}
				doc := gd.Doc
				if ts.Doc != nil {
					doc = ts.Doc
				}
				d := &typeDef{name: ts.Name.Name, doc: docText(doc), file: label}
				if label == "wire" {
					d.file = strings.TrimSuffix(n, ".go")
				}
				switch t := ts.Type.(type) {
				case *ast.StructType:
					for _, fld := range t.Fields.List {
						if len(fld.Names) == 0 {
							return fmt.Errorf("%s.%s: embedded fields are not supported on the wire", d.name, exprString(fld.Type))
						}
						for _, name := range fld.Names {
							if !name.IsExported() {
								continue
							}
							fd, skip, err := m.field(name.Name, fld)
							if err != nil {
								return fmt.Errorf("%s.%s: %w", d.name, name.Name, err)
							}
							if !skip {
								d.fields = append(d.fields, fd)
							}
						}
					}
				default:
					tr, err := m.typeRef(ts.Type)
					if err != nil {
						return fmt.Errorf("%s: %w", d.name, err)
					}
					d.alias = &tr
				}
				m.types = append(m.types, d)
				m.byName[d.name] = d
			}
		}
	}
	return nil
}

func (m *model) field(goName string, fld *ast.Field) (fieldDef, bool, error) {
	name, optional, skip := jsonTag(goName, fld.Tag)
	if skip {
		return fieldDef{}, true, nil
	}
	tr, err := m.typeRef(fld.Type)
	if err != nil {
		return fieldDef{}, false, err
	}
	if _, isPtr := fld.Type.(*ast.StarExpr); isPtr {
		optional = true
	}
	doc := docText(fld.Doc)
	if doc == "" && fld.Comment != nil {
		doc = docText(fld.Comment)
	}
	return fieldDef{name: name, doc: doc, optional: optional, typ: tr}, false, nil
}

// jsonTag reads the json struct tag: the name, omitempty, and "-".
func jsonTag(goName string, tag *ast.BasicLit) (name string, optional, skip bool) {
	name = goName
	if tag == nil {
		return name, false, false
	}
	raw := strings.Trim(tag.Value, "`")
	js, ok := reflect.StructTag(raw).Lookup("json")
	if !ok {
		return name, false, false
	}
	parts := strings.Split(js, ",")
	if parts[0] == "-" {
		return "", false, true
	}
	if parts[0] != "" {
		name = parts[0]
	}
	for _, p := range parts[1:] {
		if p == "omitempty" {
			optional = true
		}
	}
	return name, optional, false
}

func (m *model) typeRef(e ast.Expr) (typeRef, error) {
	switch t := e.(type) {
	case *ast.StarExpr:
		return m.typeRef(t.X)
	case *ast.ArrayType:
		if id, ok := t.Elt.(*ast.Ident); ok && id.Name == "byte" {
			return typeRef{kind: kString}, nil // base64 on the wire
		}
		elem, err := m.typeRef(t.Elt)
		if err != nil {
			return typeRef{}, err
		}
		return typeRef{kind: kArray, elem: &elem}, nil
	case *ast.MapType:
		if id, ok := t.Key.(*ast.Ident); !ok || id.Name != "string" {
			return typeRef{}, fmt.Errorf("map key %s: only string keys are JSON objects", exprString(t.Key))
		}
		elem, err := m.typeRef(t.Value)
		if err != nil {
			return typeRef{}, err
		}
		return typeRef{kind: kMap, elem: &elem}, nil
	case *ast.InterfaceType:
		return typeRef{kind: kAny}, nil
	case *ast.StructType:
		if len(t.Fields.List) == 0 {
			return typeRef{kind: kEmptyObject}, nil
		}
		return typeRef{}, fmt.Errorf("anonymous struct: name it")
	case *ast.SelectorExpr:
		pkg, _ := t.X.(*ast.Ident)
		switch {
		case pkg != nil && pkg.Name == "json" && t.Sel.Name == "RawMessage":
			return typeRef{kind: kAny}, nil
		case pkg != nil && pkg.Name == "time" && t.Sel.Name == "Time":
			return typeRef{kind: kString}, nil
		case pkg != nil && pkg.Name == "protocol":
			return typeRef{kind: kRef, ref: t.Sel.Name}, nil
		}
		return typeRef{}, fmt.Errorf("type %s: not a wire type", exprString(e))
	case *ast.Ident:
		switch t.Name {
		case "string":
			return typeRef{kind: kString}, nil
		case "bool":
			return typeRef{kind: kBool}, nil
		case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
			return typeRef{kind: kInteger}, nil
		case "float32", "float64":
			return typeRef{kind: kNumber}, nil
		case "any":
			return typeRef{kind: kAny}, nil
		case "byte", "rune", "error", "complex64", "complex128", "uintptr":
			return typeRef{}, fmt.Errorf("type %s: not a wire type", t.Name)
		}
		return typeRef{kind: kRef, ref: t.Name}, nil
	}
	return typeRef{}, fmt.Errorf("type %s: not a wire type", exprString(e))
}

func exprString(e ast.Expr) string {
	var b bytes.Buffer
	_ = printExpr(&b, e)
	return b.String()
}

func printExpr(b *bytes.Buffer, e ast.Expr) error {
	switch t := e.(type) {
	case *ast.Ident:
		b.WriteString(t.Name)
	case *ast.SelectorExpr:
		_ = printExpr(b, t.X)
		b.WriteString("." + t.Sel.Name)
	case *ast.StarExpr:
		b.WriteString("*")
		_ = printExpr(b, t.X)
	case *ast.ArrayType:
		b.WriteString("[]")
		_ = printExpr(b, t.Elt)
	case *ast.MapType:
		b.WriteString("map[")
		_ = printExpr(b, t.Key)
		b.WriteString("]")
		_ = printExpr(b, t.Value)
	default:
		fmt.Fprintf(b, "%T", e)
	}
	return nil
}

// docText is a comment group as one paragraph-preserving string, without
// the comment markers.
func docText(g *ast.CommentGroup) string {
	if g == nil {
		return ""
	}
	return strings.TrimSpace(g.Text())
}

// ── JSON Schema ──────────────────────────────────────────────────────────────

func schemaOf(t typeRef) map[string]any {
	switch t.kind {
	case kString:
		return map[string]any{"type": "string"}
	case kBool:
		return map[string]any{"type": "boolean"}
	case kInteger:
		return map[string]any{"type": "integer"}
	case kNumber:
		return map[string]any{"type": "number"}
	case kAny:
		return map[string]any{}
	case kArray:
		return map[string]any{"type": "array", "items": schemaOf(*t.elem)}
	case kMap:
		return map[string]any{"type": "object", "additionalProperties": schemaOf(*t.elem)}
	case kRef:
		return map[string]any{"$ref": "#/$defs/" + t.ref}
	case kEmptyObject:
		return map[string]any{"type": "object"}
	}
	return map[string]any{}
}

func renderSchema(m *model) ([]byte, error) {
	defs := map[string]any{}
	for _, d := range m.types {
		if d.alias != nil {
			s := schemaOf(*d.alias)
			if d.doc != "" {
				s["description"] = d.doc
			}
			defs[d.name] = s
			continue
		}
		props := map[string]any{}
		required := []string{}
		for _, f := range d.fields {
			s := schemaOf(f.typ)
			if f.doc != "" {
				s["description"] = f.doc
			}
			props[f.name] = s
			if !f.optional {
				required = append(required, f.name)
			}
		}
		s := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			s["required"] = required
		}
		if d.doc != "" {
			s["description"] = d.doc
		}
		defs[d.name] = s
	}
	sigs := map[string]any{}
	for _, s := range wire.Signatures() {
		entry := map[string]any{}
		if s.Params != "" {
			entry["params"] = map[string]any{"$ref": "#/$defs/" + s.Params}
		}
		if s.Result != "" {
			entry["result"] = map[string]any{"$ref": "#/$defs/" + s.Result}
		}
		sigs[s.Method] = entry
	}
	doc := map[string]any{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"title":       fmt.Sprintf("Orchestra wire contract, protocol v%d", protocol.ProtocolVersion),
		"description": "Generated from protocol/wire (go generate ./protocol/wire/...). The client↔core JSON-RPC contract: methods, notifications, requests, and the shape of every params, result and event.",
		"x-orchestra": map[string]any{
			"protocol_version":     protocol.ProtocolVersion,
			"min_protocol_version": protocol.MinProtocolVersion,
			"ops_version":          protocol.OpsVersion,
			"tools_version":        protocol.ToolsVersion,
			"methods":              wire.Methods(),
			"notifications":        wire.Notifications(),
			"requests":             wire.Requests(),
			"event_types":          wire.EventTypes(),
			"signatures":           sigs,
		},
		"$defs": defs,
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// ── TypeScript ───────────────────────────────────────────────────────────────

func tsOf(t typeRef) string {
	switch t.kind {
	case kString:
		return "string"
	case kBool:
		return "boolean"
	case kInteger, kNumber:
		return "number"
	case kAny:
		return "unknown"
	case kArray:
		inner := tsOf(*t.elem)
		if strings.Contains(inner, " ") {
			inner = "(" + inner + ")"
		}
		return inner + "[]"
	case kMap:
		return "Record<string, " + tsOf(*t.elem) + ">"
	case kRef:
		return t.ref
	case kEmptyObject:
		return "Record<string, never>"
	}
	return "unknown"
}

func tsDoc(b *strings.Builder, indent, doc string) {
	if doc == "" {
		return
	}
	lines := strings.Split(doc, "\n")
	if len(lines) == 1 {
		fmt.Fprintf(b, "%s/** %s */\n", indent, strings.ReplaceAll(lines[0], "*/", "* /"))
		return
	}
	fmt.Fprintf(b, "%s/**\n", indent)
	for _, l := range lines {
		l = strings.ReplaceAll(l, "*/", "* /")
		if l == "" {
			fmt.Fprintf(b, "%s *\n", indent)
		} else {
			fmt.Fprintf(b, "%s * %s\n", indent, l)
		}
	}
	fmt.Fprintf(b, "%s */\n", indent)
}

func tsStringList(name, doc string, values []string) string {
	var b strings.Builder
	tsDoc(&b, "", doc)
	fmt.Fprintf(&b, "export const %s = [\n", name)
	for _, v := range values {
		fmt.Fprintf(&b, "  %q,\n", v)
	}
	b.WriteString("] as const;\n")
	return b.String()
}

func renderTS(m *model) []byte {
	var b strings.Builder
	b.WriteString("// AUTO-GENERATED from protocol/wire — do not edit.\n")
	b.WriteString("// Regenerate: go generate ./protocol/wire/...  (a Go test fails while this file is stale)\n")
	b.WriteString("//\n// The client↔core contract: versions, the names on the wire, and the shape of\n")
	b.WriteString("// every params, result and event. Field meanings: protocol/wire/*.go.\n\n")
	fmt.Fprintf(&b, "/** The newest protocol version this contract describes. */\nexport const PROTOCOL_VERSION = %d;\n", protocol.ProtocolVersion)
	fmt.Fprintf(&b, "/** The oldest protocol version a core of this version still speaks. */\nexport const MIN_PROTOCOL_VERSION = %d;\n", protocol.MinProtocolVersion)
	fmt.Fprintf(&b, "/** Internal ops; must match the core's. */\nexport const OPS_VERSION = %d;\n", protocol.OpsVersion)
	fmt.Fprintf(&b, "/** The tools this contract was written against; informational to the core. */\nexport const TOOLS_VERSION = %d;\n\n", protocol.ToolsVersion)
	b.WriteString(tsStringList("METHODS", "Every method a connection answers.", wire.Methods()))
	b.WriteString("export type Method = (typeof METHODS)[number];\n\n")
	b.WriteString(tsStringList("NOTIFICATIONS", "Every notification the core sends.", wire.Notifications()))
	b.WriteString("export type Notification = (typeof NOTIFICATIONS)[number];\n\n")
	b.WriteString(tsStringList("REQUESTS", "Every request the core makes of the client.", wire.Requests()))
	b.WriteString("export type Request = (typeof REQUESTS)[number];\n\n")
	b.WriteString(tsStringList("EVENT_TYPES", "Every AgentEvent.type the core sends.", wire.EventTypes()))
	b.WriteString("export type AgentEventType = (typeof EVENT_TYPES)[number];\n")

	file := ""
	for _, d := range m.types {
		if d.file != file {
			file = d.file
			src := "protocol/wire/" + file + ".go"
			if file == "protocol" {
				src = "protocol/version.go"
			}
			fmt.Fprintf(&b, "\n// ── %s ──\n\n", src)
		} else {
			b.WriteString("\n")
		}
		tsDoc(&b, "", d.doc)
		if d.alias != nil {
			fmt.Fprintf(&b, "export type %s = %s;\n", d.name, tsOf(*d.alias))
			continue
		}
		if len(d.fields) == 0 {
			fmt.Fprintf(&b, "export type %s = Record<string, never>;\n", d.name)
			continue
		}
		fmt.Fprintf(&b, "export interface %s {\n", d.name)
		for _, f := range d.fields {
			tsDoc(&b, "  ", f.doc)
			opt := ""
			if f.optional {
				opt = "?"
			}
			fmt.Fprintf(&b, "  %s%s: %s;\n", tsName(f.name), opt, tsOf(f.typ))
		}
		b.WriteString("}\n")
	}

	b.WriteString("\n/** The params and result of each method, where they are on the wire. `unknown`\n")
	b.WriteString(" * marks a type that stays in internal/core because it carries a patch or\n")
	b.WriteString(" * config type; see docs/PROTOCOL.md for its shape. */\n")
	b.WriteString("export interface MethodSignatures {\n")
	for _, s := range wire.Signatures() {
		p, r := "unknown", "unknown"
		if s.Params != "" {
			p = s.Params
		}
		if s.Result != "" {
			r = s.Result
		}
		fmt.Fprintf(&b, "  %q: { params: %s; result: %s };\n", s.Method, p, r)
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

// tsName quotes a property name that is not an identifier.
func tsName(n string) string {
	for i, r := range n {
		if r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return fmt.Sprintf("%q", n)
	}
	return n
}

// ── the web's constants ──────────────────────────────────────────────────────

func jsList(name string, values []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "    %s: Object.freeze([\n", name)
	for _, v := range values {
		fmt.Fprintf(&b, "      %q,\n", v)
	}
	b.WriteString("    ]),\n")
	return b.String()
}

func renderJS() []byte {
	var b strings.Builder
	b.WriteString("  // AUTO-GENERATED from protocol/wire — do not edit.\n")
	b.WriteString("  // Regenerate: go generate ./protocol/wire/...  (a Go test fails while this file is stale)\n")
	b.WriteString("  //\n  // The versions and names of the client↔core contract, for the web adapter.\n")
	b.WriteString("  // A fragment of the web bundle: ui/web/scripts/bundle-web.mjs puts it right\n")
	b.WriteString("  // after the prelude, so every later fragment sees WIRE.\n")
	b.WriteString("  const WIRE = Object.freeze({\n")
	fmt.Fprintf(&b, "    PROTOCOL_VERSION: %d,\n", protocol.ProtocolVersion)
	fmt.Fprintf(&b, "    MIN_PROTOCOL_VERSION: %d,\n", protocol.MinProtocolVersion)
	fmt.Fprintf(&b, "    OPS_VERSION: %d,\n", protocol.OpsVersion)
	fmt.Fprintf(&b, "    TOOLS_VERSION: %d,\n", protocol.ToolsVersion)
	b.WriteString(jsList("METHODS", wire.Methods()))
	b.WriteString(jsList("NOTIFICATIONS", wire.Notifications()))
	b.WriteString(jsList("REQUESTS", wire.Requests()))
	b.WriteString(jsList("EVENT_TYPES", wire.EventTypes()))
	b.WriteString("  });\n")
	return []byte(b.String())
}
