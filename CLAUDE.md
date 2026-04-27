# CLAUDE.md - Agentic Module


## Definition of Done

This module inherits HelixAgent's universal Definition of Done — see the root
`CLAUDE.md` and `docs/development/definition-of-done.md`. In one line: **no
task is done without pasted output from a real run of the real system in the
same session as the change.** Coverage and green suites are not evidence.

### Acceptance demo for this module

```bash
# DAG workflow Plan → Execute → Review with checkpoint creation
cd Agentic && GOMAXPROCS=2 nice -n 19 go test -count=1 -race -v \
  -run 'TestFullAgentWorkflowPipeline_E2E|TestWorkflowCheckpointCreation_E2E' \
  ./tests/e2e/...
```
Expect: two E2E PASS showing ≥3 node executions + 1 checkpoint.


## Overview

`digital.vasic.agentic` is the graph-based workflow orchestration backbone for autonomous agents in HelixAgent. A workflow is a DAG of typed nodes (agent, tool, condition, parallel, human-in-the-loop, subgraph); the executor threads a mutable state through them, supports conditional routing, exponential-backoff retries, checkpoints/restore, and self-correction loops. It is the execution substrate that Planning, LLMOps, SelfImprove, and Benchmark build on.

**Module:** `digital.vasic.agentic` (Go 1.24+, ~2,600 LOC across 7 files).

## Architecture

```
Workflow ──────────► WorkflowGraph (nodes + edges + ConditionFunc routing)
    │
    ├── WorkflowState (messages, variables, history, checkpoints; RWMutex)
    ├── Execution loop
    │     • MaxIterations / Timeout guards
    │     • Retries: exponential backoff (custom, not math.Pow)
    │     • Checkpoint every CheckpointInterval iterations
    │     • ShouldEnd flag in NodeOutput wins over routing
    └── Optional self-correction hooks per node
```

Graph topology is not precomputed — the condition functions evaluate every iteration. Graph validation happens at execution time, not at `AddNode`/`AddEdge` time; an invalid graph produces a runtime error.

## Key types and interfaces

```go
type NodeHandler func(ctx context.Context, state *WorkflowState, input *NodeInput) (*NodeOutput, error)
type ConditionFunc func(state *WorkflowState) bool

type WorkflowConfig struct {
    MaxIterations     int
    CheckpointInterval int
    Timeout           time.Duration
    EnableCheckpoints bool
    EnableSelfCorrection bool
    MaxRetries        int
    RetryDelay        time.Duration
}

func NewWorkflow(name string, graph *WorkflowGraph, cfg *WorkflowConfig) *Workflow
func (w *Workflow) Execute(ctx context.Context, input *NodeInput) (*WorkflowState, error)
func (w *Workflow) RestoreFromCheckpoint(state *WorkflowState, checkpointID string) error
```

Node types supported: `agent`, `tool`, `condition`, `parallel`, `human`, `subgraph`.

## Integration Seams

- **Upstream (imports):** none.
- **Downstream (consumed by):** HelixLLM (primary consumer for agentic request handling).
- **Sibling complements:** `Planning` (provides the plans that become node sequences), `LLMOps` (evaluates workflow outputs), `SelfImprove` (tunes prompts used inside agent nodes), `ToolSchema` (supplies tool handlers consumed by tool nodes).

## Gotchas

1. **Checkpoints copy variables only** — not messages or history. A restore doesn't reconstruct the full conversation; design your workflow so variables carry the load-bearing state.
2. **`ShouldEnd` in `NodeOutput` overrides routing** — a node can short-circuit the graph regardless of edges. Useful for early termination, surprising if you don't know it.
3. **State mutation post-unlock is a race** — callers receiving values from `WorkflowState` must not mutate them after releasing the lock. The RWMutex protects fields, not the values they point to.
4. **Retry delay uses a custom exponential formula** — not `math.Pow`. If you're tuning, read the code rather than assuming standard behavior.
5. **No topology validation at graph-build time** — a cycle or dangling edge is discovered at execution. Consider writing a `Validate()` helper for your specific graph shape before shipping.

## Acceptance demo

```bash
# End-to-end workflow: Plan → Execute → Review, runs against real dependencies
GOMAXPROCS=2 nice -n 19 go test -race -v \
  -run TestFullAgentWorkflowPipeline_E2E ./tests/e2e/... -count=1

# Expected tail:
#     PASS: TestFullAgentWorkflowPipeline_E2E — 3 nodes executed, 1 checkpoint created
#     ok  	digital.vasic.agentic/tests/e2e	<duration>

# Stress harness (concurrent checkpoints, 100 iterations)
GOMAXPROCS=2 nice -n 19 go test -race -v ./tests/stress/... -count=1
```

A real demo from a consumer (HelixLLM) belongs alongside the consumer's tests — add a reference here once it exists.
