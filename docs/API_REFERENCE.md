# Agentic - API Reference

**Module:** `digital.vasic.agentic`
**Package:** `agentic`

## Constructor Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewWorkflow` | `NewWorkflow(name, description string, config *WorkflowConfig, logger *logrus.Logger) *Workflow` | Creates a new workflow with the given name and config. Passes `nil` for config to use defaults. |
| `DefaultWorkflowConfig` | `DefaultWorkflowConfig() *WorkflowConfig` | Returns default configuration (100 iterations, 30m timeout, checkpoints enabled). |

## Core Types

### Workflow

The top-level orchestrator. Thread-safe via internal `sync.RWMutex`.

```go
type Workflow struct {
    ID          string
    Name        string
    Description string
    Graph       *WorkflowGraph
    State       *WorkflowState
    Config      *WorkflowConfig
    Logger      *logrus.Logger
}
```

**Methods:**

| Method | Signature | Description |
|--------|-----------|-------------|
| `AddNode` | `(w *Workflow) AddNode(node *Node) error` | Adds a node to the graph. Auto-generates ID if empty. |
| `AddEdge` | `(w *Workflow) AddEdge(from, to string, condition ConditionFunc, label string) error` | Creates a directed edge. Returns error if nodes not found. |
| `SetEntryPoint` | `(w *Workflow) SetEntryPoint(nodeID string) error` | Sets the starting node for execution. |
| `AddEndNode` | `(w *Workflow) AddEndNode(nodeID string) error` | Marks a node as a terminal node. |
| `Execute` | `(w *Workflow) Execute(ctx context.Context, input *NodeInput) (*WorkflowState, error)` | Runs the workflow from the entry point to completion. |
| `RestoreFromCheckpoint` | `(w *Workflow) RestoreFromCheckpoint(state *WorkflowState, checkpointID string) error` | Restores workflow state from a saved checkpoint. |

### WorkflowGraph

Defines the DAG structure of a workflow.

```go
type WorkflowGraph struct {
    Nodes      map[string]*Node
    Edges      []*Edge
    EntryPoint string
    EndNodes   []string
}
```

### Node

A single processing unit in the workflow graph.

```go
type Node struct {
    ID          string
    Name        string
    Type        NodeType
    Handler     NodeHandler
    Condition   ConditionFunc
    Config      map[string]interface{}
    RetryPolicy *RetryPolicy
}
```

### Edge

A directed connection between two nodes.

```go
type Edge struct {
    From      string
    To        string
    Condition ConditionFunc
    Label     string
}
```

## Function Types

| Type | Signature | Description |
|------|-----------|-------------|
| `NodeHandler` | `func(ctx context.Context, state *WorkflowState, input *NodeInput) (*NodeOutput, error)` | Executes a node's logic. |
| `ConditionFunc` | `func(state *WorkflowState) bool` | Evaluates routing conditions on edges. |
| `ToolHandler` | `func(ctx context.Context, args map[string]interface{}) (interface{}, error)` | Executes a tool invocation. |

## Input/Output Types

### NodeInput

```go
type NodeInput struct {
    Query    string
    Messages []Message
    Tools    []Tool
    Context  map[string]interface{}
    Previous *NodeOutput
}
```

### NodeOutput

```go
type NodeOutput struct {
    Result    interface{}
    Messages  []Message
    ToolCalls []ToolCall
    NextNode  string          // Override next node routing
    ShouldEnd bool            // Terminate workflow
    Error     error
    Metadata  map[string]interface{}
}
```

## State Types

### WorkflowState

Mutable state threaded through all nodes. Thread-safe via `sync.RWMutex`.

```go
type WorkflowState struct {
    ID          string
    WorkflowID  string
    CurrentNode string
    Messages    []Message
    Variables   map[string]interface{}
    History     []NodeExecution
    Checkpoints []Checkpoint
    Status      WorkflowStatus
    StartTime   time.Time
    EndTime     *time.Time
    Error       error
}
```

### WorkflowConfig

```go
type WorkflowConfig struct {
    MaxIterations        int           // Default: 100
    Timeout              time.Duration // Default: 30m
    EnableCheckpoints    bool          // Default: true
    CheckpointInterval   int           // Default: 5
    EnableSelfCorrection bool          // Default: true
    MaxRetries           int           // Default: 3
    RetryDelay           time.Duration // Default: 1s
}
```

## Enums

### NodeType

| Constant | Value | Description |
|----------|-------|-------------|
| `NodeTypeAgent` | `"agent"` | LLM-based agent node |
| `NodeTypeTool` | `"tool"` | Tool execution node |
| `NodeTypeCondition` | `"condition"` | Conditional branching node |
| `NodeTypeParallel` | `"parallel"` | Parallel execution node |
| `NodeTypeHuman` | `"human"` | Human-in-the-loop node |
| `NodeTypeSubgraph` | `"subgraph"` | Nested workflow node |

### WorkflowStatus

| Constant | Value | Description |
|----------|-------|-------------|
| `StatusPending` | `"pending"` | Workflow not yet started |
| `StatusRunning` | `"running"` | Workflow actively executing |
| `StatusPaused` | `"paused"` | Workflow paused at checkpoint |
| `StatusCompleted` | `"completed"` | Workflow finished successfully |
| `StatusFailed` | `"failed"` | Workflow terminated with error |

## Supporting Types

### Message

```go
type Message struct {
    Role      string
    Content   string
    Name      string
    ToolCalls []ToolCall
}
```

### Tool / ToolCall

```go
type Tool struct {
    Name        string
    Description string
    Parameters  map[string]interface{}
    Handler     ToolHandler
}

type ToolCall struct {
    ID        string
    Name      string
    Arguments map[string]interface{}
    Result    interface{}
}
```

### RetryPolicy

```go
type RetryPolicy struct {
    MaxRetries int
    Delay      time.Duration
    Backoff    float64
}
```

### NodeExecution / Checkpoint

```go
type NodeExecution struct {
    NodeID    string
    NodeName  string
    StartTime time.Time
    EndTime   time.Time
    Input     *NodeInput
    Output    *NodeOutput
    Error     error
}

type Checkpoint struct {
    ID        string
    NodeID    string
    State     map[string]interface{}
    Timestamp time.Time
}
```
