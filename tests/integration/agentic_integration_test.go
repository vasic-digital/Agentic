package integration

import (
	"context"
	"testing"

	"digital.vasic.agentic/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowLinearExecution_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	wf := agentic.NewWorkflow("linear", "Linear workflow", nil, nil)

	visited := make([]string, 0)
	handler := func(name string) agentic.NodeHandler {
		return func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
			visited = append(visited, name)
			state.Variables[name] = true
			return &agentic.NodeOutput{Result: name + " done"}, nil
		}
	}

	require.NoError(t, wf.AddNode(&agentic.Node{ID: "a", Name: "StepA", Type: agentic.NodeTypeAgent, Handler: handler("a")}))
	require.NoError(t, wf.AddNode(&agentic.Node{ID: "b", Name: "StepB", Type: agentic.NodeTypeTool, Handler: handler("b")}))
	require.NoError(t, wf.AddNode(&agentic.Node{ID: "c", Name: "StepC", Type: agentic.NodeTypeAgent, Handler: handler("c")}))

	require.NoError(t, wf.AddEdge("a", "b", nil, "a->b"))
	require.NoError(t, wf.AddEdge("b", "c", nil, "b->c"))
	require.NoError(t, wf.SetEntryPoint("a"))
	require.NoError(t, wf.AddEndNode("c"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, agentic.StatusCompleted, state.Status)
	assert.Contains(t, visited, "a")
	assert.Contains(t, visited, "b")
	assert.Contains(t, visited, "c")
	assert.True(t, state.Variables["a"].(bool))
	assert.True(t, state.Variables["b"].(bool))
	assert.True(t, state.Variables["c"].(bool))
}

func TestWorkflowConditionalBranching_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	wf := agentic.NewWorkflow("cond", "Conditional workflow", nil, nil)
	var path string

	startHandler := func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
		state.Variables["score"] = 85
		return &agentic.NodeOutput{}, nil
	}
	passHandler := func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
		path = "pass"
		return &agentic.NodeOutput{ShouldEnd: true}, nil
	}
	failHandler := func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
		path = "fail"
		return &agentic.NodeOutput{ShouldEnd: true}, nil
	}

	require.NoError(t, wf.AddNode(&agentic.Node{ID: "start", Name: "Start", Type: agentic.NodeTypeAgent, Handler: startHandler}))
	require.NoError(t, wf.AddNode(&agentic.Node{ID: "pass", Name: "Pass", Type: agentic.NodeTypeAgent, Handler: passHandler}))
	require.NoError(t, wf.AddNode(&agentic.Node{ID: "fail", Name: "Fail", Type: agentic.NodeTypeAgent, Handler: failHandler}))

	highScore := func(state *agentic.WorkflowState) bool {
		if s, ok := state.Variables["score"].(int); ok {
			return s >= 70
		}
		return false
	}
	lowScore := func(state *agentic.WorkflowState) bool {
		if s, ok := state.Variables["score"].(int); ok {
			return s < 70
		}
		return true
	}

	require.NoError(t, wf.AddEdge("start", "pass", highScore, "high"))
	require.NoError(t, wf.AddEdge("start", "fail", lowScore, "low"))
	require.NoError(t, wf.SetEntryPoint("start"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, agentic.StatusCompleted, state.Status)
	assert.Equal(t, "pass", path)
}

func TestWorkflowNoEntryPointError_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	wf := agentic.NewWorkflow("empty", "No entry", nil, nil)
	require.NoError(t, wf.AddNode(&agentic.Node{ID: "a", Name: "A", Type: agentic.NodeTypeAgent}))

	_, err := wf.Execute(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entry point")
}

func TestWorkflowEdgeValidation_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	wf := agentic.NewWorkflow("edge-test", "Edge validation", nil, nil)
	require.NoError(t, wf.AddNode(&agentic.Node{ID: "a", Name: "A", Type: agentic.NodeTypeAgent}))

	// Edge from nonexistent source
	err := wf.AddEdge("nonexistent", "a", nil, "bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Edge to nonexistent target
	err = wf.AddEdge("a", "nonexistent", nil, "bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWorkflowHistoryTracking_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	wf := agentic.NewWorkflow("history", "History tracking", nil, nil)

	handler := func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
		return &agentic.NodeOutput{Result: "ok"}, nil
	}

	require.NoError(t, wf.AddNode(&agentic.Node{ID: "step1", Name: "Step1", Type: agentic.NodeTypeAgent, Handler: handler}))
	require.NoError(t, wf.AddNode(&agentic.Node{ID: "step2", Name: "Step2", Type: agentic.NodeTypeAgent, Handler: handler}))
	require.NoError(t, wf.AddEdge("step1", "step2", nil, "next"))
	require.NoError(t, wf.SetEntryPoint("step1"))
	require.NoError(t, wf.AddEndNode("step2"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 2, len(state.History))
	assert.Equal(t, "step1", state.History[0].NodeID)
	assert.Equal(t, "step2", state.History[1].NodeID)
	assert.NotNil(t, state.EndTime)
}
