// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal_gengo

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"

	"google.golang.org/protobuf/types/descriptorpb"
)

// Wire-format encoders for the protowire.schema.v1 carrier messages, used to
// synthesize the bytes the protocompile lowering pass embeds in descriptor
// options (proto/schema/v1/descriptor.proto).

func appendBytesField(b []byte, num protowire.Number, v []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendBytes(b, v)
}

func appendStringField(b []byte, num protowire.Number, s string) []byte {
	return appendBytesField(b, num, []byte(s))
}

// encodeStringArg encodes an AnnotationArg with string_value (field 10).
func encodeStringArg(name, value string) []byte {
	var b []byte
	if name != "" {
		b = appendStringField(b, 1, name)
	}
	return appendStringField(b, 10, value)
}

// encodeIntArg encodes an AnnotationArg with int_value (field 11).
func encodeIntArg(name string, value int64) []byte {
	var b []byte
	if name != "" {
		b = appendStringField(b, 1, name)
	}
	b = protowire.AppendTag(b, 11, protowire.VarintType)
	return protowire.AppendVarint(b, uint64(value))
}

// encodeBoolArg encodes an AnnotationArg with bool_value (field 13).
func encodeBoolArg(name string, value bool) []byte {
	var b []byte
	if name != "" {
		b = appendStringField(b, 1, name)
	}
	b = protowire.AppendTag(b, 13, protowire.VarintType)
	return protowire.AppendVarint(b, protowire.EncodeBool(value))
}

// encodeAnnotation encodes an Annotation with the given FQN and args.
func encodeAnnotation(name string, args ...[]byte) []byte {
	b := appendStringField(nil, 1, name)
	for _, arg := range args {
		b = appendBytesField(b, 2, arg)
	}
	return b
}

// encodeAnnotationList encodes an AnnotationList from encoded Annotations.
func encodeAnnotationList(anns ...[]byte) []byte {
	var b []byte
	for _, ann := range anns {
		b = appendBytesField(b, 1, ann)
	}
	return b
}

// encodeFunctionParam encodes a FunctionParam.
func encodeFunctionParam(name, typ string) []byte {
	b := appendStringField(nil, 1, name)
	return appendStringField(b, 2, typ)
}

// encodeFunctionOption encodes one FunctionDecl.options map entry.
func encodeFunctionOption(key string, arg []byte) []byte {
	b := appendStringField(nil, 1, key)
	return appendBytesField(b, 2, arg)
}

// encodeFunctionDecl encodes a FunctionDecl from pre-encoded params (field 2)
// and options map entries (field 3).
func encodeFunctionDecl(name string, params [][]byte, options [][]byte) []byte {
	b := appendStringField(nil, 1, name)
	for _, p := range params {
		b = appendBytesField(b, 2, p)
	}
	for _, o := range options {
		b = appendBytesField(b, 3, o)
	}
	return b
}

// encodeFileFunctions encodes a FileFunctions from encoded FunctionDecls.
func encodeFileFunctions(decls ...[]byte) []byte {
	var b []byte
	for _, d := range decls {
		b = appendBytesField(b, 1, d)
	}
	return b
}

// encodeTypeDecl encodes a TypeDecl with name and base_type_fqn.
func encodeTypeDecl(name, base string) []byte {
	b := appendStringField(nil, 1, name)
	return appendStringField(b, 2, base)
}

// encodeFileTypeDecls encodes a FileTypeDecls from encoded TypeDecls.
func encodeFileTypeDecls(decls ...[]byte) []byte {
	var b []byte
	for _, d := range decls {
		b = appendBytesField(b, 1, d)
	}
	return b
}

func TestReadAnnotations(t *testing.T) {
	opts := &descriptorpb.MessageOptions{}
	raw := appendBytesField(nil, schemaAnnotationsFieldNumber, encodeAnnotationList(
		encodeAnnotation(schemaDescriptionAnnotation, encodeStringArg("", "a registered user")),
		encodeAnnotation("myco.tag",
			encodeStringArg("name", "auth"),
			encodeIntArg("weight", 3),
			encodeBoolArg("pii", true)),
	))
	// A second carrier occurrence must concatenate per proto merge semantics.
	raw = appendBytesField(raw, schemaAnnotationsFieldNumber, encodeAnnotationList(
		encodeAnnotation(schemaDeprecatedAnnotation, encodeStringArg("reason", "use Account")),
	))
	opts.ProtoReflect().SetUnknown(protoreflect.RawFields(raw))

	anns := readAnnotations(opts)
	if len(anns) != 3 {
		t.Fatalf("readAnnotations returned %d annotations, want 3: %+v", len(anns), anns)
	}
	if anns[0].Name != schemaDescriptionAnnotation {
		t.Errorf("anns[0].Name = %q, want %q", anns[0].Name, schemaDescriptionAnnotation)
	}
	if text, ok := annotationsDescription(anns); !ok || text != "a registered user" {
		t.Errorf("annotationsDescription = %q, %v, want %q, true", text, ok, "a registered user")
	}
	if reason, ok := annotationsDeprecated(anns); !ok || reason != "use Account" {
		t.Errorf("annotationsDeprecated = %q, %v, want %q, true", reason, ok, "use Account")
	}

	tag := anns[1]
	if tag.Name != "myco.tag" || len(tag.Args) != 3 {
		t.Fatalf("anns[1] = %+v, want myco.tag with 3 args", tag)
	}
	if got, ok := tag.stringArg("name"); !ok || got != "auth" {
		t.Errorf(`stringArg("name") = %q, %v, want "auth", true`, got, ok)
	}
	if tag.Args[1].Kind != schemaArgInt || tag.Args[1].Int != 3 {
		t.Errorf("args[1] = %+v, want int 3", tag.Args[1])
	}
	if tag.Args[2].Kind != schemaArgBool || !tag.Args[2].Bool {
		t.Errorf("args[2] = %+v, want bool true", tag.Args[2])
	}
}

func TestReadAnnotationsIgnoresOtherAndMalformed(t *testing.T) {
	opts := &descriptorpb.MessageOptions{}
	// Unrelated unknown field plus a truncated carrier must not panic.
	raw := appendStringField(nil, 99999, "unrelated")
	raw = protowire.AppendTag(raw, schemaAnnotationsFieldNumber, protowire.BytesType)
	raw = protowire.AppendVarint(raw, 1000) // length exceeds remaining bytes
	opts.ProtoReflect().SetUnknown(protoreflect.RawFields(raw))
	if anns := readAnnotations(opts); len(anns) != 0 {
		t.Errorf("readAnnotations = %+v, want none", anns)
	}
	if anns := readAnnotations(&descriptorpb.MessageOptions{}); len(anns) != 0 {
		t.Errorf("readAnnotations on empty options = %+v, want none", anns)
	}
}

func TestParseFileFunctions(t *testing.T) {
	b := encodeFileFunctions(
		encodeFunctionDecl("fixtures.basic.is_email",
			[][]byte{encodeFunctionParam("value", "string")},
			[][]byte{
				encodeFunctionOption("description", encodeStringArg("", "checks RFC 5321 syntax")),
				encodeFunctionOption("error_code", encodeStringArg("", "email.invalid")),
			}),
		encodeFunctionDecl("fixtures.basic.in_range",
			[][]byte{
				encodeFunctionParam("value", "int64"),
				encodeFunctionParam("max", "int64"),
			},
			nil),
	)
	decls := parseFileFunctions(b)
	if len(decls) != 2 {
		t.Fatalf("parseFileFunctions returned %d decls, want 2: %+v", len(decls), decls)
	}
	d := decls[0]
	if d.Name != "fixtures.basic.is_email" {
		t.Errorf("decls[0].Name = %q", d.Name)
	}
	if len(d.Params) != 1 || d.Params[0] != (schemaFunctionParam{Name: "value", Type: "string"}) {
		t.Errorf("decls[0].Params = %+v", d.Params)
	}
	if opt := d.Options["description"]; opt.Kind != schemaArgString || opt.String != "checks RFC 5321 syntax" {
		t.Errorf(`Options["description"] = %+v`, opt)
	}
	if opt := d.Options["error_code"]; opt.String != "email.invalid" {
		t.Errorf(`Options["error_code"] = %+v`, opt)
	}
	if got := decls[1].Params; len(got) != 2 || got[1].Name != "max" || got[1].Type != "int64" {
		t.Errorf("decls[1].Params = %+v", got)
	}
}

func TestSchemaGoType(t *testing.T) {
	aliases := map[string]string{
		"fixtures.basic.Email":       "string",
		"fixtures.basic.CorpEmail":   "fixtures.basic.Email",
		"fixtures.basic.Cycle":       "fixtures.basic.Cycle",
		"fixtures.basic.OrderStatus": "fixtures.basic.OrderStatus2", // unresolvable tail
	}
	tests := []struct{ in, want string }{
		{"string", "string"},
		{"bytes", "[]byte"},
		{"bool", "bool"},
		{"int32", "int32"},
		{"sint64", "int64"},
		{"fixed32", "uint32"},
		{"uint64", "uint64"},
		{"float", "float32"},
		{"double", "float64"},
		{"fixtures.basic.Email", "string"},
		{"fixtures.basic.CorpEmail", "string"},
		{"fixtures.basic.Cycle", "any"},
		{"fixtures.basic.OrderStatus", "any"},
		{"myco.SomeMessage", "any"},
	}
	for _, tt := range tests {
		if got := schemaGoType(tt.in, aliases); got != tt.want {
			t.Errorf("schemaGoType(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAppendSchemaAnnotationComments(t *testing.T) {
	opts := &descriptorpb.FieldOptions{}
	raw := appendBytesField(nil, schemaAnnotationsFieldNumber, encodeAnnotationList(
		encodeAnnotation(schemaDescriptionAnnotation, encodeStringArg("", "primary contact address")),
		encodeAnnotation(schemaDeprecatedAnnotation, encodeStringArg("", "use contact_email")),
	))
	opts.ProtoReflect().SetUnknown(protoreflect.RawFields(raw))

	got := appendSchemaAnnotationComments(" existing comment\n", opts)
	want := protogen.Comments(" primary contact address\n\n existing comment\n\n Deprecated: use contact_email\n")
	if got != want {
		t.Errorf("appendSchemaAnnotationComments = %q, want %q", got, want)
	}

	// An existing deprecation notice (e.g. from the classic option) wins.
	got = appendSchemaAnnotationComments(" Deprecated: Marked as deprecated in a.proto.\n", opts)
	if want := "Deprecated: use contact_email"; strings.Contains(string(got), want) {
		t.Errorf("appendSchemaAnnotationComments duplicated deprecation notice: %q", got)
	}

	// No annotations: comments pass through untouched.
	if got := appendSchemaAnnotationComments(" untouched\n", &descriptorpb.FieldOptions{}); got != " untouched\n" {
		t.Errorf("appendSchemaAnnotationComments without carrier = %q", got)
	}
}
