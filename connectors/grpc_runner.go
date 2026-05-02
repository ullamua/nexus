package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nexus/core/definitions"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// GRPCRunner executes connector actions over gRPC.
type GRPCRunner struct {
	transformer *Transformer
}

// NewGRPCRunner creates a GRPCRunner backed by the given transformer.
func NewGRPCRunner(transformer *Transformer) *GRPCRunner {
	return &GRPCRunner{transformer: transformer}
}

// Execute dials the gRPC endpoint and invokes the action method.
// This is a generic pass-through: params are marshalled to JSON and sent as raw bytes.
// For production use, each connector should provide a generated proto client.
func (g *GRPCRunner) Execute(ctx context.Context, def *definitions.ConnectorDef, actionName string, params map[string]interface{}) (*RunResult, error) {
	action, ok := def.Actions[actionName]
	if !ok {
		return nil, fmt.Errorf("grpc_runner: action %q not found in connector %s", actionName, def.Name)
	}

	timeout := time.Duration(def.TimeoutMS) * time.Millisecond
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, def.BaseURL,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc_runner: dial %s: %w", def.BaseURL, err)
	}
	defer conn.Close()

	reqBytes, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("grpc_runner: marshal params: %w", err)
	}

	var respBytes []byte
	start := time.Now()
	fullMethod := fmt.Sprintf("/%s/%s", action.GRPCService, action.GRPCMethod)
	if err := conn.Invoke(ctx, fullMethod, reqBytes, &respBytes); err != nil {
		return nil, fmt.Errorf("grpc_runner: invoke %s: %w", fullMethod, err)
	}
	elapsed := time.Since(start).Milliseconds()

	var upstream map[string]interface{}
	if err := json.Unmarshal(respBytes, &upstream); err != nil {
		return nil, fmt.Errorf("grpc_runner: decode response: %w", err)
	}

	transformed := g.transformer.Apply(upstream, action, def.KeepNulls)
	return &RunResult{Data: transformed, LatencyMS: elapsed}, nil
}
