// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal_gengo

import (
	"math"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"google.golang.org/protobuf/types/descriptorpb"
)

// This file reads the RFC-001 schema-extension carriers out of descriptor
// options. The carriers are extensions in protowire's registered 1327–1331
// range, defined in protowire's proto/schema/v1/descriptor.proto. Those
// numbers live inside the 1314–1363 block the Protocol Buffers global
// extension registry granted protowire (protocolbuffers/protobuf#28919);
// they previously sat at 50400–50404, an unregistered range, which is
// retired and must never be reused. They are
// decoded from unknown fields to avoid a dependency on the schema/v1 Go
// module (the same approach as isTrackedMessage in init.go).

// Carrier extension field numbers (proto/schema/v1/descriptor.proto).
const (
	// AnnotationList; shared across every Options message kind.
	schemaAnnotationsFieldNumber = 1327
	// FileFunctions; FileOptions only.
	schemaFunctionsFieldNumber = 1328
	// FileTypeDecls; FileOptions only.
	schemaTypeDeclsFieldNumber = 1330
)

// Fully-qualified names of the built-in annotations from
// protowire/proto/schema/v1/annotations.proto that codegen surfaces.
const (
	schemaDescriptionAnnotation = "protowire.schema.v1.description"
	schemaDeprecatedAnnotation  = "protowire.schema.v1.deprecated"
)

// schemaAnnotation is one lowered `@name(args)` use site
// (protowire.schema.v1.Annotation).
type schemaAnnotation struct {
	Name string // fully-qualified, e.g. "protowire.schema.v1.description"
	Args []schemaAnnotationArg
}

type schemaArgKind int

const (
	schemaArgNone schemaArgKind = iota
	schemaArgString
	schemaArgInt
	schemaArgFloat
	schemaArgBool
	schemaArgBytes
	schemaArgOther // literal or expression; opaque to codegen
)

// schemaAnnotationArg is one annotation argument
// (protowire.schema.v1.AnnotationArg). Name is empty for positional args.
type schemaAnnotationArg struct {
	Name   string
	Kind   schemaArgKind
	String string
	Int    int64
	Float  float64
	Bool   bool
	Bytes  []byte
}

// schemaFunctionDecl is one lowered `function` declaration
// (protowire.schema.v1.FunctionDecl).
type schemaFunctionDecl struct {
	Name    string // package-qualified FQN, e.g. "myco.commons.is_e164"
	Params  []schemaFunctionParam
	Options map[string]schemaAnnotationArg // unqualified option name → value
}

type schemaFunctionParam struct {
	Name string
	Type string // FQN: primitive, enum, message, or type alias
}

// readAnnotations decodes the AnnotationList carrier (field 1327) from the
// unknown fields of any Options message. Multiple carrier occurrences are
// concatenated per proto merge semantics.
func readAnnotations(opts proto.Message) []schemaAnnotation {
	var anns []schemaAnnotation
	forEachMessageField(opts.ProtoReflect().GetUnknown(), schemaAnnotationsFieldNumber, func(v []byte) {
		anns = append(anns, parseAnnotationList(v)...)
	})
	return anns
}

// fileFunctions decodes the FileFunctions carrier (field 1328) from the
// file's options.
func fileFunctions(f *fileInfo) []schemaFunctionDecl {
	var decls []schemaFunctionDecl
	opts := f.Desc.Options().(*descriptorpb.FileOptions)
	forEachMessageField(opts.ProtoReflect().GetUnknown(), schemaFunctionsFieldNumber, func(v []byte) {
		decls = append(decls, parseFileFunctions(v)...)
	})
	return decls
}

// fileTypeAliases decodes the FileTypeDecls carrier (field 1330) into a
// alias-name → base-type-FQN map, used to resolve function parameter types
// declared as same-file type aliases.
func fileTypeAliases(f *fileInfo) map[string]string {
	var aliases map[string]string
	opts := f.Desc.Options().(*descriptorpb.FileOptions)
	forEachMessageField(opts.ProtoReflect().GetUnknown(), schemaTypeDeclsFieldNumber, func(v []byte) {
		forEachMessageField(v, 1, func(v []byte) { // repeated TypeDecl declarations = 1
			var name, base string
			forEachStringField(v, map[protowire.Number]*string{
				1: &name, // name
				2: &base, // base_type_fqn
			})
			if name != "" && base != "" {
				if aliases == nil {
					aliases = make(map[string]string)
				}
				aliases[name] = base
			}
		})
	})
	return aliases
}

// parseAnnotationList decodes a protowire.schema.v1.AnnotationList.
func parseAnnotationList(b []byte) []schemaAnnotation {
	var anns []schemaAnnotation
	forEachMessageField(b, 1, func(v []byte) { // repeated Annotation entries = 1
		anns = append(anns, parseAnnotation(v))
	})
	return anns
}

// parseAnnotation decodes a protowire.schema.v1.Annotation.
func parseAnnotation(b []byte) schemaAnnotation {
	var a schemaAnnotation
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return a
		}
		b = b[n:]
		if typ == protowire.BytesType {
			v, m := protowire.ConsumeBytes(b)
			if m < 0 {
				return a
			}
			switch num {
			case 1: // name
				a.Name = string(v)
			case 2: // args
				a.Args = append(a.Args, parseAnnotationArg(v))
			}
			b = b[m:]
			continue
		}
		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			return a
		}
		b = b[m:]
	}
	return a
}

// parseAnnotationArg decodes a protowire.schema.v1.AnnotationArg.
func parseAnnotationArg(b []byte) schemaAnnotationArg {
	var a schemaAnnotationArg
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return a
		}
		b = b[n:]
		var m int
		switch {
		case num == 1 && typ == protowire.BytesType: // name
			var v []byte
			v, m = protowire.ConsumeBytes(b)
			if m >= 0 {
				a.Name = string(v)
			}
		case num == 10 && typ == protowire.BytesType: // string_value
			var v []byte
			v, m = protowire.ConsumeBytes(b)
			if m >= 0 {
				a.Kind, a.String = schemaArgString, string(v)
			}
		case num == 11 && typ == protowire.VarintType: // int_value
			var v uint64
			v, m = protowire.ConsumeVarint(b)
			if m >= 0 {
				a.Kind, a.Int = schemaArgInt, int64(v)
			}
		case num == 12 && typ == protowire.Fixed64Type: // double_value
			var v uint64
			v, m = protowire.ConsumeFixed64(b)
			if m >= 0 {
				a.Kind, a.Float = schemaArgFloat, math.Float64frombits(v)
			}
		case num == 13 && typ == protowire.VarintType: // bool_value
			var v uint64
			v, m = protowire.ConsumeVarint(b)
			if m >= 0 {
				a.Kind, a.Bool = schemaArgBool, protowire.DecodeBool(v)
			}
		case num == 14 && typ == protowire.BytesType: // bytes_value
			var v []byte
			v, m = protowire.ConsumeBytes(b)
			if m >= 0 {
				a.Kind, a.Bytes = schemaArgBytes, append([]byte(nil), v...)
			}
		case (num == 15 || num == 20) && typ == protowire.BytesType: // literal, expression
			m = protowire.ConsumeFieldValue(num, typ, b)
			if m >= 0 {
				a.Kind = schemaArgOther
			}
		default:
			m = protowire.ConsumeFieldValue(num, typ, b)
		}
		if m < 0 {
			return a
		}
		b = b[m:]
	}
	return a
}

// parseFileFunctions decodes a protowire.schema.v1.FileFunctions.
func parseFileFunctions(b []byte) []schemaFunctionDecl {
	var decls []schemaFunctionDecl
	forEachMessageField(b, 1, func(v []byte) { // repeated FunctionDecl declarations = 1
		decls = append(decls, parseFunctionDecl(v))
	})
	return decls
}

// parseFunctionDecl decodes a protowire.schema.v1.FunctionDecl.
func parseFunctionDecl(b []byte) schemaFunctionDecl {
	var d schemaFunctionDecl
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return d
		}
		b = b[n:]
		if typ == protowire.BytesType {
			v, m := protowire.ConsumeBytes(b)
			if m < 0 {
				return d
			}
			switch num {
			case 1: // name
				d.Name = string(v)
			case 2: // params
				var p schemaFunctionParam
				forEachStringField(v, map[protowire.Number]*string{
					1: &p.Name,
					2: &p.Type,
				})
				d.Params = append(d.Params, p)
			case 3: // options map entry
				var key string
				var val schemaAnnotationArg
				forEachMessageField(v, 2, func(v []byte) { val = parseAnnotationArg(v) })
				forEachStringField(v, map[protowire.Number]*string{1: &key})
				if key != "" {
					if d.Options == nil {
						d.Options = make(map[string]schemaAnnotationArg)
					}
					d.Options[key] = val
				}
			}
			b = b[m:]
			continue
		}
		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			return d
		}
		b = b[m:]
	}
	return d
}

// forEachMessageField invokes fn with every length-delimited occurrence of
// field num within message bytes b. Malformed input terminates the scan.
func forEachMessageField(b []byte, num protowire.Number, fn func(v []byte)) {
	for len(b) > 0 {
		fnum, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return
		}
		b = b[n:]
		if fnum == num && typ == protowire.BytesType {
			v, m := protowire.ConsumeBytes(b)
			if m < 0 {
				return
			}
			fn(v)
			b = b[m:]
			continue
		}
		m := protowire.ConsumeFieldValue(fnum, typ, b)
		if m < 0 {
			return
		}
		b = b[m:]
	}
}

// forEachStringField assigns the last occurrence of each requested
// length-delimited field within message bytes b to its destination.
func forEachStringField(b []byte, dsts map[protowire.Number]*string) {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return
		}
		b = b[n:]
		if dst, ok := dsts[num]; ok && typ == protowire.BytesType {
			v, m := protowire.ConsumeBytes(b)
			if m < 0 {
				return
			}
			*dst = string(v)
			b = b[m:]
			continue
		}
		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			return
		}
		b = b[m:]
	}
}

// stringArg returns the string value of the argument named name, falling
// back to the first positional string argument.
func (a schemaAnnotation) stringArg(name string) (string, bool) {
	for _, arg := range a.Args {
		if arg.Name == name && arg.Kind == schemaArgString {
			return arg.String, true
		}
	}
	for _, arg := range a.Args {
		if arg.Name == "" && arg.Kind == schemaArgString {
			return arg.String, true
		}
	}
	return "", false
}

// annotationsDescription returns the text of the last @description
// annotation. Carrier lists are ordered base-to-derived (type-alias
// expansions precede the use site's own annotations), so the last match is
// the most specific description for the declaration being documented.
func annotationsDescription(anns []schemaAnnotation) (string, bool) {
	for i := len(anns) - 1; i >= 0; i-- {
		if anns[i].Name == schemaDescriptionAnnotation {
			return anns[i].stringArg("text")
		}
	}
	return "", false
}

// annotationsDeprecated reports whether an @deprecated annotation is
// present, and its (possibly empty) reason — like annotationsDescription,
// preferring the most-derived match.
func annotationsDeprecated(anns []schemaAnnotation) (string, bool) {
	for i := len(anns) - 1; i >= 0; i-- {
		if anns[i].Name == schemaDeprecatedAnnotation {
			reason, _ := anns[i].stringArg("reason")
			return reason, true
		}
	}
	return "", false
}

// appendSchemaAnnotationComments augments a declaration's leading comments
// from its @description and @deprecated annotations: the description becomes
// the leading paragraph of the doc comment, and @deprecated appends a
// standard "Deprecated:" notice. Existing comments are preserved; a
// description already present in them, or an existing deprecation notice
// (e.g. from the classic deprecated option), is not duplicated.
func appendSchemaAnnotationComments(comments protogen.Comments, opts proto.Message) protogen.Comments {
	anns := readAnnotations(opts)
	if len(anns) == 0 {
		return comments
	}
	if text, ok := annotationsDescription(anns); ok && text != "" && !strings.Contains(string(comments), text) {
		desc := protogen.Comments(" " + strings.ReplaceAll(text, "\n", "\n ") + "\n")
		if comments != "" {
			comments = desc + "\n" + comments
		} else {
			comments = desc
		}
	}
	if reason, ok := annotationsDeprecated(anns); ok && !strings.Contains(string(comments), "Deprecated:") {
		if reason == "" {
			reason = "Do not use."
		}
		if comments != "" {
			comments += "\n"
		}
		comments += protogen.Comments(" Deprecated: " + strings.ReplaceAll(reason, "\n", "\n ") + "\n")
	}
	return comments
}
