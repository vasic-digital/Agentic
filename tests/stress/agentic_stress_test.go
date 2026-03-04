package stress

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"digital.vasic.agentic/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	runtime.GOMAXPROCS(2)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func echoHandler() agentic.NodeHandler {
	return func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
		result := ""
		if input != nil {
			result = input.Query
		}
		return &agentic.NodeOutput{Result: result}, nil
	}
}

func buildLinearWorkflow(name string) *agentic.Workflow {
	cfg := agentic.DefaultWorkflowConfig()
	cfg.Timeout = 10 * time.Second

	wf := agentic.NewWorkflow(name, "stress test workflow", cfg, nil)
	_ = wf.AddNode(&agentic.Node{
		ID: "start", Name: "Start", Type: agentic.NodeTypeAgent, Handler: echoHandler(),
	})
	_ = wf.AddNode(&agentic.Node{
		ID: "end", Name: "End", Type: agentic.NodeTypeAgent, Handler: echoHandler(),
	})
	_ = wf.AddEdge("start", "end", nil, "start->end")
	_ = wf.SetEntryPoint("start")
	_ = wf.AddEndNode("end")
	return wf
}

// ---------------------------------------------------------------------------
// Stress Tests
// ---------------------------------------------------------------------------

func TestConcurrentWorkflowExecution_Stress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	const goroutines = 75

	var wg sync.WaitGroup
	var successCount atomic.Int64
	var errorCount atomic.Int64

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()

			wf := buildLinearWorkflow(fmt.Sprintf("concurrent-%d", idx))
			state, err := wf.Execute(context.Background(), &agentic.NodeInput{
				Query: fmt.Sprintf("query-%d", idx),
			})
			if err != nil {
				errorCount.Add(1)
				return
			}
			if state.Status == agentic.StatusCompleted {
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()
	assert.Equal(t, int64(goroutines), successCount.Load(),
		"all %d workflows should complete successfully", goroutines)
	assert.Equal(t, int64(0), errorCount.Load(), "no errors expected")
}

func TestConcurrentStateMutations_Stress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	const goroutines = 100

	cfg := agentic.DefaultWorkflowConfig()
	cfg.Timeout = 15 * time.Second
	cfg.MaxIterations = 5

	var counter atomic.Int64

	mutatingHandler := func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
		counter.Add(1)
		return &agentic.NodeOutput{
			Result:   counter.Load(),
			Metadata: map[string]interface{}{"goroutine": true},
		}, nil
	}

	var wg sync.WaitGroup
	var completedCount atomic.Int64

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()

			wf := agentic.NewWorkflow(
				fmt.Sprintf("state-mut-%d", idx),
				"state mutation stress test",
				cfg,
				nil,
			)

			_ = wf.AddNode(&agentic.Node{
				ID: "mutate", Name: "Mutate", Type: agentic.NodeTypeAgent,
				Handler: mutatingHandler,
			})
			_ = wf.SetEntryPoint("mutate")
			_ = wf.AddEndNode("mutate")

			state, err := wf.Execute(context.Background(), nil)
			if err == nil && state.Status == agentic.StatusCompleted {
				completedCount.Add(1)
			}
		}(i)
	}

	wg.Wait()
	assert.Equal(t, int64(goroutines), completedCount.Load(),
		"all goroutines should complete workflow")
	assert.GreaterOrEqual(t, counter.Load(), int64(goroutines),
		"counter should be at least %d", goroutines)
}

func TestConcurrentNodeAdditions_Stress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	const goroutines = 80

	wf := agentic.NewWorkflow("node-add-stress", "concurrent node additions", nil, nil)

	var wg sync.WaitGroup
	var errCount atomic.Int64

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()

			node := &agentic.Node{
				ID:      fmt.Sprintf("node-%d", idx),
				Name:    fmt.Sprintf("Node-%d", idx),
				Type:    agentic.NodeTypeAgent,
				Handler: echoHandler(),
			}
			if err := wf.AddNode(node); err != nil {
				errCount.Add(1)
			}
		}(i)
	}

	wg.Wait()
	assert.Equal(t, int64(0), errCount.Load(), "no errors expected during concurrent node additions")

	// Verify all nodes were added
	for i := 0; i < goroutines; i++ {
		nodeID := fmt.Sprintf("node-%d", i)
		err := wf.SetEntryPoint(nodeID)
		require.NoError(t, err, "node %s should exist", nodeID)
	}
}

func TestConcurrentWorkflowCreationAndExecution_Stress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	const goroutines = 50

	var wg sync.WaitGroup
	var completedCount atomic.Int64

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()

			cfg := agentic.DefaultWorkflowConfig()
			cfg.Timeout = 5 * time.Second
			cfg.EnableCheckpoints = true
			cfg.CheckpointInterval = 1

			wf := agentic.NewWorkflow(
				fmt.Sprintf("full-stress-%d", idx),
				"full lifecycle stress",
				cfg,
				nil,
			)

			_ = wf.AddNode(&agentic.Node{
				ID: "a", Name: "StepA", Type: agentic.NodeTypeAgent, Handler: echoHandler(),
			})
			_ = wf.AddNode(&agentic.Node{
				ID: "b", Name: "StepB", Type: agentic.NodeTypeTool, Handler: echoHandler(),
			})
			_ = wf.AddNode(&agentic.Node{
				ID: "c", Name: "StepC", Type: agentic.NodeTypeAgent, Handler: echoHandler(),
			})
			_ = wf.AddEdge("a", "b", nil, "a->b")
			_ = wf.AddEdge("b", "c", nil, "b->c")
			_ = wf.SetEntryPoint("a")
			_ = wf.AddEndNode("c")

			state, err := wf.Execute(context.Background(), &agentic.NodeInput{
				Query: fmt.Sprintf("stress-query-%d", idx),
			})
			if err == nil && state.Status == agentic.StatusCompleted {
				completedCount.Add(1)
			}
		}(i)
	}

	wg.Wait()
	assert.Equal(t, int64(goroutines), completedCount.Load())
}

func TestConcurrentEdgeOperations_Stress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	const goroutines = 60

	wf := agentic.NewWorkflow("edge-stress", "concurrent edge operations", nil, nil)

	// Pre-create nodes to add edges between
	for i := 0; i <= goroutines; i++ {
		_ = wf.AddNode(&agentic.Node{
			ID:      fmt.Sprintf("n-%d", i),
			Name:    fmt.Sprintf("Node-%d", i),
			Type:    agentic.NodeTypeAgent,
			Handler: echoHandler(),
		})
	}

	var wg sync.WaitGroup
	var errCount atomic.Int64

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()

			from := fmt.Sprintf("n-%d", idx)
			to := fmt.Sprintf("n-%d", idx+1)
			if err := wf.AddEdge(from, to, nil, fmt.Sprintf("%s->%s", from, to)); err != nil {
				errCount.Add(1)
			}
		}(i)
	}

	wg.Wait()
	assert.Equal(t, int64(0), errCount.Load(), "no errors expected during concurrent edge additions")
}
