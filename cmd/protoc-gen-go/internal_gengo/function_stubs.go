// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal_gengo

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/internal/strs"
)

// The protocheck module hosts the RFC-001 §9.1 engine SPI (Engine, Function,
// Violation) that generated function stubs program against. The import is
// resolved lazily through QualifiedGoIdent, so files without `function`
// declarations do not depend on it.
var protocheckPackage goImportPath = protogen.GoImportPath("github.com/trendvidia/protocheck/v2")

// genFunctionStubs generates the RFC-001 §9.3 stubs for the `function`
// declarations carried in the file's options (extension 1328): a Functions
// interface, an UnimplementedFunctions placeholder, and a RegisterFunctions
// helper binding implementations to a validation engine by fully-qualified
// name. Files without function declarations generate nothing.
func genFunctionStubs(g *protogen.GeneratedFile, f *fileInfo) {
	var decls []schemaFunctionDecl
	for _, d := range fileFunctions(f) {
		if d.Name != "" {
			decls = append(decls, d)
		}
	}
	if len(decls) == 0 {
		return
	}
	aliases := fileTypeAliases(f)

	// Method names: Go-camel-cased final FQN segment, "_"-suffixed on clash.
	methods := make([]string, len(decls))
	usedMethods := make(map[string]bool)
	for i, d := range decls {
		name := d.Name
		if j := strings.LastIndexByte(name, '.'); j >= 0 {
			name = name[j+1:]
		}
		name = strs.GoCamelCase(name)
		for name == "" || usedMethods[name] {
			name += "_"
		}
		usedMethods[name] = true
		methods[i] = name
	}

	violationIdent := protocheckPackage.Ident("Violation")

	g.P("// Functions declares the schema `function` implementations required by")
	g.P("// ", f.Desc.Path(), " (RFC-001 §9.3). A validation engine invokes these at")
	g.P("// runtime for @validate rules that call them. Implementations typically")
	g.P("// embed UnimplementedFunctions and override the functions they provide.")
	g.P("type Functions interface {")
	for i, d := range decls {
		if i > 0 {
			g.P()
		}
		g.P("// ", methods[i], " implements the schema function ", strconv.Quote(d.Name), ".")
		if opt, ok := d.Options["description"]; ok && opt.Kind == schemaArgString && opt.String != "" {
			g.P("//")
			for _, line := range strings.Split(opt.String, "\n") {
				g.P("// ", line)
			}
		}
		if reason, ok := functionDeprecationReason(d); ok {
			g.P("//")
			g.P("// Deprecated: ", reason)
		}
		g.P(methods[i], "(", functionParamSignature(d, aliases, true), ") (bool, *", violationIdent, ")")
	}
	g.P("}")
	g.P()

	g.P("// UnimplementedFunctions is a placeholder implementation of Functions whose")
	g.P("// methods all fail with the reserved ", protocheckPackage.Ident("CodeFunctionUnimplemented"), " violation")
	g.P("// (RFC-001 §7), per RFC-001 §9.2's lenient registration model.")
	g.P("type UnimplementedFunctions struct{}")
	g.P()
	for i, d := range decls {
		g.P("func (UnimplementedFunctions) ", methods[i], "(", functionParamSignature(d, aliases, false), ") (bool, *", violationIdent, ") {")
		g.P("return false, &", violationIdent, "{Code: ", protocheckPackage.Ident("CodeFunctionUnimplemented"), ", FallbackMessage: ", protocheckPackage.Ident("MsgFunctionUnimplemented"), "(", strconv.Quote(d.Name), ")}")
		g.P("}")
		g.P()
	}
	g.P("var _ Functions = UnimplementedFunctions{}")
	g.P()

	g.P("// RegisterFunctions binds each schema function declared in ", f.Desc.Path())
	g.P("// to eng by its fully-qualified name, adapting the typed methods of impl to")
	g.P("// the engine's []any calling convention. It returns the first registration")
	g.P("// error encountered.")
	g.P("func RegisterFunctions(eng ", protocheckPackage.Ident("Engine"), ", impl Functions) error {")
	for i, d := range decls {
		g.P("if err := eng.Register(", strconv.Quote(d.Name), ", func(args []any) (bool, *", violationIdent, ") {")
		g.P("if len(args) != ", len(d.Params), " {")
		g.P("return false, &", violationIdent, "{Code: ", protocheckPackage.Ident("CodeFunctionInvalidArgument"), ", FallbackMessage: ", protocheckPackage.Ident("MsgFunctionArity"), "(", strconv.Quote(d.Name), ", ", len(d.Params), ")}")
		g.P("}")
		var callArgs []string
		for j, p := range d.Params {
			goType := schemaGoType(p.Type, aliases)
			if goType == "any" {
				callArgs = append(callArgs, fmt.Sprintf("args[%d]", j))
				continue
			}
			// The guard asserts the resolved Go type, but the message names
			// the parameter type as declared in the schema (RFC-001 §7's
			// MsgFunctionArgType contract) — never the host-language type.
			g.P("a", j, ", ok := args[", j, "].(", goType, ")")
			g.P("if !ok {")
			g.P("return false, &", violationIdent, "{Code: ", protocheckPackage.Ident("CodeFunctionInvalidArgument"), ", FallbackMessage: ", protocheckPackage.Ident("MsgFunctionArgType"), "(", strconv.Quote(d.Name), ", ", j, ", ", strconv.Quote(p.Type), ")}")
			g.P("}")
			callArgs = append(callArgs, fmt.Sprintf("a%d", j))
		}
		g.P("return impl.", methods[i], "(", strings.Join(callArgs, ", "), ")")
		g.P("}); err != nil {")
		g.P("return err")
		g.P("}")
	}
	g.P("return nil")
	g.P("}")
	g.P()
}

// functionParamSignature renders the Go parameter list for a function
// declaration. withNames includes parameter names (for the interface);
// without, only types are emitted (for UnimplementedFunctions methods).
func functionParamSignature(d schemaFunctionDecl, aliases map[string]string, withNames bool) string {
	var parts []string
	usedNames := make(map[string]bool)
	for i, p := range d.Params {
		goType := schemaGoType(p.Type, aliases)
		if !withNames {
			parts = append(parts, goType)
			continue
		}
		name := strs.GoSanitized(p.Name)
		if p.Name == "" || usedNames[name] {
			name = fmt.Sprintf("arg%d", i)
		}
		usedNames[name] = true
		parts = append(parts, name+" "+goType)
	}
	return strings.Join(parts, ", ")
}

// schemaGoType maps a schema parameter type FQN to the Go type used in the
// generated signature. Same-file type aliases resolve through their base
// type; enum, message, and cross-file alias parameters degrade to any, as
// engines pass those values without a portable static type.
func schemaGoType(fqn string, aliases map[string]string) string {
	seen := make(map[string]bool)
	for {
		switch fqn {
		case "string":
			return "string"
		case "bool":
			return "bool"
		case "bytes":
			return "[]byte"
		case "int32", "sint32", "sfixed32":
			return "int32"
		case "int64", "sint64", "sfixed64":
			return "int64"
		case "uint32", "fixed32":
			return "uint32"
		case "uint64", "fixed64":
			return "uint64"
		case "float":
			return "float32"
		case "double":
			return "float64"
		}
		base, ok := aliases[fqn]
		if !ok || seen[fqn] {
			return "any"
		}
		seen[fqn] = true
		fqn = base
	}
}

// functionDeprecationReason reports whether the declaration carries a
// bracket-form deprecated option, and the reason to render.
func functionDeprecationReason(d schemaFunctionDecl) (string, bool) {
	opt, ok := d.Options["deprecated"]
	if !ok {
		return "", false
	}
	switch {
	case opt.Kind == schemaArgString && opt.String != "":
		return opt.String, true
	case opt.Kind == schemaArgBool && !opt.Bool:
		return "", false
	default:
		return "Do not use.", true
	}
}
