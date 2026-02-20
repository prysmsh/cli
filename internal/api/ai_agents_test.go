package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/warp-run/prysm-cli/internal/api"
)

func TestListAIAgents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/ai-agents") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"agents": []api.AIAgent{
				{ID: 1, Name: "agent1", Status: "running"},
			},
			"total": 1,
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	agents, err := client.ListAIAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAIAgents: %v", err)
	}
	if len(agents) != 1 || agents[0].Name != "agent1" {
		t.Errorf("agents = %v", agents)
	}
}

func TestListAIAgentsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"agents": nil, "total": 0})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	agents, err := client.ListAIAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAIAgents: %v", err)
	}
	if agents == nil || len(agents) != 0 {
		t.Errorf("agents = %v", agents)
	}
}

func TestGetAIAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.Contains(r.URL.Path, "/ai-agents/42") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.AIAgent{ID: 42, Name: "my-agent", Status: "deployed"})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	agent, err := client.GetAIAgent(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetAIAgent: %v", err)
	}
	if agent.ID != 42 || agent.Name != "my-agent" {
		t.Errorf("agent = %+v", agent)
	}
}

func TestCreateAIAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/ai-agents") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.AIAgent{ID: 10, Name: "new-agent", Status: "pending"})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	agent, err := client.CreateAIAgent(context.Background(), api.AIAgentCreateRequest{
		Name: "new-agent", Type: "assistant", Runtime: "openai",
	})
	if err != nil {
		t.Fatalf("CreateAIAgent: %v", err)
	}
	if agent.ID != 10 || agent.Name != "new-agent" {
		t.Errorf("agent = %+v", agent)
	}
}

func TestDeployAIAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.Contains(r.URL.Path, "/ai-agents/1/deploy") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.DeployAIAgent(context.Background(), 1)
	if err != nil {
		t.Fatalf("DeployAIAgent: %v", err)
	}
}

func TestUndeployAIAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.Contains(r.URL.Path, "/ai-agents/1/undeploy") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.UndeployAIAgent(context.Background(), 1)
	if err != nil {
		t.Fatalf("UndeployAIAgent: %v", err)
	}
}

func TestDeleteAIAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || !strings.Contains(r.URL.Path, "/ai-agents/1") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.DeleteAIAgent(context.Background(), 1)
	if err != nil {
		t.Fatalf("DeleteAIAgent: %v", err)
	}
}

func TestGetAIAgentLogs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.Contains(r.URL.Path, "/ai-agents/1/logs") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"logs": []map[string]interface{}{
				{"line": "log line 1", "source": "stdout", "timestamp": time.Now()},
				{"line": "log line 2", "source": "stderr", "timestamp": time.Now()},
			},
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	logs, err := client.GetAIAgentLogs(context.Background(), 1, 100)
	if err != nil {
		t.Fatalf("GetAIAgentLogs: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("len(logs) = %d", len(logs))
	}
	if !strings.Contains(logs[0], "log line 1") {
		t.Errorf("logs[0] = %q", logs[0])
	}
}

func TestParseAIAgentID(t *testing.T) {
	tests := []struct {
		input string
		want  uint
		err   bool
	}{
		{"0", 0, false},
		{"42", 42, false},
		{"999", 999, false},
		{"", 0, true},
		{"x", 0, true},
		{"1.5", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := api.ParseAIAgentID(tt.input)
			if tt.err {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAIAgentID: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}
