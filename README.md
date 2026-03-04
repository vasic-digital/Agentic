# Agentic

Graph-based workflow orchestration for autonomous AI agents.

**Module**: `digital.vasic.agentic`

## Installation

```go
import "digital.vasic.agentic/agentic"
```

## Usage

```go
wf := agentic.NewWorkflow("my-workflow", "description", nil, logger)
// Add nodes, edges, set entry point, then:
state, err := wf.Execute(ctx, &agentic.NodeInput{Query: "hello"})
```
