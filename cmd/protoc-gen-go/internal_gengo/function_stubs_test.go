// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal_gengo

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// generate runs the full generator over a single FileDescriptorProto and
// returns the generated .pb.go source.
func generate(t *testing.T, fdp *descriptorpb.FileDescriptorProto) string {
	t.Helper()
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{fdp.GetName()},
		ProtoFile:      []*descriptorpb.FileDescriptorProto{fdp},
	}
	gen, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatalf("protogen.Options.New: %v", err)
	}
	file, ok := gen.FilesByPath[fdp.GetName()]
	if !ok {
		t.Fatalf("file %q missing from generation request", fdp.GetName())
	}
	g := GenerateFile(gen, file)
	content, err := g.Content()
	if err != nil {
		t.Fatalf("generated file does not compile to valid Go: %v", err)
	}
	return string(content)
}

// basicFileDescriptor mirrors protowire's
// testdata/schema-extensions/01_basic.proto after lowering: the v1.2
// constructs are carried as extensions 1327/1328/1330 in the options,
// exactly as the protocompile lowering pass embeds them.
func basicFileDescriptor(t *testing.T) *descriptorpb.FileDescriptorProto {
	t.Helper()

	fileOpts := &descriptorpb.FileOptions{GoPackage: proto.String("example.com/fixtures/basicpb")}
	fileCarrier := appendBytesField(nil, schemaFunctionsFieldNumber, encodeFileFunctions(
		encodeFunctionDecl("fixtures.basic.is_email",
			[][]byte{encodeFunctionParam("value", "string")},
			[][]byte{
				encodeFunctionOption("description", encodeStringArg("", "reports whether value is an RFC 5321 address")),
			}),
		encodeFunctionDecl("fixtures.basic.is_valid",
			[][]byte{
				encodeFunctionParam("addr", "fixtures.basic.Email"),
				encodeFunctionParam("strict", "bool"),
			},
			[][]byte{
				encodeFunctionOption("deprecated", encodeStringArg("", "use is_email")),
			}),
	))
	fileCarrier = appendBytesField(fileCarrier, schemaTypeDeclsFieldNumber, encodeFileTypeDecls(
		encodeTypeDecl("fixtures.basic.Email", "string"),
	))
	fileOpts.ProtoReflect().SetUnknown(protoreflect.RawFields(fileCarrier))

	msgOpts := &descriptorpb.MessageOptions{}
	msgOpts.ProtoReflect().SetUnknown(protoreflect.RawFields(
		appendBytesField(nil, schemaAnnotationsFieldNumber, encodeAnnotationList(
			encodeAnnotation(schemaDescriptionAnnotation, encodeStringArg("", "a registered user")),
		))))

	fieldOpts := &descriptorpb.FieldOptions{}
	fieldOpts.ProtoReflect().SetUnknown(protoreflect.RawFields(
		appendBytesField(nil, schemaAnnotationsFieldNumber, encodeAnnotationList(
			encodeAnnotation(schemaDescriptionAnnotation, encodeStringArg("", "primary contact address")),
			encodeAnnotation(schemaDeprecatedAnnotation, encodeStringArg("", "use contact_email")),
		))))

	enumOpts := &descriptorpb.EnumOptions{}
	enumOpts.ProtoReflect().SetUnknown(protoreflect.RawFields(
		appendBytesField(nil, schemaAnnotationsFieldNumber, encodeAnnotationList(
			encodeAnnotation(schemaDescriptionAnnotation, encodeStringArg("", "account lifecycle state")),
		))))

	enumValueOpts := &descriptorpb.EnumValueOptions{}
	enumValueOpts.ProtoReflect().SetUnknown(protoreflect.RawFields(
		appendBytesField(nil, schemaAnnotationsFieldNumber, encodeAnnotationList(
			encodeAnnotation(schemaDeprecatedAnnotation),
		))))

	return &descriptorpb.FileDescriptorProto{
		Name:    proto.String("fixtures/basic.proto"),
		Package: proto.String("fixtures.basic"),
		Syntax:  proto.String("proto3"),
		Options: fileOpts,
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name:    proto.String("Status"),
			Options: enumOpts,
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: proto.String("STATUS_UNSPECIFIED"), Number: proto.Int32(0)},
				{Name: proto.String("STATUS_LEGACY"), Number: proto.Int32(1), Options: enumValueOpts},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name:    proto.String("User"),
			Options: msgOpts,
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:     proto.String("email"),
				Number:   proto.Int32(1),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				JsonName: proto.String("email"),
				Options:  fieldOpts,
			}},
		}},
	}
}

func TestGenFunctionStubs(t *testing.T) {
	content := generate(t, basicFileDescriptor(t))

	for _, want := range []string{
		// Functions interface with typed signatures; the type alias
		// fixtures.basic.Email resolves through FileTypeDecls to string.
		"type Functions interface {",
		"IsEmail(value string) (bool, *v2.Violation)",
		"IsValid(addr string, strict bool) (bool, *v2.Violation)",
		// Bracket-form options surface as doc comments.
		"// reports whether value is an RFC 5321 address",
		"// Deprecated: use is_email",
		// UnimplementedFunctions placeholder fails with the reserved
		// RFC-001 §7 code and spec-pinned fallback message, both referenced
		// through the runtime's typed constants and template helpers.
		"type UnimplementedFunctions struct{}",
		`{Code: v2.CodeFunctionUnimplemented, FallbackMessage: v2.MsgFunctionUnimplemented("fixtures.basic.is_email")}`,
		"var _ Functions = UnimplementedFunctions{}",
		// Registration helper with []any adapters.
		"func RegisterFunctions(eng v2.Engine, impl Functions) error {",
		`if err := eng.Register("fixtures.basic.is_email", func(args []any) (bool, *v2.Violation) {`,
		`{Code: v2.CodeFunctionInvalidArgument, FallbackMessage: v2.MsgFunctionArity("fixtures.basic.is_email", 1)}`,
		"a0, ok := args[0].(string)",
		// The arg-type guard message names the schema-declared parameter
		// type (the alias FQN), not the resolved Go type it asserts.
		`{Code: v2.CodeFunctionInvalidArgument, FallbackMessage: v2.MsgFunctionArgType("fixtures.basic.is_valid", 0, "fixtures.basic.Email")}`,
		"return impl.IsValid(a0, a1)",
		// Engine SPI import.
		`v2 "github.com/trendvidia/protocheck/v2"`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("generated output missing %q", want)
		}
	}

	// The generated file must be syntactically valid Go.
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "basic.pb.go", content, parser.SkipObjectResolution); err != nil {
		t.Errorf("generated output does not parse: %v", err)
	}

	if t.Failed() {
		t.Logf("generated output:\n%s", content)
	}
}

func TestAnnotationDocComments(t *testing.T) {
	content := generate(t, basicFileDescriptor(t))

	for _, want := range []string{
		// @description on message, field, enum.
		"// a registered user\ntype User struct {",
		"// primary contact address",
		"// account lifecycle state\ntype Status int32",
		// @deprecated on the email field and the STATUS_LEGACY value.
		"// Deprecated: use contact_email",
		"// Deprecated: Do not use.\n\tStatus_STATUS_LEGACY Status = 1",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("generated output missing %q", want)
		}
	}
	if t.Failed() {
		t.Logf("generated output:\n%s", content)
	}
}

// TestNoSchemaExtensionsNoOverhead verifies that a file without carrier
// options generates no stubs and no engine dependency.
func TestNoSchemaExtensionsNoOverhead(t *testing.T) {
	fdp := basicFileDescriptor(t)
	fdp.Options = &descriptorpb.FileOptions{GoPackage: proto.String("example.com/fixtures/basicpb")}
	fdp.MessageType[0].Options = nil
	fdp.MessageType[0].Field[0].Options = nil
	fdp.EnumType[0].Options = nil
	fdp.EnumType[0].Value[1].Options = nil

	content := generate(t, fdp)
	// Note: "Deprecated: Use xxx.Descriptor instead." legacy accessors are
	// always generated, so ban only the annotation-derived notices.
	for _, banned := range []string{"Functions", "protocheck", "Deprecated: use", "Deprecated: Do not use."} {
		if strings.Contains(content, banned) {
			t.Errorf("output for carrier-free file unexpectedly contains %q", banned)
		}
	}
}

// functionFile returns a file in package example.com/fixtures/sharedpb. It
// declares the named function, or no function when fn is empty.
func functionFile(name, fn string, deps ...string) *descriptorpb.FileDescriptorProto {
	opts := &descriptorpb.FileOptions{GoPackage: proto.String("example.com/fixtures/sharedpb")}
	if fn != "" {
		opts.ProtoReflect().SetUnknown(protoreflect.RawFields(appendBytesField(nil, schemaFunctionsFieldNumber,
			encodeFileFunctions(encodeFunctionDecl("fixtures.shared."+fn, nil, nil)))))
	}
	return &descriptorpb.FileDescriptorProto{
		Name:       proto.String(name),
		Package:    proto.String("fixtures.shared"),
		Syntax:     proto.String("proto3"),
		Dependency: deps,
		Options:    opts,
	}
}

func TestFunctionStubConflict(t *testing.T) {
	const a, b = "fixtures/a.proto", "fixtures/b.proto"
	for _, tt := range []struct {
		name      string
		parameter string
		files     []*descriptorpb.FileDescriptorProto
		generate  []string
		wantErr   []string // substrings; empty means no error
	}{{
		name:     "both files in the run declare functions",
		files:    []*descriptorpb.FileDescriptorProto{functionFile(a, "first"), functionFile(b, "second")},
		generate: []string{a, b},
		wantErr:  []string{a, b, "example.com/fixtures/sharedpb", "RegisterFunctions"},
	}, {
		name:     "imported sibling generated by an earlier run",
		files:    []*descriptorpb.FileDescriptorProto{functionFile(a, "first"), functionFile(b, "second", a)},
		generate: []string{b},
		wantErr:  []string{a, b},
	}, {
		name:     "sibling without functions",
		files:    []*descriptorpb.FileDescriptorProto{functionFile(a, "first"), functionFile(b, "")},
		generate: []string{a, b},
	}, {
		name:      "same import path in different output directories",
		parameter: "paths=source_relative",
		files: []*descriptorpb.FileDescriptorProto{
			functionFile("one/a.proto", "first"),
			functionFile("two/b.proto", "second"),
		},
		generate: []string{"one/a.proto", "two/b.proto"},
	}, {
		name:      "same directory with source-relative paths",
		parameter: "paths=source_relative",
		files:     []*descriptorpb.FileDescriptorProto{functionFile(a, "first"), functionFile(b, "second")},
		generate:  []string{a, b},
		wantErr:   []string{a, b},
	}} {
		t.Run(tt.name, func(t *testing.T) {
			gen, err := protogen.Options{}.New(&pluginpb.CodeGeneratorRequest{
				Parameter:      proto.String(tt.parameter),
				FileToGenerate: tt.generate,
				ProtoFile:      tt.files,
			})
			if err != nil {
				t.Fatalf("protogen.Options.New: %v", err)
			}
			for _, f := range gen.Files {
				if f.Generate {
					GenerateFile(gen, f)
				}
			}
			got := gen.Response().GetError()
			if len(tt.wantErr) == 0 && got != "" {
				t.Errorf("unexpected plugin error: %s", got)
			}
			if len(tt.wantErr) > 0 && got == "" {
				t.Error("no plugin error")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(got, want) {
					t.Errorf("plugin error missing %q: %s", want, got)
				}
			}
		})
	}
}
