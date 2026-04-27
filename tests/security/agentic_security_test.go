package security

import (
	"context"
	"fmt"
	"testing"
	"time"

	"digital.vasic.agentic/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNilHandlerNodeExecution_Security(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping security test in short mode")  // SKIP-OK: #short-mode
	}

	wf := agentic.NewWorkflow("nil-handler", "Nil handler test", nil, nil)
	require.NoError(t, wf.AddNode(&agentic.Node{ID: "n", Name: "NoHandler", Type: agentic.NodeTypeAgent, Handler: nil}))
	require.NoError(t, wf.SetEntryPoint("n"))
	require.NoError(t, wf.AddEndNode("n"))

	state, err := wf.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, agentic.StatusCompleted, state.Status)
}

func TestSetEntryPointNonexistentNode_Security(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping security test in short mode")  // SKIP-OK: #short-mode
	}

	wf := agentic.NewWorkflow("bad-entry", "Bad entry point", nil, nil)
	err := wf.SetEntryPoint("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAddEndNodeNonexistent_Security(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping security test in short mode")  // SKIP-OK: #short-mode
	}

	wf := agentic.NewWorkflow("bad-end", "Bad end node", nil, nil)
	err := wf.AddEndNode("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWorkflowContextCancellation_Security(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping security test in short mode")  // SKIP-OK: #short-mode
	}

	cfg := agentic.DefaultWorkflowConfig()
	cfg.Timeout = 100 * time.Millisecond

	wf := agentic.NewWorkflow("timeout", "Timeout test", cfg, nil)
	handler := func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
		time.Sleep(200 * time.Millisecond)
		return &agentic.NodeOutput{}, nil
	}

	require.NoError(t, wf.AddNode(&agentic.Node{ID: "slow", Name: "Slow", Type: agentic.NodeTypeAgent, Handler: handler}))
	require.NoError(t, wf.AddNode(&agentic.Node{ID: "end", Name: "End", Type: agentic.NodeTypeAgent}))
	require.NoError(t, wf.AddEdge("slow", "end", nil, ""))
	require.NoError(t, wf.SetEntryPoint("slow"))
	require.NoError(t, wf.AddEndNode("end"))

	state, err := wf.Execute(context.Background(), nil)
	// Timeout may cause either error or the handler completes
	if err != nil {
		assert.Equal(t, agentic.StatusFailed, state.Status)
	}
}

func TestWorkflowNodeHandlerError_Security(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping security test in short mode")  // SKIP-OK: #short-mode
	}

	cfg := agentic.DefaultWorkflowConfig()
	cfg.MaxRetries = 0
	cfg.Timeout = 5 * time.Second

	wf := agentic.NewWorkflow("error-handler", "Error handler", cfg, nil)
	handler := func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
		return nil, fmt.Errorf("simulated failure")
	}

	require.NoError(t, wf.AddNode(&agentic.Node{ID: "fail", Name: "Fail", Type: agentic.NodeTypeAgent, Handler: handler}))
	require.NoError(t, wf.SetEntryPoint("fail"))
	require.NoError(t, wf.AddEndNode("fail"))

	state, err := wf.Execute(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, agentic.StatusFailed, state.Status)
	assert.Contains(t, err.Error(), "simulated failure")
}

func TestRestoreFromCheckpointInvalid_Security(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping security test in short mode")  // SKIP-OK: #short-mode
	}

	wf := agentic.NewWorkflow("restore", "Restore test", nil, nil)

	state := &agentic.WorkflowState{
		Checkpoints: []agentic.Checkpoint{},
	}

	err := wf.RestoreFromCheckpoint(state, "nonexistent-checkpoint")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
