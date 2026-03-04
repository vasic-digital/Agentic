# Agentic Module Architecture

**Module:** `digital.vasic.agentic`

## Overview

Graph-based workflow orchestration for autonomous AI agents with planning, execution, and self-correction capabilities.

## Core Types

- **Workflow** — Top-level orchestrator with ID, graph, state, and config
- **WorkflowGraph** — DAG of Nodes connected by Edges
- **Node** — Processing unit (LLM call, tool use, decision point)
- **Edge** — Directed connection with optional conditions
- **WorkflowState** — Tracks execution state, variables, and history
- **WorkflowConfig** — Max retries, timeouts, concurrency settings

## Execution Flow

```
Define Graph → Validate → Execute Nodes (topological order)
                          ↓
                    For each Node:
                      1. Check preconditions
                      2. Execute handler
                      3. Evaluate output conditions
                      4. Route to next node(s)
                      5. Handle errors / retry
```

## Key Features

- **Conditional branching** — Edge conditions determine next node
- **Parallel execution** — Independent nodes run concurrently
- **State management** — Variables passed between nodes
- **Error recovery** — Configurable retry with backoff
- **Execution history** — Full audit trail of node executions

## Package Structure

```
agentic/
└── workflow.go   # All types and execution logic
```

## Integration

Used by HelixAgent's debate orchestrator for multi-step agent workflows.
Adapter: `internal/adapters/agentic/adapter.go`
