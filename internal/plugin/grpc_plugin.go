package plugin

import (
	"context"

	pluginv1 "github.com/warp-run/prysm-cli/proto/plugin/v1"
)

// GRPCPluginClient implements the Plugin interface by calling an external plugin over gRPC.
// Used on the host side to talk to external plugin binaries.
type GRPCPluginClient struct {
	client pluginv1.PluginServiceClient
}

func (c *GRPCPluginClient) Manifest() Manifest {
	resp, err := c.client.GetManifest(context.Background(), &pluginv1.GetManifestRequest{})
	if err != nil {
		return Manifest{Name: "unknown", Description: "failed to load manifest: " + err.Error()}
	}
	return Manifest{
		Name:        resp.Name,
		Version:     resp.Version,
		Description: resp.Description,
		Commands:    fromProtoCommandSpecs(resp.Commands),
	}
}

func (c *GRPCPluginClient) Execute(ctx context.Context, req ExecuteRequest) ExecuteResponse {
	resp, err := c.client.Execute(ctx, &pluginv1.ExecuteRequest{
		Args:         req.Args,
		Env:          req.Env,
		WorkingDir:   req.WorkingDir,
		OutputFormat: req.OutputFormat,
		Debug:        req.Debug,
	})
	if err != nil {
		return ExecuteResponse{ExitCode: 1, Error: err.Error()}
	}
	return ExecuteResponse{
		ExitCode: int(resp.ExitCode),
		Error:    resp.Error,
		Stdout:   resp.Stdout,
	}
}

func fromProtoCommandSpecs(specs []*pluginv1.CommandSpec) []CommandSpec {
	out := make([]CommandSpec, len(specs))
	for i, s := range specs {
		out[i] = CommandSpec{
			Name:        s.Name,
			Short:       s.Short,
			Long:        s.Long,
			Subcommands: fromProtoCommandSpecs(s.Subcommands),
		}
	}
	return out
}
