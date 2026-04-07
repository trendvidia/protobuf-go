# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is `google.golang.org/protobuf` — the Go implementation of Protocol Buffers (the "v2" API). It provides runtime libraries for serialization and a protoc plugin (`protoc-gen-go`) for code generation.

## Build & Test Commands

**Run all integration tests** (downloads Go toolchains, protobuf compiler, runs staticcheck — takes a long time):
```bash
./test.bash
```

**Run individual package tests:**
```bash
go test ./proto/...
go test ./encoding/protojson/...
go test ./reflect/protoregistry/...
```

**Run a single test:**
```bash
go test ./proto/... -run TestMarshal
```

**Regenerate all generated code** (requires network access for downloading tools):
```bash
./regenerate.bash
```

## Architecture

The codebase separates into four layers:

1. **Public API** (`proto/`, `encoding/`, `reflect/`, `types/`) — user-facing packages for marshaling, unmarshaling, reflection, and well-known types.
2. **Code generation** (`cmd/protoc-gen-go/`, `compiler/protogen/`) — the protoc plugin and its framework. `protogen` parses protoc requests and provides a generation API; `internal_gengo` contains the actual Go code emitters.
3. **Runtime implementation** (`internal/impl/`) — the core message implementation that backs generated code. Handles encoding, decoding, lazy fields, extensions, and message sets.
4. **Reflection system** (`reflect/protoreflect/`, `reflect/protodesc/`, `reflect/protoregistry/`) — interfaces and registries for introspecting message types at runtime.

### Key relationships

- Generated messages implement `proto.Message` (single method: `ProtoReflect() protoreflect.Message`)
- All high-level operations (Marshal, Unmarshal, Merge, Equal, Clone) go through the reflection interface
- `internal/impl` provides the fast-path implementations that generated code wires up
- `encoding/protowire` handles raw wire format; `encoding/protojson` and `encoding/prototext` handle text formats
- `types/dynamicpb` creates messages from descriptors at runtime (no code generation)
- `protoadapt/` bridges between v1 (`github.com/golang/protobuf`) and v2 APIs

### Integration test

`integration_test.go` in the repo root is the primary CI test. It downloads specific Go toolchains and the protobuf compiler, runs staticcheck, builds `protoc-gen-go`, regenerates code, and runs all package tests across multiple Go versions. It is triggered via `./test.bash` and is what CI runs.

## Module

- **Module path:** `google.golang.org/protobuf`
- **Minimum Go version:** 1.23
- **Upstream source of truth:** `go.googlesource.com/protobuf` (Gerrit, not GitHub PRs)
