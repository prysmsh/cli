package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/warp-run/prysm-cli/internal/api"
)

func TestRegisterMeshNode_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid payload"}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	_, err := client.RegisterMeshNode(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error from RegisterMeshNode")
	}
}

func TestRegisterMeshNode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.Contains(r.URL.Path, "/mesh/nodes/register") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "registered",
			"node":    api.MeshNode{ID: 1, DeviceID: "dev-1", Status: "online"},
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	node, err := client.RegisterMeshNode(context.Background(), map[string]interface{}{"device_id": "dev-1"})
	if err != nil {
		t.Fatalf("RegisterMeshNode: %v", err)
	}
	if node.DeviceID != "dev-1" || node.Status != "online" {
		t.Errorf("node = %+v", node)
	}
}

func TestListMeshNodes_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	_, err := client.ListMeshNodes(context.Background())
	if err == nil {
		t.Fatal("expected error from ListMeshNodes")
	}
}

func TestListMeshNodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.Contains(r.URL.Path, "/mesh/nodes") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"nodes": []api.MeshNode{
				{ID: 1, DeviceID: "dev-1", Status: "online"},
			},
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	nodes, err := client.ListMeshNodes(context.Background())
	if err != nil {
		t.Fatalf("ListMeshNodes: %v", err)
	}
	if len(nodes) != 1 || nodes[0].DeviceID != "dev-1" {
		t.Errorf("nodes = %v", nodes)
	}
}

func TestPingMeshNode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.Contains(r.URL.Path, "/mesh/nodes/ping") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.PingMeshNode(context.Background(), "dev-1")
	if err != nil {
		t.Fatalf("PingMeshNode: %v", err)
	}
}

func TestEnableMeshNodeExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.Contains(r.URL.Path, "/mesh/nodes/1/exit") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.EnableMeshNodeExit(context.Background(), 1)
	if err != nil {
		t.Fatalf("EnableMeshNodeExit: %v", err)
	}
}

func TestDisableMeshNodeExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || !strings.Contains(r.URL.Path, "/mesh/nodes/1/exit") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.DisableMeshNodeExit(context.Background(), 1)
	if err != nil {
		t.Fatalf("DisableMeshNodeExit: %v", err)
	}
}

func TestSetMeshNodeExitByDeviceID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || !strings.Contains(r.URL.Path, "/mesh/nodes/by-device/dev-1/exit") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.SetMeshNodeExitByDeviceID(context.Background(), "dev-1", true)
	if err != nil {
		t.Fatalf("SetMeshNodeExitByDeviceID: %v", err)
	}
}
