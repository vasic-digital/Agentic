// Package agentic provides graph-based workflow orchestration for autonomous AI agents
// with planning, execution, and self-correction capabilities.
package agentic

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// Workflow represents a graph-based agentic workflow
type Workflow struct {
	ID          string
	Name        string
	Description string
	Graph       *WorkflowGraph
	State       *WorkflowState
	Config      *WorkflowConfig
	Logger      *logrus.Logger
	mu          sync.RWMutex
}

// WorkflowGraph defines the workflow structure
type WorkflowGraph struct {
	Nodes      map[string]*Node
	Edges      []*Edge
	EntryPoint string
	EndNodes   []string
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

// NodeType indicates the type of node
type NodeType string

const (
	NodeTypeAgent     NodeType = "agent"     // LLM-based agent
	NodeTypeTool      NodeType = "tool"      // Tool execution
	NodeTypeCondition NodeType = "condition" // Conditional branching
	NodeTypeParallel  NodeType = "parallel"  // Parallel execution
	NodeTypeHuman     NodeType = "human"     // Human-in-the-loop
	NodeTypeSubgraph  NodeType = "subgraph"  // Nested workflow
)

// Edge represents a directed edge in the workflow
type Edge struct {
	From      string
	To        string
	Condition ConditionFunc // Optional condition for traversal
	Label     string
}

// NodeHandler handles node execution
type NodeHandler func(ctx context.Context, state *WorkflowState, input *NodeInput) (*NodeOutput, error)

// ConditionFunc evaluates conditions for routing
type ConditionFunc func(state *WorkflowState) bool

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
	NextNode  string // Override next node
	ShouldEnd bool
	Error     error
	Metadata  map[string]interface{}
}

// Message represents a conversation message
type Message struct {
	Role      string
	Content   string
	Name      string
	ToolCalls []ToolCall
}

// Tool represents an available tool
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]interface{}
	Handler     ToolHandler
}

// ToolCall represents a tool invocation
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]interface{}
	Result    interface{}
}

// ToolHandler executes a tool
type ToolHandler func(ctx context.Context, args map[string]interface{}) (interface{}, error)

// WorkflowState maintains state across the workflow
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
	mu          sync.RWMutex
}

// NodeExecution records a node execution
type NodeExecution struct {
	NodeID    string
	NodeName  string
	StartTime time.Time
	EndTime   time.Time
	Input     *NodeInput
	Output    *NodeOutput
	Error     error
}

// Checkpoint allows saving/restoring workflow state
type Checkpoint struct {
	ID        string
	NodeID    string
	State     map[string]interface{}
	Timestamp time.Time
}

// WorkflowStatus indicates the workflow status
type WorkflowStatus string

const (
	StatusPending   WorkflowStatus = "pending"
	StatusRunning   WorkflowStatus = "running"
	StatusPaused    WorkflowStatus = "paused"
	StatusCompleted WorkflowStatus = "completed"
	StatusFailed    WorkflowStatus = "failed"
)

// WorkflowConfig configures workflow execution
type WorkflowConfig struct {
	MaxIterations        int
	Timeout              time.Duration
	EnableCheckpoints    bool
	CheckpointInterval   int
	EnableSelfCorrection bool
	MaxRetries           int
	RetryDelay           time.Duration
}

// RetryPolicy defines retry behavior for a node
type RetryPolicy struct {
	MaxRetries int
	Delay      time.Duration
	Backoff    float64
}

// DefaultWorkflowConfig returns default configuration
func DefaultWorkflowConfig() *WorkflowConfig {
	return &WorkflowConfig{
		MaxIterations:        100,
		Timeout:              30 * time.Minute,
		EnableCheckpoints:    true,
		CheckpointInterval:   5,
		EnableSelfCorrection: true,
		MaxRetries:           3,
		RetryDelay:           1 * time.Second,
	}
}

// NewWorkflow creates a new workflow
func NewWorkflow(name, description string, config *WorkflowConfig, logger *logrus.Logger) *Workflow {
	if config == nil {
		config = DefaultWorkflowConfig()
	}
	if logger == nil {
		logger = logrus.New()
	}

	return &Workflow{
		ID:          uuid.New().String(),
		Name:        name,
		Description: description,
		Graph: &WorkflowGraph{
			Nodes:    make(map[string]*Node),
			Edges:    make([]*Edge, 0),
			EndNodes: make([]string, 0),
		},
		Config: config,
		Logger: logger,
	}
}

// AddNode adds a node to the workflow
func (w *Workflow) AddNode(node *Node) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if node.ID == "" {
		node.ID = uuid.New().String()
	}

	w.Graph.Nodes[node.ID] = node
	return nil
}

// AddEdge adds an edge between nodes
func (w *Workflow) AddEdge(from, to string, condition ConditionFunc, label string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.Graph.Nodes[from]; !exists {
		return fmt.Errorf("source node not found: %s", from)
	}
	if _, exists := w.Graph.Nodes[to]; !exists {
		return fmt.Errorf("target node not found: %s", to)
	}

	w.Graph.Edges = append(w.Graph.Edges, &Edge{
		From:      from,
		To:        to,
		Condition: condition,
		Label:     label,
	})

	return nil
}

// SetEntryPoint sets the entry node
func (w *Workflow) SetEntryPoint(nodeID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.Graph.Nodes[nodeID]; !exists {
		return fmt.Errorf("node not found: %s", nodeID)
	}

	w.Graph.EntryPoint = nodeID
	return nil
}

// AddEndNode marks a node as an end node
func (w *Workflow) AddEndNode(nodeID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.Graph.Nodes[nodeID]; !exists {
		return fmt.Errorf("node not found: %s", nodeID)
	}

	w.Graph.EndNodes = append(w.Graph.EndNodes, nodeID)
	return nil
}

// Execute runs the workflow
func (w *Workflow) Execute(ctx context.Context, input *NodeInput) (*WorkflowState, error) {
	w.mu.Lock()
	if w.Graph.EntryPoint == "" {
		w.mu.Unlock()
		return nil, fmt.Errorf("no entry point defined")
	}
	w.mu.Unlock()

	// Initialize state
	state := &WorkflowState{
		ID:          uuid.New().String(),
		WorkflowID:  w.ID,
		CurrentNode: w.Graph.EntryPoint,
		Messages:    make([]Message, 0),
		Variables:   make(map[string]interface{}),
		History:     make([]NodeExecution, 0),
		Checkpoints: make([]Checkpoint, 0),
		Status:      StatusRunning,
		StartTime:   time.Now(),
	}

	// Copy input messages
	if input != nil && input.Messages != nil {
		state.Messages = append(state.Messages, input.Messages...)
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, w.Config.Timeout)
	defer cancel()

	// Execute workflow
	err := w.executeLoop(execCtx, state, input)
	if err != nil {
		state.Status = StatusFailed
		state.Error = err
	} else {
		state.Status = StatusCompleted
	}

	now := time.Now()
	state.EndTime = &now

	return state, err
}

func (w *Workflow) executeLoop(ctx context.Context, state *WorkflowState, input *NodeInput) error {
	iterations := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check max iterations
		if iterations >= w.Config.MaxIterations {
			return fmt.Errorf("max iterations reached: %d", w.Config.MaxIterations)
		}
		iterations++

		// Get current node
		w.mu.RLock()
		currentNode, exists := w.Graph.Nodes[state.CurrentNode]
		w.mu.RUnlock()

		if !exists {
			return fmt.Errorf("node not found: %s", state.CurrentNode)
		}

		// Execute node
		execution := NodeExecution{
			NodeID:    currentNode.ID,
			NodeName:  currentNode.Name,
			StartTime: time.Now(),
			Input:     input,
		}

		output, err := w.executeNode(ctx, currentNode, state, input)
		execution.EndTime = time.Now()
		execution.Output = output
		execution.Error = err

		state.mu.Lock()
		state.History = append(state.History, execution)
		state.mu.Unlock()

		if err != nil {
			return fmt.Errorf("node %s failed: %w", currentNode.Name, err)
		}

		// Check if we should end
		if output.ShouldEnd {
			return nil
		}

		// Check if current node is an end node
		w.mu.RLock()
		isEndNode := false
		for _, endNode := range w.Graph.EndNodes {
			if endNode == state.CurrentNode {
				isEndNode = true
				break
			}
		}
		w.mu.RUnlock()

		if isEndNode {
			return nil
		}

		// Determine next node
		nextNode := output.NextNode
		if nextNode == "" {
			nextNode = w.getNextNode(state)
		}

		if nextNode == "" {
			return nil // No more nodes to execute
		}

		state.mu.Lock()
		state.CurrentNode = nextNode
		state.mu.Unlock()

		// Update input for next node
		input = &NodeInput{
			Previous: output,
			Context:  state.Variables,
		}

		// Checkpoint if enabled
		if w.Config.EnableCheckpoints && iterations%w.Config.CheckpointInterval == 0 {
			w.createCheckpoint(state)
		}
	}
}

func (w *Workflow) executeNode(ctx context.Context, node *Node, state *WorkflowState, input *NodeInput) (*NodeOutput, error) {
	if node.Handler == nil {
		return &NodeOutput{}, nil
	}

	var output *NodeOutput
	var err error

	// Execute with retries
	maxRetries := w.Config.MaxRetries
	if node.RetryPolicy != nil {
		maxRetries = node.RetryPolicy.MaxRetries
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		output, err = node.Handler(ctx, state, input)
		if err == nil {
			break
		}

		w.Logger.WithFields(logrus.Fields{
			"node":    node.Name,
			"attempt": attempt + 1,
			"error":   err,
		}).Warn("Node execution failed, retrying")

		if attempt < maxRetries {
			delay := w.Config.RetryDelay
			if node.RetryPolicy != nil {
				delay = time.Duration(float64(node.RetryPolicy.Delay) *
					pow(node.RetryPolicy.Backoff, float64(attempt)))
			}
			time.Sleep(delay)
		}
	}

	return output, err
}

func (w *Workflow) getNextNode(state *WorkflowState) string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	for _, edge := range w.Graph.Edges {
		if edge.From != state.CurrentNode {
			continue
		}

		// Check condition if present
		if edge.Condition != nil && !edge.Condition(state) {
			continue
		}

		return edge.To
	}

	return ""
}

func (w *Workflow) createCheckpoint(state *WorkflowState) {
	state.mu.Lock()
	defer state.mu.Unlock()

	checkpoint := Checkpoint{
		ID:        uuid.New().String(),
		NodeID:    state.CurrentNode,
		State:     make(map[string]interface{}),
		Timestamp: time.Now(),
	}

	// Copy variables
	for k, v := range state.Variables {
		checkpoint.State[k] = v
	}

	state.Checkpoints = append(state.Checkpoints, checkpoint)

	w.Logger.WithField("checkpoint_id", checkpoint.ID).Debug("Checkpoint created")
}

// RestoreFromCheckpoint restores state from a checkpoint
func (w *Workflow) RestoreFromCheckpoint(state *WorkflowState, checkpointID string) error {
	state.mu.Lock()
	defer state.mu.Unlock()

	for _, cp := range state.Checkpoints {
		if cp.ID == checkpointID {
			state.CurrentNode = cp.NodeID
			state.Variables = cp.State
			state.Status = StatusRunning
			return nil
		}
	}

	return fmt.Errorf("checkpoint not found: %s", checkpointID)
}

func pow(base, exp float64) float64 {
	result := 1.0
	for exp > 0 {
		result *= base
		exp--
	}
	return result
}
