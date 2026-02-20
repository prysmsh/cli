package plugin

import (
	"context"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	pluginv1 "github.com/warp-run/prysm-cli/proto/plugin/v1"
)

// HandshakeConfig is the shared handshake for all Prysm plugins.
// Both the host and plugin must agree on these values.
var HandshakeConfig = goplugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PRYSM_PLUGIN",
	MagicCookieValue: "prysm-v1",
}

// PluginMap is the map of plugin types served/consumed by go-plugin.
const PluginKey = "plugin"

// GRPCPluginImpl implements goplugin.GRPCPlugin for the PluginService.
type GRPCPluginImpl struct {
	goplugin.Plugin
	// Impl is only set on the plugin side (server).
	Impl Plugin
}

// GRPCServer registers the PluginService server (plugin side).
func (p *GRPCPluginImpl) GRPCServer(broker *goplugin.GRPCBroker, s *grpc.Server) error {
	pluginv1.RegisterPluginServiceServer(s, &grpcPluginServer{impl: p.Impl})
	return nil
}

// GRPCClient returns a PluginService client (host side).
func (p *GRPCPluginImpl) GRPCClient(ctx context.Context, broker *goplugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &GRPCPluginClient{client: pluginv1.NewPluginServiceClient(c)}, nil
}

// grpcPluginServer wraps a Plugin into a gRPC server implementation.
type grpcPluginServer struct {
	pluginv1.UnimplementedPluginServiceServer
	impl Plugin
}

func (s *grpcPluginServer) GetManifest(ctx context.Context, req *pluginv1.GetManifestRequest) (*pluginv1.GetManifestResponse, error) {
	m := s.impl.Manifest()
	return &pluginv1.GetManifestResponse{
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		Commands:    convertCommandSpecs(m.Commands),
	}, nil
}

func (s *grpcPluginServer) Execute(ctx context.Context, req *pluginv1.ExecuteRequest) (*pluginv1.ExecuteResponse, error) {
	resp := s.impl.Execute(ctx, ExecuteRequest{
		Args:         req.Args,
		Env:          req.Env,
		WorkingDir:   req.WorkingDir,
		OutputFormat: req.OutputFormat,
		Debug:        req.Debug,
	})
	return &pluginv1.ExecuteResponse{
		ExitCode: int32(resp.ExitCode),
		Error:    resp.Error,
		Stdout:   resp.Stdout,
	}, nil
}

func convertCommandSpecs(specs []CommandSpec) []*pluginv1.CommandSpec {
	out := make([]*pluginv1.CommandSpec, len(specs))
	for i, s := range specs {
		out[i] = &pluginv1.CommandSpec{
			Name:        s.Name,
			Short:       s.Short,
			Long:        s.Long,
			Subcommands: convertCommandSpecs(s.Subcommands),
		}
	}
	return out
}
