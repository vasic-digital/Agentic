# Agentic - Getting Started

**Module:** `digital.vasic.agentic`

## Installation

Add the module to your Go project:

```go
import "digital.vasic.agentic/agentic"
```

## Quick Start: Create and Execute a Workflow

### 1. Create a Workflow

```go
package main

import (
    "context"
    "fmt"

    "digital.vasic.agentic/agentic"
    "github.com/sirupsen/logrus"
)

func main() {
    logger := logrus.New()
    config := agentic.DefaultWorkflowConfig()
    wf := agentic.NewWorkflow("greeting-pipeline", "A two-step greeting workflow", config, logger)
```

### 2. Define Nodes

Each node has an ID, name, type, and a handler function:

```go
    greetNode := &agentic.Node{
        ID:   "greet",
        Name: "Greeter",
        Type: agentic.NodeTypeAgent,
        Handler: func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
            name := input.Query
            state.Variables["greeting"] = fmt.Sprintf("Hello, %s!", name)
            return &agentic.NodeOutput{
                Result: state.Variables["greeting"],
            }, nil
        },
    }

    formatNode := &agentic.Node{
        ID:   "format",
        Name: "Formatter",
        Type: agentic.NodeTypeTool,
        Handler: func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
            greeting := state.Variables["greeting"].(string)
            return &agentic.NodeOutput{
                Result:    fmt.Sprintf("[%s] %s", "BOT", greeting),
                ShouldEnd: true,
            }, nil
        },
    }
```

### 3. Build the Graph

```go
    wf.AddNode(greetNode)
    wf.AddNode(formatNode)
    wf.AddEdge("greet", "format", nil, "always")
    wf.SetEntryPoint("greet")
    wf.AddEndNode("format")
```

### 4. Execute

```go
    state, err := wf.Execute(context.Background(), &agentic.NodeInput{
        Query: "World",
    })
    if err != nil {
        panic(err)
    }

    fmt.Printf("Status: %s\n", state.Status)
    fmt.Printf("Steps:  %d\n", len(state.History))
}
```

## Conditional Branching

Use `ConditionFunc` on edges to route execution dynamically:

```go
wf.AddEdge("classifier", "positive-path", func(s *agentic.WorkflowState) bool {
    sentiment, _ := s.Variables["sentiment"].(string)
    return sentiment == "positive"
}, "positive")

wf.AddEdge("classifier", "negative-path", func(s *agentic.WorkflowState) bool {
    sentiment, _ := s.Variables["sentiment"].(string)
    return sentiment == "negative"
}, "negative")
```

## Retry Policies

Attach per-node retry configuration for fault tolerance:

```go
node := &agentic.Node{
    ID:      "api-call",
    Name:    "External API",
    Type:    agentic.NodeTypeTool,
    Handler: apiCallHandler,
    RetryPolicy: &agentic.RetryPolicy{
        MaxRetries: 5,
        Delay:      2 * time.Second,
        Backoff:    1.5,
    },
}
```

## Checkpoints and Recovery

Enable checkpoints for long-running workflows:

```go
config := &agentic.WorkflowConfig{
    EnableCheckpoints:  true,
    CheckpointInterval: 3, // every 3 iterations
    MaxRetries:         3,
    Timeout:            10 * time.Minute,
}

// After failure, restore from a checkpoint:
err := wf.RestoreFromCheckpoint(state, checkpointID)
```

## Node Types

| Node Type | Constant | Use Case |
|-----------|----------|----------|
| Agent | `NodeTypeAgent` | LLM-based processing |
| Tool | `NodeTypeTool` | Tool or API execution |
| Condition | `NodeTypeCondition` | Branching logic |
| Parallel | `NodeTypeParallel` | Concurrent execution |
| Human | `NodeTypeHuman` | Human-in-the-loop approval |
| Subgraph | `NodeTypeSubgraph` | Nested workflow |

## Integration with HelixAgent

HelixAgent uses the Agentic module through the adapter at
`internal/adapters/agentic/adapter.go`. The debate orchestrator
leverages workflows for multi-step agent coordination.

## Next Steps

- See [ARCHITECTURE.md](ARCHITECTURE.md) for design details
- See [API_REFERENCE.md](API_REFERENCE.md) for the full type catalog
