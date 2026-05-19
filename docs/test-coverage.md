# Test Coverage — digital.vasic.agentic (round-260)

> Verbatim 2026-05-19 operator mandate: *"all existing tests and Challenges do work in anti-bluff manner - they MUST confirm that all tested codebase really works as expected! We had been in position that all tests do execute with success and all Challenges as well, but in reality the most of the features does not work and can't be used! This MUST NOT be the case and execution of tests and Challenges MUST guarantee the quality, the completition and full usability by end users of the product!"*

CONST-050(B) symbol-to-test ledger. Every exported symbol in the
`agentic` package is cross-referenced to the unit-test name(s) in
`agentic/workflow_test.go` that exercise it AND to the round-260
Challenge runner section that exercises it against real workflow
execution (real graph traversal, real conditional branching, real
retry-with-backoff timing, real checkpoint/restore round-trip,
real nil-handler sentinel verification). No metadata-only PASS —
every entry below names the production code path and the runtime
evidence channel that proves it works.

## Anti-bluff posture (round-260)

- **Multi-locale linear DAG round-trip.** `challenges/runner/main.go`
  Section 1 builds a real `Workflow`, registers 3 nodes per locale
  (5 locales: en, sr, ja, ar, zh-CN), executes the graph, and
  asserts `state.History` length is exactly 3, every `NodeName` is
  byte-exact to the fixture label, and the final result string
  survives non-ASCII bytes verbatim. The PASS line carries the
  locale code AND `utf8.RuneCountInString` so byte preservation is
  observable, not assumed.
- **Real conditional branching.** Section 2 constructs a
  Classify -> {CodeGen | QA} fan-out with `ConditionFunc`-gated
  edges and asserts only the truthy branch executes — the rejected
  branch's `NodeID` MUST NOT appear in `state.History`.
- **Real ShouldEnd short-circuit.** Section 3 builds a 4-node chain
  where node 2 sets `NodeOutput.ShouldEnd = true` and asserts
  `state.History` length is exactly 2 AND nodes 3+4's poison flags
  never appear in `state.Variables` — proves the short-circuit
  truly skips downstream nodes rather than executing them silently.
- **Real exponential-backoff timing.** Section 4 registers a node
  whose handler errors on attempts 1-3 and succeeds on attempt 4,
  with `RetryPolicy{Delay: 20ms, Backoff: 2.0, MaxRetries: 3}`, and
  asserts the wall-clock elapsed is >= sum(20ms + 40ms + 80ms) =
  140ms — proves backoff fires for real, not as a no-op.
- **Real checkpoint + restore round-trip.** Section 5 runs a
  10-node workflow with `CheckpointInterval = 2`, captures the
  produced `state.Checkpoints`, poisons `state.CurrentNode` and a
  variable, restores from a middle checkpoint, and asserts the
  `CurrentNode` is reset to `cp.NodeID`, the variables byte-exact
  equal the snapshot, the status is reset to `Running`, AND an
  unknown checkpoint ID returns a real error.
- **Real nil-handler sentinel.** Section 6 registers a node with
  a nil `Handler`, executes the workflow, and asserts the returned
  error wraps `ErrNodeHandlerNotConfigured` via `errors.Is(...)`.
  This is the round-23 §11.4 audit fix: the previous silent-empty-
  success path was a PASS-bluff at the workflow-executor layer and
  is closed for good.
- **Paired mutation.** Running the gate with `--anti-bluff-mutate`
  plants a deliberate `ErrNodeHandlerNotConfigured ->
  ErrBogus_MUTATED` rename in a tmp copy of this ledger, reruns
  the cross-reference check, and asserts the gate exits 99. Proves
  the symbol-to-test ledger actually catches drift instead of
  rubber-stamping it.

## agentic — exported types

| Exported symbol | Unit-test coverage (`agentic/workflow_test.go`) | Runner section |
|-----------------|-------------------------------------------------|----------------|
| `type Workflow` | every `TestWorkflow_*`, `TestExecute_*` | Sections 1, 2, 3, 4, 5, 6 (every section constructs one) |
| `type WorkflowGraph` | constructor coverage via `NewWorkflow` | Sections 1-6 (read indirectly through `Workflow.Graph`) |
| `type Node` | every `TestNode_*`, `TestAddNode_*` | Sections 1-6 (every section registers nodes) |
| `type Edge` | `TestAddEdge_*` | Sections 1, 2, 3, 5 (edges drive the graph) |
| `type NodeInput` | `TestNodeInput_*` | Sections 1, 2 (Query carries the prompt) |
| `type NodeOutput` | `TestNodeOutput_*`, `TestExecute_ShouldEnd` | Sections 1, 2, 3 (Result / ShouldEnd / NextNode) |
| `type WorkflowState` | every `TestWorkflowState_*` | Sections 1-6 (History, Variables, Checkpoints inspected directly) |
| `type Message` | covered transitively via `NodeInput.Messages` | (not exercised in round-260 runner — covered in `helix_llm` consumer) |
| `type Tool` | `TestTool_*` | (not exercised in round-260 runner) |
| `type ToolCall` | `TestToolCall_*` | (not exercised in round-260 runner) |
| `type NodeExecution` | `TestExecute_HistoryRecorded` | Sections 1, 3, 6 (asserted explicitly via `state.History[i]`) |
| `type Checkpoint` | `TestCheckpoint_*` | Section 5 (counted, then used as restore target) |
| `type WorkflowConfig` | `TestDefaultWorkflowConfig` | Sections 1-6 (constructed per section) |
| `type RetryPolicy` | `TestRetryPolicy_*` | Section 4 (Delay + Backoff timing asserted) |
| `type NodeType` (string-typed enum) | every test that names a NodeType constant | Sections 1-5 (NodeTypeAgent/Tool/Condition referenced explicitly) |
| `type WorkflowStatus` (string-typed enum) | every test that asserts a Status value | Sections 1, 4 (StatusCompleted), 5 (StatusRunning), 6 (StatusFailed) |

## agentic — exported functions / methods

| Exported symbol | Unit-test coverage | Runner section |
|-----------------|--------------------|----------------|
| `NewWorkflow(name, description, config, logger) *Workflow` | every `TestNewWorkflow_*` | Sections 1-6 (constructor used in every section) |
| `DefaultWorkflowConfig() *WorkflowConfig` | `TestDefaultWorkflowConfig` | Sections 1-6 (cloned + mutated per section) |
| `(*Workflow).AddNode(node *Node) error` | `TestAddNode_*` | Sections 1-6 |
| `(*Workflow).AddEdge(from, to string, cond ConditionFunc, label string) error` | `TestAddEdge_*` | Sections 1, 2, 3, 5 |
| `(*Workflow).SetEntryPoint(nodeID string) error` | `TestSetEntryPoint_*` | Sections 1-6 |
| `(*Workflow).AddEndNode(nodeID string) error` | `TestAddEndNode_*` | Sections 1-6 |
| `(*Workflow).Execute(ctx, input) (*WorkflowState, error)` | every `TestExecute_*` | Sections 1, 2, 3, 4, 5, 6 (the central path) |
| `(*Workflow).RestoreFromCheckpoint(state, checkpointID) error` | `TestRestoreFromCheckpoint_*` | Section 5 (both happy + bogus-id paths) |

## agentic — exported sentinels + function types

| Exported symbol | Unit-test coverage | Runner section |
|-----------------|--------------------|----------------|
| `var ErrNodeHandlerNotConfigured` | `TestExecute_NilHandler_*` | Section 6 (`errors.Is` against the real exported var) |
| `type NodeHandler` | every test using a handler | Sections 1-6 (every handler is a `NodeHandler`) |
| `type ConditionFunc` | `TestEdgeCondition_*` | Section 2 (both truthy + falsy ConditionFunc exercised) |
| `type ToolHandler` | `TestTool_*` | (not exercised in round-260 runner — consumer-side) |

## agentic — exported enum values

| Exported symbol | Runner section |
|-----------------|----------------|
| `NodeTypeAgent`, `NodeTypeTool`, `NodeTypeCondition`, `NodeTypeParallel`, `NodeTypeHuman`, `NodeTypeSubgraph` | Sections 1, 2, 3, 4, 5 (every node-type constant referenced by section nodes) |
| `StatusPending`, `StatusRunning`, `StatusPaused`, `StatusCompleted`, `StatusFailed` | Sections 1 (`StatusCompleted`), 4 (`StatusCompleted`), 5 (`StatusRunning` post-restore), 6 (`StatusFailed`) |

## Round-260 artefacts inventory

| Artefact | Path | Purpose |
|----------|------|---------|
| Runner | `challenges/runner/main.go` | Real Agentic exerciser (6 sections, 5 locales) |
| Mutation gate | `challenges/scripts/agentic_describe_challenge.sh` | Cross-reference + paired-mutation enforcement |
| Multi-locale fixture | `tests/fixtures/agentic/payloads.json` | 5 locales: en, sr, ja, ar, zh-CN |
| README guarantees | `README.md` | Anti-bluff section + quick start |
| Ledger | `docs/test-coverage.md` (this file) | Symbol → test cross-reference |

## Inherited governance challenges (still in scope)

| Script | Purpose |
|--------|---------|
| `challenges/scripts/agentic_workflow_challenge.sh` | Legacy module presence smoke |
| `challenges/scripts/no_suspend_calls_challenge.sh` | CONST-033 host-power scan |
| `challenges/scripts/host_no_auto_suspend_challenge.sh` | CONST-033 host hardening |
| `challenges/scripts/chaos_failure_injection_challenge.sh` | CONST-050(B) chaos type |
| `challenges/scripts/ddos_health_flood_challenge.sh` | CONST-050(B) DDoS type |
| `challenges/scripts/scaling_horizontal_challenge.sh` | CONST-050(B) scaling type |
| `challenges/scripts/stress_sustained_load_challenge.sh` | CONST-050(B) stress type |
| `challenges/scripts/ui_terminal_interaction_challenge.sh` | CONST-050(B) UI type |
| `challenges/scripts/ux_end_to_end_flow_challenge.sh` | CONST-050(B) UX type |
