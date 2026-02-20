package plugin

import (
	"context"

	pluginv1 "github.com/warp-run/prysm-cli/proto/plugin/v1"
)

// GRPCHostServer implements the HostService gRPC server.
// Wraps the HostServices interface so external plugins can call host methods.
type GRPCHostServer struct {
	pluginv1.UnimplementedHostServiceServer
	host HostServices
}

// NewGRPCHostServer creates a new gRPC host service server.
func NewGRPCHostServer(host HostServices) *GRPCHostServer {
	return &GRPCHostServer{host: host}
}

func (s *GRPCHostServer) GetAuthContext(ctx context.Context, req *pluginv1.GetAuthContextRequest) (*pluginv1.GetAuthContextResponse, error) {
	auth, err := s.host.GetAuthContext(ctx)
	if err != nil {
		return nil, err
	}
	return &pluginv1.GetAuthContextResponse{
		Token:      auth.Token,
		OrgId:      auth.OrgID,
		OrgName:    auth.OrgName,
		UserId:     auth.UserID,
		UserEmail:  auth.UserEmail,
		ApiBaseUrl: auth.APIBaseURL,
	}, nil
}

func (s *GRPCHostServer) APIRequest(ctx context.Context, req *pluginv1.APIRequestRequest) (*pluginv1.APIRequestResponse, error) {
	status, body, err := s.host.APIRequest(ctx, req.Method, req.Endpoint, req.Body)
	if err != nil {
		return nil, err
	}
	return &pluginv1.APIRequestResponse{
		StatusCode: int32(status),
		Body:       body,
	}, nil
}

func (s *GRPCHostServer) GetConfig(ctx context.Context, req *pluginv1.GetConfigRequest) (*pluginv1.GetConfigResponse, error) {
	cfg, err := s.host.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	return &pluginv1.GetConfigResponse{
		ApiBaseUrl:   cfg.APIBaseURL,
		DerpUrl:      cfg.DERPURL,
		HomeDir:      cfg.HomeDir,
		OutputFormat: cfg.OutputFormat,
	}, nil
}

func (s *GRPCHostServer) Log(ctx context.Context, req *pluginv1.LogRequest) (*pluginv1.LogResponse, error) {
	level := LogLevel(req.Level)
	if err := s.host.Log(ctx, level, req.Message); err != nil {
		return nil, err
	}
	return &pluginv1.LogResponse{}, nil
}

func (s *GRPCHostServer) PromptInput(ctx context.Context, req *pluginv1.PromptInputRequest) (*pluginv1.PromptInputResponse, error) {
	value, err := s.host.PromptInput(ctx, req.Label, req.IsSecret)
	if err != nil {
		return nil, err
	}
	return &pluginv1.PromptInputResponse{Value: value}, nil
}

func (s *GRPCHostServer) PromptConfirm(ctx context.Context, req *pluginv1.PromptConfirmRequest) (*pluginv1.PromptConfirmResponse, error) {
	confirmed, err := s.host.PromptConfirm(ctx, req.Label)
	if err != nil {
		return nil, err
	}
	return &pluginv1.PromptConfirmResponse{Confirmed: confirmed}, nil
}
