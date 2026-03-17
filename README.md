# Agentic

`digital.vasic.agentic` -- Graph-based agentic workflow orchestration with multi-step execution, conditional branching, state management, checkpointing, and self-correction.

## Overview

Agentic is a Go module that provides a framework for building autonomous AI agent workflows modelled as directed graphs. Each node in the graph represents an execution step (LLM agent call, tool invocation, conditional branch, parallel fan-out, human approval gate, or a nested subgraph), and edges define the flow between steps with optional conditions.

The workflow engine supports automatic retries with configurable backoff, periodic checkpointing for resumption after failures, timeout enforcement at both the workflow and node level, and a maximum iteration guard to prevent infinite loops. State is threaded through all nodes via a shared `WorkflowState` object that carries messages, variables, and a full execution history.

Agentic is designed as a generic, reusable module with no dependencies on HelixAgent internals. It can be used independently in any Go project that needs structured multi-step AI agent orchestration.

## Architecture

```
+------------------+       +------------------+       +------------------+
|   Node: Agent    | ----> |  Node: Tool      | ----> |  Node: Condition |
|  (LLM call)      |       |  (code exec)     |       |  (branch logic)  |
+------------------+       +------------------+       +--------+---------+
                                                               |
                                                  +------------+------------+
                                                  |                         |
                                          +-------v------+         +-------v------+
                                          | Node: Agent  |         | Node: Human  |
                                          | (refinement) |         | (approval)   |
                                          +--------------+         +--------------+

WorkflowState flows through every node:
  - Messages []Message       (conversation history)
  - Variables map[string]any (shared state)
  - History []NodeExecution  (audit trail)
  - Checkpoints []Checkpoint (resumption points)
```

## Package Structure

| Package | Purpose |
|---------|---------|
| `agentic` | Core module: workflow graph, node types, execution engine, state management |

### Source Files

| File | Description |
|------|-------------|
| `workflow.go` | Complete module: all types, workflow builder, execution loop, checkpointing, retry logic |

## API Reference

### Types

**Node types**: `NodeTypeAgent` (LLM-based), `NodeTypeTool` (tool execution), `NodeTypeCondition` (conditional branching), `NodeTypeParallel` (parallel execution), `NodeTypeHuman` (human-in-the-loop), `NodeTypeSubgraph` (nested workflow)

**Workflow statuses**: `StatusPending`, `StatusRunning`, `StatusPaused`, `StatusCompleted`, `StatusFailed`

### Core Types

```go
// Workflow represents a graph-based agentic workflow
type Workflow struct {
    ID          string
    Name        string
    Description string
    Graph       *WorkflowGraph
    State       *WorkflowState
    Config      *WorkflowConfig
}

// Node represents a node in the workflow graph
type Node struct {
    ID          string
    Name        string
    Type        NodeType
    Handler     NodeHandler
    Condition   ConditionFunc
    Config      map[string]interface{}
    RetryPolicy *RetryPolicy
}

// WorkflowState maintains state across the workflow
type WorkflowState struct {
    ID          string
    CurrentNode string
    Messages    []Message
    Variables   map[string]interface{}
    History     []NodeExecution
    Checkpoints []Checkpoint
    Status      WorkflowStatus
}

// Handler function signatures
type NodeHandler func(ctx context.Context, state *WorkflowState, input *NodeInput) (*NodeOutput, error)
type ConditionFunc func(state *WorkflowState) bool
type ToolHandler func(ctx context.Context, args map[string]interface{}) (interface{}, error)
```

### Workflow Builder Methods

```go
func NewWorkflow(name, description string, config *WorkflowConfig, logger *logrus.Logger) *Workflow
func (w *Workflow) AddNode(node *Node) error
func (w *Workflow) AddEdge(from, to string, condition ConditionFunc, label string) error
func (w *Workflow) SetEntryPoint(nodeID string) error
func (w *Workflow) AddEndNode(nodeID string) error
func (w *Workflow) Execute(ctx context.Context, input *NodeInput) (*WorkflowState, error)
func (w *Workflow) RestoreFromCheckpoint(state *WorkflowState, checkpointID string) error
```

### Input/Output Types

```go
// NodeInput contains input for a node
type NodeInput struct {
    Query    string
    Messages []Message
    Tools    []Tool
    Context  map[string]interface{}
    Previous *NodeOutput
}

// NodeOutput contains output from a node
type NodeOutput struct {
    Result    interface{}
    Messages  []Message
    ToolCalls []ToolCall
    NextNode  string  // Override next node routing
    ShouldEnd bool    // Signal workflow completion
    Error     error
    Metadata  map[string]interface{}
}
```

## Usage Examples

### Basic linear workflow

```go
wf := agentic.NewWorkflow("code-review", "AI code review pipeline", nil, logger)

// Add nodes
wf.AddNode(&agentic.Node{
    ID:   "analyze",
    Name: "Code Analysis",
    Type: agentic.NodeTypeAgent,
    Handler: func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
        // Call LLM to analyze code
        result := analyzeCode(ctx, input.Query)
        state.Variables["analysis"] = result
        return &agentic.NodeOutput{Result: result}, nil
    },
})

wf.AddNode(&agentic.Node{
    ID:   "suggest",
    Name: "Generate Suggestions",
    Type: agentic.NodeTypeAgent,
    Handler: func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
        analysis := state.Variables["analysis"].(string)
        suggestions := generateSuggestions(ctx, analysis)
        return &agentic.NodeOutput{Result: suggestions, ShouldEnd: true}, nil
    },
})

wf.AddEdge("analyze", "suggest", nil, "proceed")
wf.SetEntryPoint("analyze")
wf.AddEndNode("suggest")

state, err := wf.Execute(ctx, &agentic.NodeInput{Query: codeToReview})
```

### Conditional branching workflow

```go
wf.AddNode(&agentic.Node{ID: "classify", Name: "Classify Intent", Type: agentic.NodeTypeAgent, Handler: classifyHandler})
wf.AddNode(&agentic.Node{ID: "code-gen", Name: "Generate Code",   Type: agentic.NodeTypeAgent, Handler: codeGenHandler})
wf.AddNode(&agentic.Node{ID: "qa",       Name: "Answer Question", Type: agentic.NodeTypeAgent, Handler: qaHandler})

wf.AddEdge("classify", "code-gen", func(s *agentic.WorkflowState) bool {
    return s.Variables["intent"] == "code_generation"
}, "code request")

wf.AddEdge("classify", "qa", func(s *agentic.WorkflowState) bool {
    return s.Variables["intent"] == "question"
}, "question")

wf.SetEntryPoint("classify")
wf.AddEndNode("code-gen")
wf.AddEndNode("qa")
```

### Node with retry policy

```go
wf.AddNode(&agentic.Node{
    ID:   "api-call",
    Name: "External API Call",
    Type: agentic.NodeTypeTool,
    Handler: apiCallHandler,
    RetryPolicy: &agentic.RetryPolicy{
        MaxRetries: 5,
        Delay:      2 * time.Second,
        Backoff:    2.0, // exponential backoff multiplier
    },
})
```

## Configuration

```go
type WorkflowConfig struct {
    MaxIterations        int           // Maximum execution loop iterations (default: 100)
    Timeout              time.Duration // Overall workflow timeout (default: 30m)
    EnableCheckpoints    bool          // Enable periodic checkpointing (default: true)
    CheckpointInterval   int           // Checkpoint every N iterations (default: 5)
    EnableSelfCorrection bool          // Enable self-correction on errors (default: true)
    MaxRetries           int           // Default max retries per node (default: 3)
    RetryDelay           time.Duration // Default delay between retries (default: 1s)
}

type RetryPolicy struct {
    MaxRetries int           // Max retries for this specific node
    Delay      time.Duration // Initial delay
    Backoff    float64       // Backoff multiplier (e.g., 2.0 for exponential)
}
```

### Execution Guarantees

- **Timeout enforcement**: Both workflow-level and per-node timeouts via `context.WithTimeout`
- **Iteration limit**: Hard cap on loop iterations prevents infinite cycles
- **Checkpointing**: Periodic state snapshots allow `RestoreFromCheckpoint` for resumption
- **Retry with backoff**: Per-node retry policies with configurable exponential backoff
- **Graceful termination**: Nodes can signal completion via `NodeOutput.ShouldEnd` or by reaching an end node

## Testing

```bash
go build ./...
go test ./... -count=1 -race
```

## Integration with HelixAgent

Agentic connects to HelixAgent through the adapter at `internal/adapters/agentic/`. The main integration points are:

- **Debate Orchestration**: The debate orchestrator uses Agentic workflows to model multi-phase debate topologies (mesh, star, chain, tree) where each debate phase is a node and transitions are conditional on convergence criteria.
- **Tool Integration**: HelixAgent's 21-tool registry is exposed to workflow nodes, allowing agents to invoke tools (file operations, web search, code execution) during workflow steps.
- **State Persistence**: Workflow state and checkpoints can be persisted to PostgreSQL for long-running agentic tasks that span multiple sessions.
- **Human-in-the-Loop**: The `NodeTypeHuman` node type integrates with HelixAgent's approval gate REST API for workflows that require human review before proceeding.
