package e2e

import (
	"context"
	"testing"
	"time"

	"digital.vasic.agentic/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFullAgentWorkflowPipeline_E2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := agentic.DefaultWorkflowConfig()
	cfg.Timeout = 10 * time.Second
	cfg.EnableCheckpoints = true
	cfg.CheckpointInterval = 1

	wf := agentic.NewWorkflow("agent-pipeline", "Full agent pipeline", cfg, nil)

	// Plan -> Execute -> Review -> Complete
	planHandler := func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
		state.Variables["plan"] = "implement feature X"
		return &agentic.NodeOutput{Result: "plan created"}, nil
	}
	execHandler := func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
		state.Variables["executed"] = true
		return &agentic.NodeOutput{Result: "code written"}, nil
	}
	reviewHandler := func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
		state.Variables["reviewed"] = true
		state.Variables["quality"] = 0.95
		return &agentic.NodeOutput{Result: "review passed"}, nil
	}

	require.NoError(t, wf.AddNode(&agentic.Node{ID: "plan", Name: "Plan", Type: agentic.NodeTypeAgent, Handler: planHandler}))
	require.NoError(t, wf.AddNode(&agentic.Node{ID: "exec", Name: "Execute", Type: agentic.NodeTypeTool, Handler: execHandler}))
	require.NoError(t, wf.AddNode(&agentic.Node{ID: "review", Name: "Review", Type: agentic.NodeTypeAgent, Handler: reviewHandler}))

	require.NoError(t, wf.AddEdge("plan", "exec", nil, "plan->exec"))
	require.NoError(t, wf.AddEdge("exec", "review", nil, "exec->review"))
	require.NoError(t, wf.SetEntryPoint("plan"))
	require.NoError(t, wf.AddEndNode("review"))

	state, err := wf.Execute(context.Background(), &agentic.NodeInput{
		Query: "Implement feature X",
	})
	require.NoError(t, err)
	assert.Equal(t, agentic.StatusCompleted, state.Status)
	assert.Equal(t, "implement feature X", state.Variables["plan"])
	assert.True(t, state.Variables["executed"].(bool))
	assert.True(t, state.Variables["reviewed"].(bool))
	assert.Equal(t, 3, len(state.History))
}

func TestWorkflowWithShouldEnd_E2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	wf := agentic.NewWorkflow("early-exit", "Early exit workflow", nil, nil)

	counter := 0
	handler := func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
		counter++
		return &agentic.NodeOutput{ShouldEnd: true, Result: "exiting early"}, nil
	}

	require.NoError(t, wf.AddNode(&agentic.Node{ID: "start", Name: "Start", Type: agentic.NodeTypeAgent, Handler: handler}))
	require.NoError(t, wf.AddNode(&agentic.Node{ID: "never", Name: "Never", Type: agentic.NodeTypeAgent, Handler: handler}))
	require.NoError(t, wf.AddEdge("start", "never", nil, "next"))
	require.NoError(t, wf.SetEntryPoint("start"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, agentic.StatusCompleted, state.Status)
	assert.Equal(t, 1, counter, "only the first node should execute")
}

func TestWorkflowDefaultConfig_E2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := agentic.DefaultWorkflowConfig()
	assert.Equal(t, 100, cfg.MaxIterations)
	assert.Equal(t, 30*time.Minute, cfg.Timeout)
	assert.True(t, cfg.EnableCheckpoints)
	assert.Equal(t, 5, cfg.CheckpointInterval)
	assert.True(t, cfg.EnableSelfCorrection)
	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 1*time.Second, cfg.RetryDelay)
}

func TestWorkflowCheckpointCreation_E2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	cfg := agentic.DefaultWorkflowConfig()
	cfg.EnableCheckpoints = true
	cfg.CheckpointInterval = 1
	cfg.Timeout = 10 * time.Second

	wf := agentic.NewWorkflow("checkpoint-test", "Checkpoint testing", cfg, nil)

	handler := func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
		state.Variables["step"] = state.CurrentNode
		return &agentic.NodeOutput{}, nil
	}

	require.NoError(t, wf.AddNode(&agentic.Node{ID: "a", Name: "A", Type: agentic.NodeTypeAgent, Handler: handler}))
	require.NoError(t, wf.AddNode(&agentic.Node{ID: "b", Name: "B", Type: agentic.NodeTypeAgent, Handler: handler}))
	require.NoError(t, wf.AddNode(&agentic.Node{ID: "c", Name: "C", Type: agentic.NodeTypeAgent, Handler: handler}))
	require.NoError(t, wf.AddEdge("a", "b", nil, ""))
	require.NoError(t, wf.AddEdge("b", "c", nil, ""))
	require.NoError(t, wf.SetEntryPoint("a"))
	require.NoError(t, wf.AddEndNode("c"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, agentic.StatusCompleted, state.Status)
	// Checkpoints may be created during execution
	assert.GreaterOrEqual(t, len(state.History), 3)
}

func TestWorkflowNodeTypeVariety_E2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	nodeTypes := []agentic.NodeType{
		agentic.NodeTypeAgent,
		agentic.NodeTypeTool,
		agentic.NodeTypeCondition,
		agentic.NodeTypeParallel,
		agentic.NodeTypeHuman,
		agentic.NodeTypeSubgraph,
	}

	assert.Equal(t, agentic.NodeType("agent"), agentic.NodeTypeAgent)
	assert.Equal(t, agentic.NodeType("tool"), agentic.NodeTypeTool)
	assert.Equal(t, agentic.NodeType("condition"), agentic.NodeTypeCondition)
	assert.Equal(t, agentic.NodeType("parallel"), agentic.NodeTypeParallel)
	assert.Equal(t, agentic.NodeType("human"), agentic.NodeTypeHuman)
	assert.Equal(t, agentic.NodeType("subgraph"), agentic.NodeTypeSubgraph)
	assert.Equal(t, 6, len(nodeTypes))
}
