package benchmark

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"digital.vasic.agentic/agentic"
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

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkWorkflowCreation(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		agentic.NewWorkflow(
			fmt.Sprintf("bench-wf-%d", i),
			"benchmark workflow",
			nil,
			nil,
		)
	}
}

func BenchmarkWorkflowCreationWithConfig(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	cfg := agentic.DefaultWorkflowConfig()
	cfg.Timeout = 5 * time.Second

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		agentic.NewWorkflow(
			fmt.Sprintf("bench-wf-cfg-%d", i),
			"benchmark workflow with config",
			cfg,
			nil,
		)
	}
}

func BenchmarkNodeAddition(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	wf := agentic.NewWorkflow("bench-add-node", "benchmark node addition", nil, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = wf.AddNode(&agentic.Node{
			ID:      fmt.Sprintf("node-%d", i),
			Name:    fmt.Sprintf("Node-%d", i),
			Type:    agentic.NodeTypeAgent,
			Handler: echoHandler(),
		})
	}
}

func BenchmarkEdgeAddition(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	wf := agentic.NewWorkflow("bench-add-edge", "benchmark edge addition", nil, nil)

	// Pre-create enough nodes
	for i := 0; i <= b.N+1; i++ {
		_ = wf.AddNode(&agentic.Node{
			ID:   fmt.Sprintf("n-%d", i),
			Name: fmt.Sprintf("N-%d", i),
			Type: agentic.NodeTypeAgent,
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = wf.AddEdge(
			fmt.Sprintf("n-%d", i),
			fmt.Sprintf("n-%d", i+1),
			nil,
			fmt.Sprintf("edge-%d", i),
		)
	}
}

func BenchmarkSingleNodeExecution(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	cfg := agentic.DefaultWorkflowConfig()
	cfg.Timeout = 10 * time.Second

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wf := agentic.NewWorkflow("bench-exec", "benchmark single exec", cfg, nil)
		_ = wf.AddNode(&agentic.Node{
			ID: "only", Name: "Only", Type: agentic.NodeTypeAgent, Handler: echoHandler(),
		})
		_ = wf.SetEntryPoint("only")
		_ = wf.AddEndNode("only")

		_, _ = wf.Execute(context.Background(), &agentic.NodeInput{Query: "bench"})
	}
}

func BenchmarkLinearWorkflowExecution(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	cfg := agentic.DefaultWorkflowConfig()
	cfg.Timeout = 10 * time.Second
	cfg.EnableCheckpoints = false

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wf := agentic.NewWorkflow("bench-linear", "benchmark linear", cfg, nil)
		_ = wf.AddNode(&agentic.Node{
			ID: "a", Name: "A", Type: agentic.NodeTypeAgent, Handler: echoHandler(),
		})
		_ = wf.AddNode(&agentic.Node{
			ID: "b", Name: "B", Type: agentic.NodeTypeTool, Handler: echoHandler(),
		})
		_ = wf.AddNode(&agentic.Node{
			ID: "c", Name: "C", Type: agentic.NodeTypeAgent, Handler: echoHandler(),
		})
		_ = wf.AddEdge("a", "b", nil, "")
		_ = wf.AddEdge("b", "c", nil, "")
		_ = wf.SetEntryPoint("a")
		_ = wf.AddEndNode("c")

		_, _ = wf.Execute(context.Background(), nil)
	}
}

func BenchmarkWorkflowWithCheckpoints(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	cfg := agentic.DefaultWorkflowConfig()
	cfg.Timeout = 10 * time.Second
	cfg.EnableCheckpoints = true
	cfg.CheckpointInterval = 1

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wf := agentic.NewWorkflow("bench-cp", "benchmark checkpoints", cfg, nil)
		_ = wf.AddNode(&agentic.Node{
			ID: "a", Name: "A", Type: agentic.NodeTypeAgent, Handler: echoHandler(),
		})
		_ = wf.AddNode(&agentic.Node{
			ID: "b", Name: "B", Type: agentic.NodeTypeAgent, Handler: echoHandler(),
		})
		_ = wf.AddEdge("a", "b", nil, "")
		_ = wf.SetEntryPoint("a")
		_ = wf.AddEndNode("b")

		_, _ = wf.Execute(context.Background(), nil)
	}
}

func BenchmarkStateOperations(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	cfg := agentic.DefaultWorkflowConfig()
	cfg.Timeout = 10 * time.Second

	stateHandler := func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
		state.Variables["key1"] = "value1"
		state.Variables["key2"] = 42
		state.Variables["key3"] = true
		state.Variables["key4"] = []string{"a", "b", "c"}
		return &agentic.NodeOutput{Result: "done"}, nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wf := agentic.NewWorkflow("bench-state", "benchmark state ops", cfg, nil)
		_ = wf.AddNode(&agentic.Node{
			ID: "s", Name: "S", Type: agentic.NodeTypeAgent, Handler: stateHandler,
		})
		_ = wf.SetEntryPoint("s")
		_ = wf.AddEndNode("s")

		_, _ = wf.Execute(context.Background(), nil)
	}
}

func BenchmarkConditionalRouting(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	cfg := agentic.DefaultWorkflowConfig()
	cfg.Timeout = 10 * time.Second

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wf := agentic.NewWorkflow("bench-cond", "benchmark conditional", cfg, nil)

		startHandler := func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
			state.Variables["val"] = i % 2
			return &agentic.NodeOutput{}, nil
		}
		endHandler := func(ctx context.Context, state *agentic.WorkflowState, input *agentic.NodeInput) (*agentic.NodeOutput, error) {
			return &agentic.NodeOutput{ShouldEnd: true}, nil
		}

		_ = wf.AddNode(&agentic.Node{ID: "start", Name: "Start", Type: agentic.NodeTypeAgent, Handler: startHandler})
		_ = wf.AddNode(&agentic.Node{ID: "even", Name: "Even", Type: agentic.NodeTypeAgent, Handler: endHandler})
		_ = wf.AddNode(&agentic.Node{ID: "odd", Name: "Odd", Type: agentic.NodeTypeAgent, Handler: endHandler})

		_ = wf.AddEdge("start", "even", func(state *agentic.WorkflowState) bool {
			v, ok := state.Variables["val"].(int)
			return ok && v == 0
		}, "even")
		_ = wf.AddEdge("start", "odd", func(state *agentic.WorkflowState) bool {
			v, ok := state.Variables["val"].(int)
			return ok && v != 0
		}, "odd")
		_ = wf.SetEntryPoint("start")

		_, _ = wf.Execute(context.Background(), nil)
	}
}

func BenchmarkDefaultWorkflowConfig(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfg := agentic.DefaultWorkflowConfig()
		require.NotNil(b, cfg)
	}
}

func BenchmarkRestoreFromCheckpoint(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	state := &agentic.WorkflowState{
		Checkpoints: make([]agentic.Checkpoint, 100),
		Variables:   make(map[string]interface{}),
	}
	for i := 0; i < 100; i++ {
		state.Checkpoints[i] = agentic.Checkpoint{
			ID:     fmt.Sprintf("cp-%d", i),
			NodeID: fmt.Sprintf("node-%d", i),
			State:  map[string]interface{}{"step": i},
		}
	}

	wf := agentic.NewWorkflow("bench-restore", "benchmark restore", nil, nil)
	// Add nodes so restore has valid targets
	for i := 0; i < 100; i++ {
		_ = wf.AddNode(&agentic.Node{
			ID:   fmt.Sprintf("node-%d", i),
			Name: fmt.Sprintf("Node-%d", i),
			Type: agentic.NodeTypeAgent,
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cpID := fmt.Sprintf("cp-%d", i%100)
		_ = wf.RestoreFromCheckpoint(state, cpID)
	}
}
