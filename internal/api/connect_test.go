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

func TestConnectListClusters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.Contains(r.URL.Path, "/connect/k8s/clusters") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"clusters": []api.Cluster{
				{ID: 1, Name: "cluster-a", Status: "connected"},
			},
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	clusters, err := client.ListClusters(context.Background())
	if err != nil {
		t.Fatalf("ListClusters: %v", err)
	}
	if len(clusters) != 1 || clusters[0].Name != "cluster-a" {
		t.Errorf("clusters = %v", clusters)
	}
}

func TestConnectListClusters_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	_, err := client.ListClusters(context.Background())
	if err == nil {
		t.Fatal("expected error from ListClusters")
	}
}

func TestEnableClusterExitRouter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.Contains(r.URL.Path, "/clusters/1/exit-router") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.EnableClusterExitRouter(context.Background(), 1)
	if err != nil {
		t.Fatalf("EnableClusterExitRouter: %v", err)
	}
}

func TestDisableClusterExitRouter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || !strings.Contains(r.URL.Path, "/clusters/1/exit-router") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.DisableClusterExitRouter(context.Background(), 1)
	if err != nil {
		t.Fatalf("DisableClusterExitRouter: %v", err)
	}
}

func TestConnectKubernetes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/connect/k8s") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["cluster_id"] != float64(100) {
			t.Errorf("cluster_id = %v", body["cluster_id"])
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.ClusterConnectResponse{
			Cluster:    api.Cluster{ID: 100, Name: "k8s"},
			Session:    api.KubernetesSessionInfo{ID: 1, SessionID: "sess", Status: "active"},
			Kubeconfig: api.KubeconfigMaterial{Encoding: "base64", Value: "e30="},
			IssuedAt:   time.Now(),
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	resp, err := client.ConnectKubernetes(context.Background(), 100, "default", "test")
	if err != nil {
		t.Fatalf("ConnectKubernetes: %v", err)
	}
	if resp.Cluster.ID != 100 {
		t.Errorf("Cluster.ID = %d", resp.Cluster.ID)
	}
	if resp.Kubeconfig.Value != "e30=" {
		t.Errorf("Kubeconfig.Value = %q", resp.Kubeconfig.Value)
	}
}

func TestConnectKubernetesWithNamespaceAndReason(t *testing.T) {
	var capturedPayload map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedPayload)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.ClusterConnectResponse{
			Cluster:  api.Cluster{ID: 1},
			IssuedAt: time.Now(),
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	_, _ = client.ConnectKubernetes(context.Background(), 1, "myns", "reason")
	if capturedPayload["namespace"] != "myns" {
		t.Errorf("namespace = %v", capturedPayload["namespace"])
	}
	if capturedPayload["reason"] != "reason" {
		t.Errorf("reason = %v", capturedPayload["reason"])
	}
}
