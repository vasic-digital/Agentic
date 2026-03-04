# CLAUDE.md - Agentic Module

## Overview

`digital.vasic.agentic` is a generic, reusable Go module for graph-based workflow orchestration for autonomous AI agents with planning, execution, and self-correction capabilities.

**Module**: `digital.vasic.agentic` (Go 1.24+)

## Build & Test

```bash
go build ./...
go test ./... -count=1 -race
go test ./... -short
```

## Package Structure

| Package | Purpose |
|---------|---------|
| `agentic` | Core workflow types: Workflow, Node, Edge, WorkflowState |

## Key Types

- `Workflow` — Graph-based agentic workflow
- `WorkflowGraph` — Defines workflow structure (nodes + edges)
- `Node` — Single step in the workflow with a handler function
- `WorkflowState` — Mutable state threaded through all nodes
- `NodeHandler` — `func(ctx, state, input) (*NodeOutput, error)`
