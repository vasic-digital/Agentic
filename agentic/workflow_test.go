package agentic

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	runtime.GOMAXPROCS(2)
}

// ---------------------------------------------------------------------------
// Helper factories
// ---------------------------------------------------------------------------

// echoHandler returns a NodeHandler that copies the query into the output result.
func echoHandler() NodeHandler {
	return func(ctx context.Context, state *WorkflowState, input *NodeInput) (*NodeOutput, error) {
		result := ""
		if input != nil {
			result = input.Query
		}
		return &NodeOutput{Result: result}, nil
	}
}

// failingHandler returns a NodeHandler that always returns an error.
func failingHandler(msg string) NodeHandler {
	return func(_ context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
		return nil, errors.New(msg)
	}
}

// countingHandler returns a NodeHandler that increments an atomic counter
// on each invocation and returns the current count as the result.
func countingHandler(counter *atomic.Int64) NodeHandler {
	return func(_ context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
		c := counter.Add(1)
		return &NodeOutput{Result: c}, nil
	}
}

// shouldEndHandler returns a NodeHandler that signals the workflow to stop.
func shouldEndHandler() NodeHandler {
	return func(_ context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
		return &NodeOutput{ShouldEnd: true, Result: "done"}, nil
	}
}

// overrideNextHandler returns a NodeHandler that overrides the next node.
func overrideNextHandler(nextNodeID string) NodeHandler {
	return func(_ context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
		return &NodeOutput{NextNode: nextNodeID, Result: "redirect"}, nil
	}
}

// stateWriteHandler writes a key-value pair into the workflow state variables.
func stateWriteHandler(key string, value interface{}) NodeHandler {
	return func(_ context.Context, state *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
		state.mu.Lock()
		state.Variables[key] = value
		state.mu.Unlock()
		return &NodeOutput{Result: value}, nil
	}
}

// slowHandler returns a NodeHandler that sleeps for the given duration.
func slowHandler(d time.Duration) NodeHandler {
	return func(ctx context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
		select {
		case <-time.After(d):
			return &NodeOutput{Result: "slow-done"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// failNTimesHandler fails the first n calls, then succeeds.
func failNTimesHandler(n int) NodeHandler {
	var mu sync.Mutex
	attempts := 0
	return func(_ context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
		mu.Lock()
		attempts++
		current := attempts
		mu.Unlock()
		if current <= n {
			return nil, fmt.Errorf("transient failure %d", current)
		}
		return &NodeOutput{Result: "recovered"}, nil
	}
}

// buildLinearWorkflow creates a workflow with N nodes chained linearly.
// The first node is the entry point, the last is the end node.
func buildLinearWorkflow(t *testing.T, n int, handler NodeHandler) *Workflow {
	t.Helper()
	wf := NewWorkflow("linear", "linear workflow", nil, nil)

	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = fmt.Sprintf("node-%d", i)
		err := wf.AddNode(&Node{
			ID:      ids[i],
			Name:    fmt.Sprintf("Node %d", i),
			Type:    NodeTypeAgent,
			Handler: handler,
		})
		require.NoError(t, err)
	}

	for i := 0; i < n-1; i++ {
		err := wf.AddEdge(ids[i], ids[i+1], nil, "")
		require.NoError(t, err)
	}

	require.NoError(t, wf.SetEntryPoint(ids[0]))
	require.NoError(t, wf.AddEndNode(ids[n-1]))
	return wf
}

// ---------------------------------------------------------------------------
// DefaultWorkflowConfig
// ---------------------------------------------------------------------------

func TestDefaultWorkflowConfig(t *testing.T) {
	cfg := DefaultWorkflowConfig()
	require.NotNil(t, cfg)
	assert.Equal(t, 100, cfg.MaxIterations)
	assert.Equal(t, 30*time.Minute, cfg.Timeout)
	assert.True(t, cfg.EnableCheckpoints)
	assert.Equal(t, 5, cfg.CheckpointInterval)
	assert.True(t, cfg.EnableSelfCorrection)
	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 1*time.Second, cfg.RetryDelay)
}

// ---------------------------------------------------------------------------
// NewWorkflow
// ---------------------------------------------------------------------------

func TestNewWorkflow_WithNilConfig(t *testing.T) {
	wf := NewWorkflow("test", "desc", nil, nil)
	require.NotNil(t, wf)
	assert.Equal(t, "test", wf.Name)
	assert.Equal(t, "desc", wf.Description)
	assert.NotEmpty(t, wf.ID)
	require.NotNil(t, wf.Graph)
	assert.Empty(t, wf.Graph.Nodes)
	assert.Empty(t, wf.Graph.Edges)
	assert.Empty(t, wf.Graph.EndNodes)
	assert.Equal(t, "", wf.Graph.EntryPoint)
	require.NotNil(t, wf.Config)
	assert.Equal(t, DefaultWorkflowConfig().MaxIterations, wf.Config.MaxIterations)
	require.NotNil(t, wf.Logger)
}

func TestNewWorkflow_WithCustomConfig(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 10,
		Timeout:       5 * time.Second,
	}
	wf := NewWorkflow("custom", "", cfg, nil)
	assert.Equal(t, 10, wf.Config.MaxIterations)
	assert.Equal(t, 5*time.Second, wf.Config.Timeout)
}

func TestNewWorkflow_WithCustomLogger(t *testing.T) {
	// Just verify that supplying a non-nil logger uses it.
	wf := NewWorkflow("log", "", nil, nil)
	require.NotNil(t, wf.Logger)
}

func TestNewWorkflow_EmptyStrings(t *testing.T) {
	wf := NewWorkflow("", "", nil, nil)
	require.NotNil(t, wf)
	assert.Equal(t, "", wf.Name)
	assert.Equal(t, "", wf.Description)
	assert.NotEmpty(t, wf.ID, "ID should be auto-generated even with empty name")
}

// ---------------------------------------------------------------------------
// AddNode
// ---------------------------------------------------------------------------

func TestWorkflow_AddNode_Basic(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	node := &Node{ID: "n1", Name: "Node 1", Type: NodeTypeAgent, Handler: echoHandler()}
	err := wf.AddNode(node)
	require.NoError(t, err)

	assert.Len(t, wf.Graph.Nodes, 1)
	assert.Equal(t, node, wf.Graph.Nodes["n1"])
}

func TestWorkflow_AddNode_EmptyID(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	node := &Node{Name: "NoID", Type: NodeTypeTool}
	err := wf.AddNode(node)
	require.NoError(t, err)
	assert.NotEmpty(t, node.ID, "empty ID should be auto-generated")
	assert.Len(t, wf.Graph.Nodes, 1)
}

func TestWorkflow_AddNode_MultipleNodes(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	for i := 0; i < 10; i++ {
		err := wf.AddNode(&Node{
			ID:   fmt.Sprintf("n%d", i),
			Name: fmt.Sprintf("Node %d", i),
			Type: NodeTypeAgent,
		})
		require.NoError(t, err)
	}
	assert.Len(t, wf.Graph.Nodes, 10)
}

func TestWorkflow_AddNode_OverwritesSameID(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	node1 := &Node{ID: "dup", Name: "First"}
	node2 := &Node{ID: "dup", Name: "Second"}
	require.NoError(t, wf.AddNode(node1))
	require.NoError(t, wf.AddNode(node2))

	assert.Len(t, wf.Graph.Nodes, 1)
	assert.Equal(t, "Second", wf.Graph.Nodes["dup"].Name)
}

func TestWorkflow_AddNode_AllNodeTypes(t *testing.T) {
	types := []NodeType{
		NodeTypeAgent, NodeTypeTool, NodeTypeCondition,
		NodeTypeParallel, NodeTypeHuman, NodeTypeSubgraph,
	}
	wf := NewWorkflow("types", "", nil, nil)
	for i, nt := range types {
		err := wf.AddNode(&Node{
			ID:   fmt.Sprintf("t%d", i),
			Name: string(nt),
			Type: nt,
		})
		require.NoError(t, err)
	}
	assert.Len(t, wf.Graph.Nodes, len(types))
}

func TestWorkflow_AddNode_NilHandler(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	err := wf.AddNode(&Node{ID: "nil-h", Name: "NilHandler"})
	require.NoError(t, err)
}

func TestWorkflow_AddNode_WithConfig(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	cfg := map[string]interface{}{"temperature": 0.7, "max_tokens": 100}
	err := wf.AddNode(&Node{
		ID:     "cfg",
		Name:   "Configured",
		Type:   NodeTypeAgent,
		Config: cfg,
	})
	require.NoError(t, err)
	assert.Equal(t, 0.7, wf.Graph.Nodes["cfg"].Config["temperature"])
}

func TestWorkflow_AddNode_WithRetryPolicy(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	err := wf.AddNode(&Node{
		ID:   "retry",
		Name: "Retryable",
		Type: NodeTypeTool,
		RetryPolicy: &RetryPolicy{
			MaxRetries: 5,
			Delay:      100 * time.Millisecond,
			Backoff:    2.0,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 5, wf.Graph.Nodes["retry"].RetryPolicy.MaxRetries)
}

// ---------------------------------------------------------------------------
// AddEdge
// ---------------------------------------------------------------------------

func TestWorkflow_AddEdge_Basic(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a"}))
	require.NoError(t, wf.AddNode(&Node{ID: "b"}))

	err := wf.AddEdge("a", "b", nil, "a-to-b")
	require.NoError(t, err)
	assert.Len(t, wf.Graph.Edges, 1)
	assert.Equal(t, "a", wf.Graph.Edges[0].From)
	assert.Equal(t, "b", wf.Graph.Edges[0].To)
	assert.Equal(t, "a-to-b", wf.Graph.Edges[0].Label)
}

func TestWorkflow_AddEdge_SourceNotFound(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "b"}))

	err := wf.AddEdge("missing", "b", nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source node not found")
}

func TestWorkflow_AddEdge_TargetNotFound(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a"}))

	err := wf.AddEdge("a", "missing", nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target node not found")
}

func TestWorkflow_AddEdge_BothMissing(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	err := wf.AddEdge("x", "y", nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source node not found")
}

func TestWorkflow_AddEdge_WithCondition(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a"}))
	require.NoError(t, wf.AddNode(&Node{ID: "b"}))

	cond := func(s *WorkflowState) bool { return true }
	err := wf.AddEdge("a", "b", cond, "conditional")
	require.NoError(t, err)
	assert.NotNil(t, wf.Graph.Edges[0].Condition)
}

func TestWorkflow_AddEdge_SelfLoop(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "loop"}))

	err := wf.AddEdge("loop", "loop", nil, "self")
	require.NoError(t, err)
}

func TestWorkflow_AddEdge_DuplicateEdges(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a"}))
	require.NoError(t, wf.AddNode(&Node{ID: "b"}))

	require.NoError(t, wf.AddEdge("a", "b", nil, "first"))
	require.NoError(t, wf.AddEdge("a", "b", nil, "second"))
	assert.Len(t, wf.Graph.Edges, 2)
}

// ---------------------------------------------------------------------------
// SetEntryPoint
// ---------------------------------------------------------------------------

func TestWorkflow_SetEntryPoint_Success(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "start"}))

	err := wf.SetEntryPoint("start")
	require.NoError(t, err)
	assert.Equal(t, "start", wf.Graph.EntryPoint)
}

func TestWorkflow_SetEntryPoint_NotFound(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	err := wf.SetEntryPoint("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "node not found")
}

func TestWorkflow_SetEntryPoint_Override(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a"}))
	require.NoError(t, wf.AddNode(&Node{ID: "b"}))

	require.NoError(t, wf.SetEntryPoint("a"))
	assert.Equal(t, "a", wf.Graph.EntryPoint)

	require.NoError(t, wf.SetEntryPoint("b"))
	assert.Equal(t, "b", wf.Graph.EntryPoint)
}

// ---------------------------------------------------------------------------
// AddEndNode
// ---------------------------------------------------------------------------

func TestWorkflow_AddEndNode_Success(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "end"}))

	err := wf.AddEndNode("end")
	require.NoError(t, err)
	assert.Contains(t, wf.Graph.EndNodes, "end")
}

func TestWorkflow_AddEndNode_NotFound(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	err := wf.AddEndNode("ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "node not found")
}

func TestWorkflow_AddEndNode_Multiple(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "e1"}))
	require.NoError(t, wf.AddNode(&Node{ID: "e2"}))
	require.NoError(t, wf.AddNode(&Node{ID: "e3"}))

	require.NoError(t, wf.AddEndNode("e1"))
	require.NoError(t, wf.AddEndNode("e2"))
	require.NoError(t, wf.AddEndNode("e3"))
	assert.Len(t, wf.Graph.EndNodes, 3)
}

func TestWorkflow_AddEndNode_Duplicate(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "end"}))

	require.NoError(t, wf.AddEndNode("end"))
	require.NoError(t, wf.AddEndNode("end"))
	// Duplicates are appended — not deduplicated.
	assert.Len(t, wf.Graph.EndNodes, 2)
}

// ---------------------------------------------------------------------------
// Execute — entry conditions
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_NoEntryPoint(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a", Handler: echoHandler()}))

	state, err := wf.Execute(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, state)
	assert.Contains(t, err.Error(), "no entry point defined")
}

func TestWorkflow_Execute_NilInput(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a", Handler: shouldEndHandler()}))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, StatusCompleted, state.Status)
}

// ---------------------------------------------------------------------------
// Execute — single node workflows
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_SingleNodeShouldEnd(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "only", Name: "Only", Handler: shouldEndHandler()}))
	require.NoError(t, wf.SetEntryPoint("only"))

	state, err := wf.Execute(context.Background(), &NodeInput{Query: "hello"})
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	assert.NotNil(t, state.EndTime)
	assert.Len(t, state.History, 1)
	assert.Equal(t, "only", state.History[0].NodeID)
}

func TestWorkflow_Execute_SingleEndNode(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "x", Name: "X", Handler: echoHandler()}))
	require.NoError(t, wf.SetEntryPoint("x"))
	require.NoError(t, wf.AddEndNode("x"))

	state, err := wf.Execute(context.Background(), &NodeInput{Query: "hi"})
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
}

func TestWorkflow_Execute_SingleNodeNoEdgesNoEnd(t *testing.T) {
	// Node is neither an end node nor has outgoing edges — should complete
	// because getNextNode returns "" and the loop exits.
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "lonely", Name: "Lonely", Handler: echoHandler()}))
	require.NoError(t, wf.SetEntryPoint("lonely"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
}

// ---------------------------------------------------------------------------
// Execute — linear multi-node workflows
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_LinearChain(t *testing.T) {
	var counter atomic.Int64
	wf := buildLinearWorkflow(t, 5, countingHandler(&counter))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	assert.Equal(t, int64(5), counter.Load())
	assert.Len(t, state.History, 5)
}

func TestWorkflow_Execute_InputMessagesCopied(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a", Handler: shouldEndHandler()}))
	require.NoError(t, wf.SetEntryPoint("a"))

	msgs := []Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}}
	state, err := wf.Execute(context.Background(), &NodeInput{Messages: msgs})
	require.NoError(t, err)
	assert.Len(t, state.Messages, 2)
	assert.Equal(t, "user", state.Messages[0].Role)
	assert.Equal(t, "hello", state.Messages[0].Content)
}

func TestWorkflow_Execute_StateFieldsInitialized(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "s", Handler: shouldEndHandler()}))
	require.NoError(t, wf.SetEntryPoint("s"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)

	assert.NotEmpty(t, state.ID)
	assert.Equal(t, wf.ID, state.WorkflowID)
	assert.NotNil(t, state.Variables)
	assert.NotNil(t, state.History)
	assert.NotNil(t, state.Checkpoints)
	assert.NotNil(t, state.EndTime)
	assert.False(t, state.StartTime.IsZero())
	assert.Nil(t, state.Error)
}

// ---------------------------------------------------------------------------
// Execute — state propagation across nodes
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_StatePropagation(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	// Node A writes a variable.
	require.NoError(t, wf.AddNode(&Node{
		ID: "a", Name: "Writer", Type: NodeTypeAgent,
		Handler: stateWriteHandler("key1", "value1"),
	}))

	// Node B reads the variable written by A and verifies it.
	require.NoError(t, wf.AddNode(&Node{
		ID: "b", Name: "Reader", Type: NodeTypeAgent,
		Handler: func(_ context.Context, state *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			state.mu.RLock()
			v, ok := state.Variables["key1"]
			state.mu.RUnlock()
			if !ok || v != "value1" {
				return nil, fmt.Errorf("expected key1=value1, got %v", v)
			}
			return &NodeOutput{ShouldEnd: true, Result: v}, nil
		},
	}))

	require.NoError(t, wf.AddEdge("a", "b", nil, ""))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	assert.Len(t, state.History, 2)
}

func TestWorkflow_Execute_PreviousOutputPassedToNext(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	// Node A produces a result.
	require.NoError(t, wf.AddNode(&Node{
		ID: "a", Name: "Producer", Handler: func(_ context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			return &NodeOutput{Result: "from-a", Metadata: map[string]interface{}{"source": "a"}}, nil
		},
	}))

	// Node B verifies it receives A's output via input.Previous.
	require.NoError(t, wf.AddNode(&Node{
		ID: "b", Name: "Consumer", Handler: func(_ context.Context, _ *WorkflowState, input *NodeInput) (*NodeOutput, error) {
			if input == nil || input.Previous == nil {
				return nil, fmt.Errorf("expected previous output")
			}
			if input.Previous.Result != "from-a" {
				return nil, fmt.Errorf("expected result 'from-a', got %v", input.Previous.Result)
			}
			return &NodeOutput{ShouldEnd: true}, nil
		},
	}))

	require.NoError(t, wf.AddEdge("a", "b", nil, ""))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
}

// ---------------------------------------------------------------------------
// Execute — NextNode override
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_NextNodeOverride(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	// A has edges to B and C, but overrides next to C.
	require.NoError(t, wf.AddNode(&Node{
		ID: "a", Name: "A", Handler: overrideNextHandler("c"),
	}))
	require.NoError(t, wf.AddNode(&Node{
		ID: "b", Name: "B", Handler: func(_ context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			return nil, fmt.Errorf("should not reach B")
		},
	}))
	require.NoError(t, wf.AddNode(&Node{
		ID: "c", Name: "C", Handler: shouldEndHandler(),
	}))

	require.NoError(t, wf.AddEdge("a", "b", nil, "default"))
	require.NoError(t, wf.AddEdge("a", "c", nil, "alt"))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	// Only A and C should be in history.
	assert.Len(t, state.History, 2)
	assert.Equal(t, "a", state.History[0].NodeID)
	assert.Equal(t, "c", state.History[1].NodeID)
}

// ---------------------------------------------------------------------------
// Execute — conditional branching
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_ConditionalBranching(t *testing.T) {
	tests := []struct {
		name       string
		writeValue string
		expected   string
	}{
		{"TrueBranch", "go-left", "left"},
		{"FalseBranch", "go-right", "right"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wf := NewWorkflow("cond", "", nil, nil)

			require.NoError(t, wf.AddNode(&Node{
				ID: "start", Name: "Start", Type: NodeTypeAgent,
				Handler: stateWriteHandler("direction", tc.writeValue),
			}))
			require.NoError(t, wf.AddNode(&Node{
				ID: "left", Name: "Left", Type: NodeTypeAgent,
				Handler: shouldEndHandler(),
			}))
			require.NoError(t, wf.AddNode(&Node{
				ID: "right", Name: "Right", Type: NodeTypeAgent,
				Handler: shouldEndHandler(),
			}))

			goLeft := func(state *WorkflowState) bool {
				state.mu.RLock()
				defer state.mu.RUnlock()
				return state.Variables["direction"] == "go-left"
			}
			goRight := func(state *WorkflowState) bool {
				state.mu.RLock()
				defer state.mu.RUnlock()
				return state.Variables["direction"] != "go-left"
			}

			require.NoError(t, wf.AddEdge("start", "left", goLeft, "left"))
			require.NoError(t, wf.AddEdge("start", "right", goRight, "right"))
			require.NoError(t, wf.SetEntryPoint("start"))

			state, err := wf.Execute(context.Background(), nil)
			require.NoError(t, err)
			assert.Equal(t, StatusCompleted, state.Status)
			assert.Len(t, state.History, 2)
			assert.Equal(t, tc.expected, state.History[1].NodeID)
		})
	}
}

func TestWorkflow_Execute_ConditionalEdge_NoMatch(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{
		ID: "a", Name: "A", Handler: echoHandler(),
	}))
	require.NoError(t, wf.AddNode(&Node{
		ID: "b", Name: "B", Handler: shouldEndHandler(),
	}))

	// Condition always returns false — B will never be reached.
	never := func(_ *WorkflowState) bool { return false }
	require.NoError(t, wf.AddEdge("a", "b", never, "blocked"))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	// Completes because getNextNode returns "" (no matching edge).
	assert.Equal(t, StatusCompleted, state.Status)
	assert.Len(t, state.History, 1)
}

// ---------------------------------------------------------------------------
// Execute — node types
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_ToolNode(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{
		ID: "tool1", Name: "ToolNode", Type: NodeTypeTool,
		Handler: func(_ context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			return &NodeOutput{
				Result: "tool-result",
				ToolCalls: []ToolCall{
					{ID: "tc1", Name: "search", Arguments: map[string]interface{}{"q": "test"}},
				},
				ShouldEnd: true,
			}, nil
		},
	}))
	require.NoError(t, wf.SetEntryPoint("tool1"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	assert.Equal(t, "tool-result", state.History[0].Output.Result)
	assert.Len(t, state.History[0].Output.ToolCalls, 1)
}

func TestWorkflow_Execute_ConditionNode(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	condNode := &Node{
		ID: "cond", Name: "CondNode", Type: NodeTypeCondition,
		Handler: func(_ context.Context, state *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			state.mu.RLock()
			defer state.mu.RUnlock()
			return &NodeOutput{Result: "condition-evaluated", ShouldEnd: true}, nil
		},
		Condition: func(state *WorkflowState) bool { return true },
	}
	require.NoError(t, wf.AddNode(condNode))
	require.NoError(t, wf.SetEntryPoint("cond"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
}

func TestWorkflow_Execute_ParallelNode(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	// Simulate parallel execution within a single handler.
	require.NoError(t, wf.AddNode(&Node{
		ID: "par", Name: "ParallelNode", Type: NodeTypeParallel,
		Handler: func(ctx context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			var wg sync.WaitGroup
			results := make([]string, 3)
			for i := 0; i < 3; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					results[idx] = fmt.Sprintf("result-%d", idx)
				}(i)
			}
			wg.Wait()
			return &NodeOutput{Result: results, ShouldEnd: true}, nil
		},
	}))
	require.NoError(t, wf.SetEntryPoint("par"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	results, ok := state.History[0].Output.Result.([]string)
	require.True(t, ok)
	assert.Len(t, results, 3)
}

func TestWorkflow_Execute_HumanNode(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{
		ID: "human", Name: "HumanNode", Type: NodeTypeHuman,
		Handler: func(_ context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			return &NodeOutput{Result: "human-approved", ShouldEnd: true}, nil
		},
	}))
	require.NoError(t, wf.SetEntryPoint("human"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
}

func TestWorkflow_Execute_SubgraphNode(t *testing.T) {
	// Create inner workflow.
	inner := NewWorkflow("inner", "", nil, nil)
	require.NoError(t, inner.AddNode(&Node{
		ID: "inner-start", Name: "InnerStart", Handler: func(_ context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			return &NodeOutput{Result: "inner-done", ShouldEnd: true}, nil
		},
	}))
	require.NoError(t, inner.SetEntryPoint("inner-start"))

	// Outer workflow wraps the inner as a subgraph node.
	outer := NewWorkflow("outer", "", nil, nil)
	require.NoError(t, outer.AddNode(&Node{
		ID: "subgraph", Name: "SubgraphNode", Type: NodeTypeSubgraph,
		Handler: func(ctx context.Context, _ *WorkflowState, input *NodeInput) (*NodeOutput, error) {
			innerState, err := inner.Execute(ctx, input)
			if err != nil {
				return nil, err
			}
			return &NodeOutput{
				Result:    innerState.History[0].Output.Result,
				ShouldEnd: true,
			}, nil
		},
	}))
	require.NoError(t, outer.SetEntryPoint("subgraph"))

	state, err := outer.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	assert.Equal(t, "inner-done", state.History[0].Output.Result)
}

// ---------------------------------------------------------------------------
// Execute — nil handler
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_NilHandler(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "nil-h", Name: "NilHandler"}))
	require.NoError(t, wf.SetEntryPoint("nil-h"))
	require.NoError(t, wf.AddEndNode("nil-h"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	// nil handler returns empty NodeOutput.
	assert.NotNil(t, state.History[0].Output)
}

// ---------------------------------------------------------------------------
// Execute — error handling
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_NodeError(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 100,
		Timeout:       10 * time.Second,
		MaxRetries:    0, // No retries.
		RetryDelay:    0,
	}
	wf := NewWorkflow("test", "", cfg, nil)
	require.NoError(t, wf.AddNode(&Node{
		ID: "fail", Name: "Failing", Handler: failingHandler("boom"),
	}))
	require.NoError(t, wf.SetEntryPoint("fail"))

	state, err := wf.Execute(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "node Failing failed")
	assert.Contains(t, err.Error(), "boom")
	assert.Equal(t, StatusFailed, state.Status)
	assert.NotNil(t, state.Error)
	assert.NotNil(t, state.EndTime)
}

func TestWorkflow_Execute_ErrorInMiddleOfChain(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 100,
		Timeout:       10 * time.Second,
		MaxRetries:    0,
		RetryDelay:    0,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	require.NoError(t, wf.AddNode(&Node{ID: "a", Name: "A", Handler: echoHandler()}))
	require.NoError(t, wf.AddNode(&Node{ID: "b", Name: "B", Handler: failingHandler("mid-fail")}))
	require.NoError(t, wf.AddNode(&Node{ID: "c", Name: "C", Handler: shouldEndHandler()}))

	require.NoError(t, wf.AddEdge("a", "b", nil, ""))
	require.NoError(t, wf.AddEdge("b", "c", nil, ""))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, StatusFailed, state.Status)
	// History should have A (success) and B (failure).
	assert.GreaterOrEqual(t, len(state.History), 2)
	assert.Equal(t, "a", state.History[0].NodeID)
	assert.Equal(t, "b", state.History[1].NodeID)
	assert.NotNil(t, state.History[1].Error)
}

// ---------------------------------------------------------------------------
// Execute — retry policies
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_GlobalRetryPolicy(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 100,
		Timeout:       10 * time.Second,
		MaxRetries:    3,
		RetryDelay:    1 * time.Millisecond,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	// Handler fails 2 times, then succeeds on 3rd attempt (attempt index 2).
	require.NoError(t, wf.AddNode(&Node{
		ID: "retry-node", Name: "RetryNode",
		Handler: failNTimesHandler(2),
	}))
	require.NoError(t, wf.SetEntryPoint("retry-node"))
	require.NoError(t, wf.AddEndNode("retry-node"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	assert.Equal(t, "recovered", state.History[0].Output.Result)
}

func TestWorkflow_Execute_NodeRetryPolicy(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 100,
		Timeout:       10 * time.Second,
		MaxRetries:    0, // Global: no retries.
		RetryDelay:    0,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	// Node has its own retry policy.
	require.NoError(t, wf.AddNode(&Node{
		ID: "node-retry", Name: "NodeRetry",
		Handler: failNTimesHandler(2),
		RetryPolicy: &RetryPolicy{
			MaxRetries: 3,
			Delay:      1 * time.Millisecond,
			Backoff:    1.0,
		},
	}))
	require.NoError(t, wf.SetEntryPoint("node-retry"))
	require.NoError(t, wf.AddEndNode("node-retry"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
}

func TestWorkflow_Execute_RetryExhausted(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 100,
		Timeout:       10 * time.Second,
		MaxRetries:    2,
		RetryDelay:    1 * time.Millisecond,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	// Fails forever — will exhaust retries.
	require.NoError(t, wf.AddNode(&Node{
		ID: "always-fail", Name: "AlwaysFail",
		Handler: failingHandler("permanent"),
	}))
	require.NoError(t, wf.SetEntryPoint("always-fail"))

	state, err := wf.Execute(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, StatusFailed, state.Status)
	assert.Contains(t, err.Error(), "permanent")
}

func TestWorkflow_Execute_RetryWithBackoff(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 100,
		Timeout:       10 * time.Second,
		MaxRetries:    0,
		RetryDelay:    0,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	start := time.Now()
	require.NoError(t, wf.AddNode(&Node{
		ID: "backoff", Name: "Backoff",
		Handler: failNTimesHandler(2),
		RetryPolicy: &RetryPolicy{
			MaxRetries: 3,
			Delay:      10 * time.Millisecond,
			Backoff:    2.0,
		},
	}))
	require.NoError(t, wf.SetEntryPoint("backoff"))
	require.NoError(t, wf.AddEndNode("backoff"))

	state, err := wf.Execute(context.Background(), nil)
	elapsed := time.Since(start)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	// With backoff=2.0: attempt 0 fails → delay 10ms*2^0=10ms, attempt 1 fails → delay 10ms*2^1=20ms.
	// Total minimum delay ~30ms.
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(20))
}

// ---------------------------------------------------------------------------
// Execute — max iterations
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_MaxIterations(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 3,
		Timeout:       10 * time.Second,
		MaxRetries:    0,
		RetryDelay:    0,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	// Create a cycle: a → b → a (infinite loop).
	require.NoError(t, wf.AddNode(&Node{ID: "a", Name: "A", Handler: echoHandler()}))
	require.NoError(t, wf.AddNode(&Node{ID: "b", Name: "B", Handler: echoHandler()}))
	require.NoError(t, wf.AddEdge("a", "b", nil, ""))
	require.NoError(t, wf.AddEdge("b", "a", nil, ""))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max iterations reached: 3")
	assert.Equal(t, StatusFailed, state.Status)
}

func TestWorkflow_Execute_MaxIterationsOne(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 1,
		Timeout:       10 * time.Second,
		MaxRetries:    0,
		RetryDelay:    0,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	require.NoError(t, wf.AddNode(&Node{ID: "a", Name: "A", Handler: echoHandler()}))
	require.NoError(t, wf.AddNode(&Node{ID: "b", Name: "B", Handler: shouldEndHandler()}))
	require.NoError(t, wf.AddEdge("a", "b", nil, ""))
	require.NoError(t, wf.SetEntryPoint("a"))

	// Only 1 iteration allowed. A runs, then max iterations is hit when trying B.
	state, err := wf.Execute(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max iterations reached")
	assert.Len(t, state.History, 1)
}

func TestWorkflow_Execute_MaxIterationsZero(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 0,
		Timeout:       10 * time.Second,
		MaxRetries:    0,
		RetryDelay:    0,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	require.NoError(t, wf.AddNode(&Node{ID: "a", Name: "A", Handler: shouldEndHandler()}))
	require.NoError(t, wf.SetEntryPoint("a"))

	// MaxIterations=0 means the loop immediately triggers max iterations.
	state, err := wf.Execute(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max iterations reached: 0")
	assert.Equal(t, StatusFailed, state.Status)
}

// ---------------------------------------------------------------------------
// Execute — context cancellation and timeout
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_ContextCancellation(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	require.NoError(t, wf.AddNode(&Node{
		ID: "slow", Name: "Slow",
		Handler: slowHandler(10 * time.Second),
	}))
	require.NoError(t, wf.SetEntryPoint("slow"))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	state, err := wf.Execute(ctx, nil)
	require.Error(t, err)
	assert.Equal(t, StatusFailed, state.Status)
}

func TestWorkflow_Execute_Timeout(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 100,
		Timeout:       50 * time.Millisecond,
		MaxRetries:    0,
		RetryDelay:    0,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	require.NoError(t, wf.AddNode(&Node{
		ID: "slow", Name: "Slow",
		Handler: slowHandler(5 * time.Second),
	}))
	require.NoError(t, wf.SetEntryPoint("slow"))

	state, err := wf.Execute(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, StatusFailed, state.Status)
}

func TestWorkflow_Execute_TimeoutBetweenNodes(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 100,
		Timeout:       100 * time.Millisecond,
		MaxRetries:    0,
		RetryDelay:    0,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	// Create many nodes that each take some time.
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("n%d", i)
		require.NoError(t, wf.AddNode(&Node{
			ID: id, Name: id,
			Handler: slowHandler(20 * time.Millisecond),
		}))
		if i > 0 {
			prev := fmt.Sprintf("n%d", i-1)
			require.NoError(t, wf.AddEdge(prev, id, nil, ""))
		}
	}
	require.NoError(t, wf.SetEntryPoint("n0"))
	require.NoError(t, wf.AddEndNode("n19"))

	state, err := wf.Execute(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, StatusFailed, state.Status)
}

// ---------------------------------------------------------------------------
// Execute — checkpoints
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_CheckpointsCreated(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations:      100,
		Timeout:            10 * time.Second,
		EnableCheckpoints:  true,
		CheckpointInterval: 1, // Checkpoint after every iteration.
		MaxRetries:         0,
		RetryDelay:         0,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	// 4 nodes in a chain: checkpoint at iteration 1, 2, 3 (transition points).
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("n%d", i)
		require.NoError(t, wf.AddNode(&Node{
			ID: id, Name: id,
			Handler: stateWriteHandler(fmt.Sprintf("step%d", i), i),
		}))
		if i > 0 {
			prev := fmt.Sprintf("n%d", i-1)
			require.NoError(t, wf.AddEdge(prev, id, nil, ""))
		}
	}
	require.NoError(t, wf.SetEntryPoint("n0"))
	require.NoError(t, wf.AddEndNode("n3"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	// Checkpoints are created at the end of an iteration (after determining next node).
	// With interval=1, checkpoints at iterations 1, 2, 3. But iteration 4 hits end node.
	assert.Greater(t, len(state.Checkpoints), 0)
}

func TestWorkflow_Execute_CheckpointsDisabled(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations:      100,
		Timeout:            10 * time.Second,
		EnableCheckpoints:  false,
		CheckpointInterval: 1,
		MaxRetries:         0,
		RetryDelay:         0,
	}
	wf := buildLinearWorkflow(t, 5, echoHandler())
	wf.Config = cfg

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, state.Checkpoints)
}

func TestWorkflow_Execute_CheckpointCapturesState(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations:      100,
		Timeout:            10 * time.Second,
		EnableCheckpoints:  true,
		CheckpointInterval: 1,
		MaxRetries:         0,
		RetryDelay:         0,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	require.NoError(t, wf.AddNode(&Node{
		ID: "a", Name: "A",
		Handler: stateWriteHandler("key", "value-from-a"),
	}))
	require.NoError(t, wf.AddNode(&Node{
		ID: "b", Name: "B",
		Handler: shouldEndHandler(),
	}))
	require.NoError(t, wf.AddEdge("a", "b", nil, ""))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)

	require.NotEmpty(t, state.Checkpoints)
	cp := state.Checkpoints[0]
	assert.NotEmpty(t, cp.ID)
	assert.Equal(t, "b", cp.NodeID) // Checkpoint records next node.
	assert.Equal(t, "value-from-a", cp.State["key"])
	assert.False(t, cp.Timestamp.IsZero())
}

// ---------------------------------------------------------------------------
// RestoreFromCheckpoint
// ---------------------------------------------------------------------------

func TestWorkflow_RestoreFromCheckpoint_Success(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a"}))
	require.NoError(t, wf.AddNode(&Node{ID: "b"}))

	state := &WorkflowState{
		CurrentNode: "a",
		Variables:   map[string]interface{}{"x": 1},
		Status:      StatusPaused,
		Checkpoints: []Checkpoint{
			{
				ID:     "cp-1",
				NodeID: "b",
				State:  map[string]interface{}{"x": 2, "y": 3},
			},
		},
	}

	err := wf.RestoreFromCheckpoint(state, "cp-1")
	require.NoError(t, err)
	assert.Equal(t, "b", state.CurrentNode)
	assert.Equal(t, 2, state.Variables["x"])
	assert.Equal(t, 3, state.Variables["y"])
	assert.Equal(t, StatusRunning, state.Status)
}

func TestWorkflow_RestoreFromCheckpoint_NotFound(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	state := &WorkflowState{
		Checkpoints: []Checkpoint{
			{ID: "cp-1", NodeID: "a", State: map[string]interface{}{}},
		},
	}

	err := wf.RestoreFromCheckpoint(state, "cp-999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checkpoint not found: cp-999")
}

func TestWorkflow_RestoreFromCheckpoint_EmptyCheckpoints(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	state := &WorkflowState{
		Checkpoints: []Checkpoint{},
	}

	err := wf.RestoreFromCheckpoint(state, "any")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checkpoint not found")
}

func TestWorkflow_RestoreFromCheckpoint_MultipleCheckpoints(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a"}))
	require.NoError(t, wf.AddNode(&Node{ID: "b"}))
	require.NoError(t, wf.AddNode(&Node{ID: "c"}))

	state := &WorkflowState{
		CurrentNode: "c",
		Variables:   map[string]interface{}{},
		Checkpoints: []Checkpoint{
			{ID: "cp-1", NodeID: "a", State: map[string]interface{}{"step": 1}},
			{ID: "cp-2", NodeID: "b", State: map[string]interface{}{"step": 2}},
			{ID: "cp-3", NodeID: "c", State: map[string]interface{}{"step": 3}},
		},
	}

	// Restore to second checkpoint.
	err := wf.RestoreFromCheckpoint(state, "cp-2")
	require.NoError(t, err)
	assert.Equal(t, "b", state.CurrentNode)
	assert.Equal(t, 2, state.Variables["step"])
}

// ---------------------------------------------------------------------------
// Execute — missing CurrentNode during execution
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_MissingCurrentNode(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 100,
		Timeout:       10 * time.Second,
		MaxRetries:    0,
		RetryDelay:    0,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	// Node A overrides next to a non-existent node.
	require.NoError(t, wf.AddNode(&Node{
		ID: "a", Name: "A",
		Handler: overrideNextHandler("phantom"),
	}))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "node not found: phantom")
	assert.Equal(t, StatusFailed, state.Status)
}

// ---------------------------------------------------------------------------
// Execute — history recording
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_HistoryRecordsTimings(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{
		ID: "a", Name: "A",
		Handler: func(_ context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			time.Sleep(5 * time.Millisecond)
			return &NodeOutput{ShouldEnd: true}, nil
		},
	}))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)

	require.Len(t, state.History, 1)
	exec := state.History[0]
	assert.Equal(t, "a", exec.NodeID)
	assert.Equal(t, "A", exec.NodeName)
	assert.False(t, exec.StartTime.IsZero())
	assert.False(t, exec.EndTime.IsZero())
	assert.True(t, exec.EndTime.After(exec.StartTime) || exec.EndTime.Equal(exec.StartTime))
	assert.Nil(t, exec.Error)
}

func TestWorkflow_Execute_HistoryRecordsErrors(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 100,
		Timeout:       10 * time.Second,
		MaxRetries:    0,
		RetryDelay:    0,
	}
	wf := NewWorkflow("test", "", cfg, nil)
	require.NoError(t, wf.AddNode(&Node{
		ID: "fail", Name: "Fail", Handler: failingHandler("oops"),
	}))
	require.NoError(t, wf.SetEntryPoint("fail"))

	state, err := wf.Execute(context.Background(), nil)
	require.Error(t, err)

	require.Len(t, state.History, 1)
	assert.NotNil(t, state.History[0].Error)
	assert.Contains(t, state.History[0].Error.Error(), "oops")
}

// ---------------------------------------------------------------------------
// Execute — edge cases with workflow graph
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_DiamondGraph(t *testing.T) {
	// Diamond: start → (left, right) → end
	// Only the first matching edge is taken (left).
	cfg := &WorkflowConfig{
		MaxIterations: 100,
		Timeout:       10 * time.Second,
		MaxRetries:    0,
		RetryDelay:    0,
	}
	wf := NewWorkflow("diamond", "", cfg, nil)

	require.NoError(t, wf.AddNode(&Node{ID: "start", Name: "Start", Handler: echoHandler()}))
	require.NoError(t, wf.AddNode(&Node{ID: "left", Name: "Left", Handler: echoHandler()}))
	require.NoError(t, wf.AddNode(&Node{ID: "right", Name: "Right", Handler: echoHandler()}))
	require.NoError(t, wf.AddNode(&Node{ID: "end", Name: "End", Handler: shouldEndHandler()}))

	require.NoError(t, wf.AddEdge("start", "left", nil, ""))
	require.NoError(t, wf.AddEdge("start", "right", nil, ""))
	require.NoError(t, wf.AddEdge("left", "end", nil, ""))
	require.NoError(t, wf.AddEdge("right", "end", nil, ""))
	require.NoError(t, wf.SetEntryPoint("start"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	// First matching edge from start goes to "left", then "left" → "end".
	assert.Len(t, state.History, 3)
	assert.Equal(t, "start", state.History[0].NodeID)
	assert.Equal(t, "left", state.History[1].NodeID)
	assert.Equal(t, "end", state.History[2].NodeID)
}

func TestWorkflow_Execute_MultipleEdgesFirstMatchWins(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 100,
		Timeout:       10 * time.Second,
		MaxRetries:    0,
		RetryDelay:    0,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	require.NoError(t, wf.AddNode(&Node{ID: "a", Name: "A", Handler: echoHandler()}))
	require.NoError(t, wf.AddNode(&Node{ID: "b", Name: "B", Handler: shouldEndHandler()}))
	require.NoError(t, wf.AddNode(&Node{ID: "c", Name: "C", Handler: shouldEndHandler()}))

	// Both unconditional — first added wins.
	require.NoError(t, wf.AddEdge("a", "b", nil, "first"))
	require.NoError(t, wf.AddEdge("a", "c", nil, "second"))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, state.History, 2)
	assert.Equal(t, "b", state.History[1].NodeID)
}

// ---------------------------------------------------------------------------
// Execute — ShouldEnd from non-end node
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_ShouldEndFromMiddleNode(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	require.NoError(t, wf.AddNode(&Node{ID: "a", Name: "A", Handler: echoHandler()}))
	require.NoError(t, wf.AddNode(&Node{
		ID: "b", Name: "B",
		Handler: func(_ context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			return &NodeOutput{ShouldEnd: true, Result: "early-exit"}, nil
		},
	}))
	require.NoError(t, wf.AddNode(&Node{ID: "c", Name: "C", Handler: echoHandler()}))

	require.NoError(t, wf.AddEdge("a", "b", nil, ""))
	require.NoError(t, wf.AddEdge("b", "c", nil, ""))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	assert.Len(t, state.History, 2) // A and B — C never runs.
}

// ---------------------------------------------------------------------------
// Execute — workflow with only entry and end on same node
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_EntryIsEndNode(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{
		ID: "solo", Name: "Solo", Handler: echoHandler(),
	}))
	require.NoError(t, wf.SetEntryPoint("solo"))
	require.NoError(t, wf.AddEndNode("solo"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	assert.Len(t, state.History, 1)
}

// ---------------------------------------------------------------------------
// Execute — complex workflow with state, conditions, and retries
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_ComplexWorkflow(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations:      100,
		Timeout:            10 * time.Second,
		EnableCheckpoints:  true,
		CheckpointInterval: 2,
		MaxRetries:         0,
		RetryDelay:         0,
	}
	wf := NewWorkflow("complex", "Complex workflow", cfg, nil)

	// Node: init — writes "phase" = "init" into state.
	require.NoError(t, wf.AddNode(&Node{
		ID: "init", Name: "Init", Type: NodeTypeAgent,
		Handler: stateWriteHandler("phase", "init"),
	}))

	// Node: process — writes "phase" = "processed".
	require.NoError(t, wf.AddNode(&Node{
		ID: "process", Name: "Process", Type: NodeTypeTool,
		Handler: stateWriteHandler("phase", "processed"),
	}))

	// Node: check — condition node, decides whether to retry or finish.
	require.NoError(t, wf.AddNode(&Node{
		ID: "check", Name: "Check", Type: NodeTypeCondition,
		Handler: func(_ context.Context, state *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			state.mu.RLock()
			phase := state.Variables["phase"]
			state.mu.RUnlock()
			return &NodeOutput{Result: phase}, nil
		},
	}))

	// Node: finalize — ends the workflow.
	require.NoError(t, wf.AddNode(&Node{
		ID: "finalize", Name: "Finalize", Type: NodeTypeAgent,
		Handler: func(_ context.Context, state *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			state.mu.Lock()
			state.Variables["phase"] = "finalized"
			state.mu.Unlock()
			return &NodeOutput{ShouldEnd: true, Result: "finalized"}, nil
		},
	}))

	require.NoError(t, wf.AddEdge("init", "process", nil, ""))
	require.NoError(t, wf.AddEdge("process", "check", nil, ""))

	// From check: if phase is "processed", go to finalize.
	isProcessed := func(state *WorkflowState) bool {
		state.mu.RLock()
		defer state.mu.RUnlock()
		return state.Variables["phase"] == "processed"
	}
	require.NoError(t, wf.AddEdge("check", "finalize", isProcessed, "done"))

	require.NoError(t, wf.SetEntryPoint("init"))

	state, err := wf.Execute(context.Background(), &NodeInput{
		Query:    "complex task",
		Messages: []Message{{Role: "user", Content: "start"}},
	})
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	assert.Equal(t, "finalized", state.Variables["phase"])
	// init → process → check → finalize = 4 nodes.
	assert.Len(t, state.History, 4)
	assert.Len(t, state.Messages, 1) // Input messages copied.
}

// ---------------------------------------------------------------------------
// pow (unexported helper)
// ---------------------------------------------------------------------------

func TestPow(t *testing.T) {
	tests := []struct {
		name     string
		base     float64
		exp      float64
		expected float64
	}{
		{"2^0", 2.0, 0, 1.0},
		{"2^1", 2.0, 1, 2.0},
		{"2^3", 2.0, 3, 8.0},
		{"1.5^2", 1.5, 2, 2.25},
		{"3^0", 3.0, 0, 1.0},
		{"1^10", 1.0, 10, 1.0},
		{"0^5", 0.0, 5, 0.0},
		{"2^negative_treated_as_zero", 2.0, -1, 1.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := pow(tc.base, tc.exp)
			assert.InDelta(t, tc.expected, result, 0.001)
		})
	}
}

// ---------------------------------------------------------------------------
// Concurrency safety
// ---------------------------------------------------------------------------

func TestWorkflow_ConcurrentAddNode(t *testing.T) {
	wf := NewWorkflow("concurrent", "", nil, nil)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = wf.AddNode(&Node{
				ID:   fmt.Sprintf("n%d", idx),
				Name: fmt.Sprintf("Node %d", idx),
			})
		}(i)
	}
	wg.Wait()
	assert.Len(t, wf.Graph.Nodes, 50)
}

func TestWorkflow_ConcurrentSetEntryPoint(t *testing.T) {
	wf := NewWorkflow("concurrent", "", nil, nil)
	for i := 0; i < 10; i++ {
		_ = wf.AddNode(&Node{ID: fmt.Sprintf("n%d", i)})
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = wf.SetEntryPoint(fmt.Sprintf("n%d", idx))
		}(i)
	}
	wg.Wait()
	// EntryPoint should be one of the valid nodes.
	assert.NotEmpty(t, wf.Graph.EntryPoint)
}

// ---------------------------------------------------------------------------
// NodeType string values
// ---------------------------------------------------------------------------

func TestNodeType_StringValues(t *testing.T) {
	assert.Equal(t, NodeType("agent"), NodeTypeAgent)
	assert.Equal(t, NodeType("tool"), NodeTypeTool)
	assert.Equal(t, NodeType("condition"), NodeTypeCondition)
	assert.Equal(t, NodeType("parallel"), NodeTypeParallel)
	assert.Equal(t, NodeType("human"), NodeTypeHuman)
	assert.Equal(t, NodeType("subgraph"), NodeTypeSubgraph)
}

// ---------------------------------------------------------------------------
// WorkflowStatus string values
// ---------------------------------------------------------------------------

func TestWorkflowStatus_StringValues(t *testing.T) {
	assert.Equal(t, WorkflowStatus("pending"), StatusPending)
	assert.Equal(t, WorkflowStatus("running"), StatusRunning)
	assert.Equal(t, WorkflowStatus("paused"), StatusPaused)
	assert.Equal(t, WorkflowStatus("completed"), StatusCompleted)
	assert.Equal(t, WorkflowStatus("failed"), StatusFailed)
}

// ---------------------------------------------------------------------------
// Struct field initialization / zero values
// ---------------------------------------------------------------------------

func TestNodeInput_ZeroValue(t *testing.T) {
	input := &NodeInput{}
	assert.Empty(t, input.Query)
	assert.Nil(t, input.Messages)
	assert.Nil(t, input.Tools)
	assert.Nil(t, input.Context)
	assert.Nil(t, input.Previous)
}

func TestNodeOutput_ZeroValue(t *testing.T) {
	output := &NodeOutput{}
	assert.Nil(t, output.Result)
	assert.Nil(t, output.Messages)
	assert.Nil(t, output.ToolCalls)
	assert.Empty(t, output.NextNode)
	assert.False(t, output.ShouldEnd)
	assert.Nil(t, output.Error)
	assert.Nil(t, output.Metadata)
}

func TestMessage_Fields(t *testing.T) {
	msg := Message{
		Role:    "assistant",
		Content: "hello",
		Name:    "bot",
		ToolCalls: []ToolCall{
			{ID: "t1", Name: "search"},
		},
	}
	assert.Equal(t, "assistant", msg.Role)
	assert.Equal(t, "hello", msg.Content)
	assert.Equal(t, "bot", msg.Name)
	assert.Len(t, msg.ToolCalls, 1)
}

func TestTool_Fields(t *testing.T) {
	called := false
	tool := Tool{
		Name:        "calculator",
		Description: "Math tool",
		Parameters:  map[string]interface{}{"expr": "string"},
		Handler: func(_ context.Context, args map[string]interface{}) (interface{}, error) {
			called = true
			return args["expr"], nil
		},
	}
	assert.Equal(t, "calculator", tool.Name)
	result, err := tool.Handler(context.Background(), map[string]interface{}{"expr": "2+2"})
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, "2+2", result)
}

func TestToolCall_Fields(t *testing.T) {
	tc := ToolCall{
		ID:        "tc-1",
		Name:      "search",
		Arguments: map[string]interface{}{"query": "test"},
		Result:    []string{"result1"},
	}
	assert.Equal(t, "tc-1", tc.ID)
	assert.Equal(t, "search", tc.Name)
	assert.Equal(t, "test", tc.Arguments["query"])
}

func TestNodeExecution_Fields(t *testing.T) {
	now := time.Now()
	ne := NodeExecution{
		NodeID:    "n1",
		NodeName:  "TestNode",
		StartTime: now,
		EndTime:   now.Add(time.Second),
		Error:     errors.New("test error"),
	}
	assert.Equal(t, "n1", ne.NodeID)
	assert.Equal(t, "TestNode", ne.NodeName)
	assert.Equal(t, time.Second, ne.EndTime.Sub(ne.StartTime))
	assert.NotNil(t, ne.Error)
}

func TestCheckpoint_Fields(t *testing.T) {
	now := time.Now()
	cp := Checkpoint{
		ID:        "cp-1",
		NodeID:    "n1",
		State:     map[string]interface{}{"x": 42},
		Timestamp: now,
	}
	assert.Equal(t, "cp-1", cp.ID)
	assert.Equal(t, "n1", cp.NodeID)
	assert.Equal(t, 42, cp.State["x"])
	assert.Equal(t, now, cp.Timestamp)
}

func TestRetryPolicy_Fields(t *testing.T) {
	rp := RetryPolicy{
		MaxRetries: 5,
		Delay:      200 * time.Millisecond,
		Backoff:    1.5,
	}
	assert.Equal(t, 5, rp.MaxRetries)
	assert.Equal(t, 200*time.Millisecond, rp.Delay)
	assert.Equal(t, 1.5, rp.Backoff)
}

func TestWorkflowConfig_Fields(t *testing.T) {
	cfg := WorkflowConfig{
		MaxIterations:        50,
		Timeout:              5 * time.Minute,
		EnableCheckpoints:    true,
		CheckpointInterval:   10,
		EnableSelfCorrection: false,
		MaxRetries:           2,
		RetryDelay:           500 * time.Millisecond,
	}
	assert.Equal(t, 50, cfg.MaxIterations)
	assert.Equal(t, 5*time.Minute, cfg.Timeout)
	assert.True(t, cfg.EnableCheckpoints)
	assert.Equal(t, 10, cfg.CheckpointInterval)
	assert.False(t, cfg.EnableSelfCorrection)
	assert.Equal(t, 2, cfg.MaxRetries)
	assert.Equal(t, 500*time.Millisecond, cfg.RetryDelay)
}

// ---------------------------------------------------------------------------
// Execute — NodeOutput.Error field (distinct from handler error return)
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_OutputErrorField(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{
		ID: "a", Name: "A",
		Handler: func(_ context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			return &NodeOutput{
				Error:     errors.New("soft error"),
				ShouldEnd: true,
			}, nil // No hard error — workflow continues.
		},
	}))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err) // Handler did not return error.
	assert.Equal(t, StatusCompleted, state.Status)
	assert.NotNil(t, state.History[0].Output.Error)
}

// ---------------------------------------------------------------------------
// Execute — large workflow (stress-like)
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_LargeLinearChain(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large chain test in short mode")
	}
	var counter atomic.Int64
	wf := buildLinearWorkflow(t, 50, countingHandler(&counter))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	assert.Equal(t, int64(50), counter.Load())
	assert.Len(t, state.History, 50)
}

// ---------------------------------------------------------------------------
// Execute — self-loop with MaxIterations guard
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_SelfLoopMaxIterations(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 5,
		Timeout:       10 * time.Second,
		MaxRetries:    0,
		RetryDelay:    0,
	}
	wf := NewWorkflow("loop", "", cfg, nil)

	var counter atomic.Int64
	require.NoError(t, wf.AddNode(&Node{
		ID: "loop", Name: "Loop",
		Handler: countingHandler(&counter),
	}))
	require.NoError(t, wf.AddEdge("loop", "loop", nil, "self"))
	require.NoError(t, wf.SetEntryPoint("loop"))

	state, err := wf.Execute(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max iterations reached: 5")
	assert.Equal(t, int64(5), counter.Load())
	assert.Equal(t, StatusFailed, state.Status)
}

// ---------------------------------------------------------------------------
// Execute — conditional loop that eventually exits
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_ConditionalLoopExits(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 100,
		Timeout:       10 * time.Second,
		MaxRetries:    0,
		RetryDelay:    0,
	}
	wf := NewWorkflow("loop-exit", "", cfg, nil)

	var counter atomic.Int64
	require.NoError(t, wf.AddNode(&Node{
		ID: "worker", Name: "Worker",
		Handler: func(_ context.Context, state *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			c := counter.Add(1)
			state.mu.Lock()
			state.Variables["count"] = c
			state.mu.Unlock()
			return &NodeOutput{Result: c}, nil
		},
	}))
	require.NoError(t, wf.AddNode(&Node{
		ID: "done", Name: "Done", Handler: shouldEndHandler(),
	}))

	notDone := func(state *WorkflowState) bool {
		state.mu.RLock()
		defer state.mu.RUnlock()
		v, ok := state.Variables["count"]
		if !ok {
			return true
		}
		return v.(int64) < 5
	}
	isDone := func(state *WorkflowState) bool {
		return !notDone(state)
	}

	require.NoError(t, wf.AddEdge("worker", "worker", notDone, "loop"))
	require.NoError(t, wf.AddEdge("worker", "done", isDone, "exit"))
	require.NoError(t, wf.SetEntryPoint("worker"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	assert.Equal(t, int64(5), counter.Load())
	// 5 worker iterations + 1 done = 6 history entries.
	assert.Len(t, state.History, 6)
}

// ---------------------------------------------------------------------------
// Execute — metadata propagation in NodeOutput
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_MetadataPropagation(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	require.NoError(t, wf.AddNode(&Node{
		ID: "meta-producer", Name: "MetaProducer",
		Handler: func(_ context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			return &NodeOutput{
				Result:   "with-metadata",
				Metadata: map[string]interface{}{"tokens": 42, "model": "gpt-4"},
			}, nil
		},
	}))
	require.NoError(t, wf.AddNode(&Node{
		ID: "meta-consumer", Name: "MetaConsumer",
		Handler: func(_ context.Context, _ *WorkflowState, input *NodeInput) (*NodeOutput, error) {
			if input == nil || input.Previous == nil || input.Previous.Metadata == nil {
				return nil, fmt.Errorf("metadata not propagated")
			}
			tokens := input.Previous.Metadata["tokens"]
			if tokens != 42 {
				return nil, fmt.Errorf("wrong tokens: %v", tokens)
			}
			return &NodeOutput{ShouldEnd: true}, nil
		},
	}))

	require.NoError(t, wf.AddEdge("meta-producer", "meta-consumer", nil, ""))
	require.NoError(t, wf.SetEntryPoint("meta-producer"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
}

// ---------------------------------------------------------------------------
// Execute — InputContext passed to second node includes state variables
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_InputContextFromStateVariables(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	require.NoError(t, wf.AddNode(&Node{
		ID: "writer", Name: "Writer",
		Handler: stateWriteHandler("shared_key", "shared_value"),
	}))
	require.NoError(t, wf.AddNode(&Node{
		ID: "reader", Name: "Reader",
		Handler: func(_ context.Context, _ *WorkflowState, input *NodeInput) (*NodeOutput, error) {
			if input == nil || input.Context == nil {
				return nil, fmt.Errorf("context not set")
			}
			v, ok := input.Context["shared_key"]
			if !ok || v != "shared_value" {
				return nil, fmt.Errorf("expected shared_key=shared_value in context, got %v", v)
			}
			return &NodeOutput{ShouldEnd: true}, nil
		},
	}))

	require.NoError(t, wf.AddEdge("writer", "reader", nil, ""))
	require.NoError(t, wf.SetEntryPoint("writer"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
}

// ---------------------------------------------------------------------------
// Execute — node with messages in output
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_NodeOutputMessages(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	require.NoError(t, wf.AddNode(&Node{
		ID: "msg-node", Name: "MsgNode",
		Handler: func(_ context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			return &NodeOutput{
				Messages: []Message{
					{Role: "assistant", Content: "I processed your request"},
				},
				ShouldEnd: true,
			}, nil
		},
	}))
	require.NoError(t, wf.SetEntryPoint("msg-node"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	require.Len(t, state.History, 1)
	assert.Len(t, state.History[0].Output.Messages, 1)
	assert.Equal(t, "I processed your request", state.History[0].Output.Messages[0].Content)
}

// ---------------------------------------------------------------------------
// Execute — tools in NodeInput
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_ToolsInInput(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	require.NoError(t, wf.AddNode(&Node{
		ID: "tool-user", Name: "ToolUser",
		Handler: func(_ context.Context, _ *WorkflowState, input *NodeInput) (*NodeOutput, error) {
			if input == nil || len(input.Tools) == 0 {
				return nil, fmt.Errorf("expected tools in input")
			}
			// Execute the tool.
			result, err := input.Tools[0].Handler(context.Background(), map[string]interface{}{"x": 1})
			if err != nil {
				return nil, err
			}
			return &NodeOutput{Result: result, ShouldEnd: true}, nil
		},
	}))
	require.NoError(t, wf.SetEntryPoint("tool-user"))

	input := &NodeInput{
		Tools: []Tool{
			{
				Name:        "add",
				Description: "Adds one",
				Handler: func(_ context.Context, args map[string]interface{}) (interface{}, error) {
					x := args["x"].(int)
					return x + 1, nil
				},
			},
		},
	}

	state, err := wf.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	assert.Equal(t, 2, state.History[0].Output.Result)
}

// ---------------------------------------------------------------------------
// Execute — workflow with no edges, only entry + end on same node (no handler)
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_NoEdgesNilHandlerEndNode(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "only"}))
	require.NoError(t, wf.SetEntryPoint("only"))
	require.NoError(t, wf.AddEndNode("only"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
}

// ---------------------------------------------------------------------------
// Execute — verify workflow ID is set in state
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_StateWorkflowID(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a", Handler: shouldEndHandler()}))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, wf.ID, state.WorkflowID)
	assert.NotEmpty(t, state.ID)
	assert.NotEqual(t, wf.ID, state.ID, "state ID should differ from workflow ID")
}

// ---------------------------------------------------------------------------
// Execute — handler receives correct input on first node
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_FirstNodeReceivesOriginalInput(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	var receivedQuery string
	require.NoError(t, wf.AddNode(&Node{
		ID: "first", Name: "First",
		Handler: func(_ context.Context, _ *WorkflowState, input *NodeInput) (*NodeOutput, error) {
			if input != nil {
				receivedQuery = input.Query
			}
			return &NodeOutput{ShouldEnd: true}, nil
		},
	}))
	require.NoError(t, wf.SetEntryPoint("first"))

	_, err := wf.Execute(context.Background(), &NodeInput{Query: "original-query"})
	require.NoError(t, err)
	assert.Equal(t, "original-query", receivedQuery)
}

// ---------------------------------------------------------------------------
// Execute — edge condition evaluation receives correct state
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_EdgeConditionReceivesCurrentState(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations: 100,
		Timeout:       10 * time.Second,
		MaxRetries:    0,
		RetryDelay:    0,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	require.NoError(t, wf.AddNode(&Node{
		ID: "a", Name: "A",
		Handler: stateWriteHandler("routed", true),
	}))
	require.NoError(t, wf.AddNode(&Node{
		ID: "b", Name: "B", Handler: shouldEndHandler(),
	}))

	var conditionState *WorkflowState
	cond := func(s *WorkflowState) bool {
		conditionState = s
		s.mu.RLock()
		defer s.mu.RUnlock()
		v, ok := s.Variables["routed"]
		return ok && v == true
	}

	require.NoError(t, wf.AddEdge("a", "b", cond, ""))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	assert.NotNil(t, conditionState)
}

// ---------------------------------------------------------------------------
// Execute — end-to-end with all features combined
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_EndToEnd_AllFeatures(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations:      50,
		Timeout:            10 * time.Second,
		EnableCheckpoints:  true,
		CheckpointInterval: 1,
		MaxRetries:         0,
		RetryDelay:         0,
	}
	wf := NewWorkflow("e2e", "End to end test workflow", cfg, nil)

	// Agent node: init.
	require.NoError(t, wf.AddNode(&Node{
		ID: "init", Name: "Init", Type: NodeTypeAgent,
		Handler: func(_ context.Context, state *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			state.mu.Lock()
			state.Variables["step"] = 0
			state.mu.Unlock()
			return &NodeOutput{Result: "initialized"}, nil
		},
	}))

	// Tool node: fetch data.
	require.NoError(t, wf.AddNode(&Node{
		ID: "fetch", Name: "Fetch", Type: NodeTypeTool,
		Handler: func(_ context.Context, state *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			state.mu.Lock()
			state.Variables["step"] = 1
			state.Variables["data"] = "fetched"
			state.mu.Unlock()
			return &NodeOutput{
				Result: "data-fetched",
				ToolCalls: []ToolCall{
					{ID: "tc1", Name: "http_get", Arguments: map[string]interface{}{"url": "http://example.com"}},
				},
			}, nil
		},
	}))

	// Condition node: decide next step.
	require.NoError(t, wf.AddNode(&Node{
		ID: "decide", Name: "Decide", Type: NodeTypeCondition,
		Handler: func(_ context.Context, state *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			state.mu.Lock()
			state.Variables["step"] = 2
			state.mu.Unlock()
			return &NodeOutput{Result: "decided"}, nil
		},
	}))

	// End node: finish.
	require.NoError(t, wf.AddNode(&Node{
		ID: "finish", Name: "Finish", Type: NodeTypeAgent,
		Handler: func(_ context.Context, state *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			state.mu.Lock()
			state.Variables["step"] = 3
			state.Variables["status"] = "complete"
			state.mu.Unlock()
			return &NodeOutput{Result: "finished"}, nil
		},
	}))

	require.NoError(t, wf.AddEdge("init", "fetch", nil, ""))
	require.NoError(t, wf.AddEdge("fetch", "decide", nil, ""))

	hasFetchedData := func(state *WorkflowState) bool {
		state.mu.RLock()
		defer state.mu.RUnlock()
		return state.Variables["data"] == "fetched"
	}
	require.NoError(t, wf.AddEdge("decide", "finish", hasFetchedData, "has-data"))

	require.NoError(t, wf.SetEntryPoint("init"))
	require.NoError(t, wf.AddEndNode("finish"))

	state, err := wf.Execute(context.Background(), &NodeInput{
		Query:    "run e2e",
		Messages: []Message{{Role: "user", Content: "execute e2e test"}},
		Context:  map[string]interface{}{"env": "test"},
	})

	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, state.Status)
	assert.Equal(t, wf.ID, state.WorkflowID)
	assert.NotNil(t, state.EndTime)
	assert.Nil(t, state.Error)

	// Verify history: init → fetch → decide → finish.
	require.Len(t, state.History, 4)
	assert.Equal(t, "init", state.History[0].NodeID)
	assert.Equal(t, "fetch", state.History[1].NodeID)
	assert.Equal(t, "decide", state.History[2].NodeID)
	assert.Equal(t, "finish", state.History[3].NodeID)

	// Verify state variables.
	assert.Equal(t, 3, state.Variables["step"])
	assert.Equal(t, "complete", state.Variables["status"])
	assert.Equal(t, "fetched", state.Variables["data"])

	// Verify checkpoints were created.
	assert.Greater(t, len(state.Checkpoints), 0)

	// Verify input messages copied.
	assert.Len(t, state.Messages, 1)
	assert.Equal(t, "execute e2e test", state.Messages[0].Content)
}

// ---------------------------------------------------------------------------
// Execute — empty messages in input (non-nil but empty slice)
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_EmptyMessagesSlice(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a", Handler: shouldEndHandler()}))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), &NodeInput{Messages: []Message{}})
	require.NoError(t, err)
	assert.Empty(t, state.Messages)
}

// ---------------------------------------------------------------------------
// Execute — nil messages in input
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_NilMessagesInInput(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a", Handler: shouldEndHandler()}))
	require.NoError(t, wf.SetEntryPoint("a"))

	state, err := wf.Execute(context.Background(), &NodeInput{Messages: nil})
	require.NoError(t, err)
	assert.Empty(t, state.Messages)
}

// ---------------------------------------------------------------------------
// Execute — checkpoint interval larger than total iterations
// ---------------------------------------------------------------------------

func TestWorkflow_Execute_CheckpointIntervalLargerThanIterations(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxIterations:      100,
		Timeout:            10 * time.Second,
		EnableCheckpoints:  true,
		CheckpointInterval: 999,
		MaxRetries:         0,
		RetryDelay:         0,
	}
	wf := buildLinearWorkflow(t, 3, echoHandler())
	wf.Config = cfg

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	// Checkpoint interval 999 means no checkpoints in 3 iterations.
	assert.Empty(t, state.Checkpoints)
}

// ---------------------------------------------------------------------------
// getNextNode — no edges from current node
// ---------------------------------------------------------------------------

func TestWorkflow_GetNextNode_NoEdges(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "isolated"}))

	state := &WorkflowState{CurrentNode: "isolated"}
	next := wf.getNextNode(state)
	assert.Empty(t, next)
}

func TestWorkflow_GetNextNode_AllConditionsFalse(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a"}))
	require.NoError(t, wf.AddNode(&Node{ID: "b"}))
	require.NoError(t, wf.AddNode(&Node{ID: "c"}))

	never := func(_ *WorkflowState) bool { return false }
	require.NoError(t, wf.AddEdge("a", "b", never, ""))
	require.NoError(t, wf.AddEdge("a", "c", never, ""))

	state := &WorkflowState{CurrentNode: "a"}
	next := wf.getNextNode(state)
	assert.Empty(t, next)
}

func TestWorkflow_GetNextNode_UnconditionalEdge(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a"}))
	require.NoError(t, wf.AddNode(&Node{ID: "b"}))
	require.NoError(t, wf.AddEdge("a", "b", nil, ""))

	state := &WorkflowState{CurrentNode: "a"}
	next := wf.getNextNode(state)
	assert.Equal(t, "b", next)
}

func TestWorkflow_GetNextNode_ConditionalTrueEdge(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a"}))
	require.NoError(t, wf.AddNode(&Node{ID: "b"}))
	require.NoError(t, wf.AddNode(&Node{ID: "c"}))

	// First edge condition false, second true.
	require.NoError(t, wf.AddEdge("a", "b", func(_ *WorkflowState) bool { return false }, ""))
	require.NoError(t, wf.AddEdge("a", "c", func(_ *WorkflowState) bool { return true }, ""))

	state := &WorkflowState{CurrentNode: "a"}
	next := wf.getNextNode(state)
	assert.Equal(t, "c", next)
}

func TestWorkflow_GetNextNode_WrongFromNode(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)
	require.NoError(t, wf.AddNode(&Node{ID: "a"}))
	require.NoError(t, wf.AddNode(&Node{ID: "b"}))
	require.NoError(t, wf.AddEdge("a", "b", nil, ""))

	// Current node is "b" — the edge goes from "a", not "b".
	state := &WorkflowState{CurrentNode: "b"}
	next := wf.getNextNode(state)
	assert.Empty(t, next)
}

// ---------------------------------------------------------------------------
// createCheckpoint — direct invocation
// ---------------------------------------------------------------------------

func TestWorkflow_CreateCheckpoint_CopiesVariables(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	state := &WorkflowState{
		CurrentNode: "n1",
		Variables:   map[string]interface{}{"a": 1, "b": "two"},
		Checkpoints: make([]Checkpoint, 0),
	}

	wf.createCheckpoint(state)
	require.Len(t, state.Checkpoints, 1)

	cp := state.Checkpoints[0]
	assert.NotEmpty(t, cp.ID)
	assert.Equal(t, "n1", cp.NodeID)
	assert.Equal(t, 1, cp.State["a"])
	assert.Equal(t, "two", cp.State["b"])

	// Modify original — checkpoint should not change.
	state.Variables["a"] = 999
	assert.Equal(t, 1, cp.State["a"])
}

func TestWorkflow_CreateCheckpoint_EmptyVariables(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	state := &WorkflowState{
		CurrentNode: "n1",
		Variables:   map[string]interface{}{},
		Checkpoints: make([]Checkpoint, 0),
	}

	wf.createCheckpoint(state)
	require.Len(t, state.Checkpoints, 1)
	assert.Empty(t, state.Checkpoints[0].State)
}

func TestWorkflow_CreateCheckpoint_MultipleCheckpoints(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	state := &WorkflowState{
		CurrentNode: "n1",
		Variables:   map[string]interface{}{"step": 1},
		Checkpoints: make([]Checkpoint, 0),
	}

	wf.createCheckpoint(state)
	state.CurrentNode = "n2"
	state.Variables["step"] = 2
	wf.createCheckpoint(state)
	state.CurrentNode = "n3"
	state.Variables["step"] = 3
	wf.createCheckpoint(state)

	require.Len(t, state.Checkpoints, 3)
	assert.Equal(t, "n1", state.Checkpoints[0].NodeID)
	assert.Equal(t, 1, state.Checkpoints[0].State["step"])
	assert.Equal(t, "n2", state.Checkpoints[1].NodeID)
	assert.Equal(t, 2, state.Checkpoints[1].State["step"])
	assert.Equal(t, "n3", state.Checkpoints[2].NodeID)
	assert.Equal(t, 3, state.Checkpoints[2].State["step"])
}

// ---------------------------------------------------------------------------
// executeNode — direct invocation (nil handler)
// ---------------------------------------------------------------------------

func TestWorkflow_ExecuteNode_NilHandler(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	node := &Node{ID: "n", Name: "N"}
	state := &WorkflowState{}
	output, err := wf.executeNode(context.Background(), node, state, nil)
	require.NoError(t, err)
	require.NotNil(t, output)
	// Empty output when handler is nil.
	assert.Nil(t, output.Result)
	assert.False(t, output.ShouldEnd)
}

func TestWorkflow_ExecuteNode_HandlerSuccess(t *testing.T) {
	wf := NewWorkflow("test", "", nil, nil)

	node := &Node{
		ID: "n", Name: "N",
		Handler: func(_ context.Context, _ *WorkflowState, _ *NodeInput) (*NodeOutput, error) {
			return &NodeOutput{Result: "ok"}, nil
		},
	}

	state := &WorkflowState{}
	output, err := wf.executeNode(context.Background(), node, state, nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", output.Result)
}

func TestWorkflow_ExecuteNode_HandlerError_NoRetry(t *testing.T) {
	cfg := &WorkflowConfig{MaxRetries: 0, RetryDelay: 0}
	wf := NewWorkflow("test", "", cfg, nil)

	node := &Node{
		ID: "n", Name: "N",
		Handler: failingHandler("fail"),
	}

	state := &WorkflowState{}
	output, err := wf.executeNode(context.Background(), node, state, nil)
	require.Error(t, err)
	assert.Nil(t, output)
}

func TestWorkflow_ExecuteNode_HandlerError_WithRetry(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxRetries: 3,
		RetryDelay: 1 * time.Millisecond,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	node := &Node{
		ID: "n", Name: "N",
		Handler: failNTimesHandler(2),
	}

	state := &WorkflowState{}
	output, err := wf.executeNode(context.Background(), node, state, nil)
	require.NoError(t, err)
	assert.Equal(t, "recovered", output.Result)
}

func TestWorkflow_ExecuteNode_NodeRetryOverridesGlobal(t *testing.T) {
	cfg := &WorkflowConfig{
		MaxRetries: 0, // Global: no retries.
		RetryDelay: 0,
	}
	wf := NewWorkflow("test", "", cfg, nil)

	node := &Node{
		ID: "n", Name: "N",
		Handler: failNTimesHandler(1),
		RetryPolicy: &RetryPolicy{
			MaxRetries: 2,
			Delay:      1 * time.Millisecond,
			Backoff:    1.0,
		},
	}

	state := &WorkflowState{}
	output, err := wf.executeNode(context.Background(), node, state, nil)
	require.NoError(t, err)
	assert.Equal(t, "recovered", output.Result)
}
