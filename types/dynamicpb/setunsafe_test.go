// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dynamicpb_test

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	testpb "google.golang.org/protobuf/internal/testprotos/test"
)

// TestSetUnsafeRoundTripsThroughProto encodes a message via SetUnsafe,
// proto-marshals it, decodes it back through generated code, and checks
// every field. SetUnsafe skips the per-field type-check, so this test
// guards against regressions where it would silently store a value
// using the wrong constructor.
func TestSetUnsafeRoundTripsThroughProto(t *testing.T) {
	desc := (&testpb.TestAllTypes{}).ProtoReflect().Descriptor()
	m := dynamicpb.NewMessage(desc)

	type kv struct {
		name string
		val  protoreflect.Value
	}
	cases := []kv{
		{"optional_int32", protoreflect.ValueOfInt32(-7)},
		{"optional_int64", protoreflect.ValueOfInt64(9_000_000_001)},
		{"optional_uint32", protoreflect.ValueOfUint32(123)},
		{"optional_uint64", protoreflect.ValueOfUint64(1 << 40)},
		{"optional_float", protoreflect.ValueOfFloat32(2.5)},
		{"optional_double", protoreflect.ValueOfFloat64(0.85)},
		{"optional_bool", protoreflect.ValueOfBool(true)},
		{"optional_string", protoreflect.ValueOfString("hi")},
		{"optional_bytes", protoreflect.ValueOfBytes([]byte{0xde, 0xad})},
	}
	for _, c := range cases {
		fd := desc.Fields().ByName(protoreflect.Name(c.name))
		if fd == nil {
			t.Fatalf("test descriptor missing field %s", c.name)
		}
		m.SetUnsafe(fd, c.val)
	}

	wire, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("marshal after SetUnsafe: %v", err)
	}
	var got testpb.TestAllTypes
	if err := proto.Unmarshal(wire, &got); err != nil {
		t.Fatalf("unmarshal back to generated: %v", err)
	}

	if got.GetOptionalInt32() != -7 ||
		got.GetOptionalInt64() != 9_000_000_001 ||
		got.GetOptionalUint32() != 123 ||
		got.GetOptionalUint64() != 1<<40 ||
		got.GetOptionalFloat() != 2.5 ||
		got.GetOptionalDouble() != 0.85 ||
		!got.GetOptionalBool() ||
		got.GetOptionalString() != "hi" ||
		string(got.GetOptionalBytes()) != "\xde\xad" {
		t.Fatalf("round-trip mismatch: %+v", &got)
	}
}

// TestSetUnsafeMatchesSet confirms that for a correctly-typed Value,
// SetUnsafe produces a message indistinguishable from Set on the wire.
func TestSetUnsafeMatchesSet(t *testing.T) {
	desc := (&testpb.TestAllTypes{}).ProtoReflect().Descriptor()
	fd := desc.Fields().ByName("optional_int64")

	a := dynamicpb.NewMessage(desc)
	a.Set(fd, protoreflect.ValueOfInt64(42))
	wireA, err := proto.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}

	b := dynamicpb.NewMessage(desc)
	b.SetUnsafe(fd, protoreflect.ValueOfInt64(42))
	wireB, err := proto.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}

	if string(wireA) != string(wireB) {
		t.Fatalf("Set vs SetUnsafe wire diverged:\n  Set:       %x\n  SetUnsafe: %x", wireA, wireB)
	}
}

// TestSetUnsafeOneofClearsOthers ensures the oneof-fixup is still run
// (only the typecheck is skipped).
func TestSetUnsafeOneofClearsOthers(t *testing.T) {
	desc := (&testpb.TestAllTypes{}).ProtoReflect().Descriptor()
	first := desc.Fields().ByName("oneof_uint32")
	second := desc.Fields().ByName("oneof_string")
	if first == nil || second == nil {
		t.Skip("test descriptor missing oneof fields")
	}

	m := dynamicpb.NewMessage(desc)
	m.SetUnsafe(first, protoreflect.ValueOfUint32(7))
	m.SetUnsafe(second, protoreflect.ValueOfString("later"))

	if m.Has(first) {
		t.Fatalf("expected first oneof member to be cleared after second SetUnsafe")
	}
	if !m.Has(second) {
		t.Fatalf("expected second oneof member to be set")
	}
}
