package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prysmsh/cli/internal/api"
)

func TestCreateAccessRequest(t *testing.T) {
	var captured map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/access/requests") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"request": map[string]interface{}{
				"id":            "req_123",
				"status":        "pending",
				"resource":      "server-prod",
				"resource_type": "ssh",
				"reason":        "breakfix",
			},
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	exp := time.Now().UTC().Add(30 * time.Minute)
	created, err := client.CreateAccessRequest(context.Background(), api.AccessRequestCreateRequest{
		Resource:     "server-prod",
		ResourceType: "ssh",
		Reason:       "breakfix",
		ExpiresAt:    &exp,
		AuditFields: map[string]string{
			"ticket": "INC-123",
		},
	})
	if err != nil {
		t.Fatalf("CreateAccessRequest failed: %v", err)
	}
	if created.Identifier() != "req_123" {
		t.Fatalf("Identifier() = %q, want req_123", created.Identifier())
	}
	if captured["reason"] != "breakfix" {
		t.Fatalf("reason = %v, want breakfix", captured["reason"])
	}
	if captured["resource"] != "server-prod" {
		t.Fatalf("resource = %v, want server-prod", captured["resource"])
	}
	if captured["expires_at"] == nil {
		t.Fatalf("expires_at missing from payload")
	}
	if _, ok := captured["audit_fields"].(map[string]interface{}); !ok {
		t.Fatalf("audit_fields should be object, got %T", captured["audit_fields"])
	}
}

func TestListAccessRequestsWithFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/access/requests") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		q := r.URL.Query()
		if q.Get("status") != "pending" {
			t.Fatalf("status query = %q, want pending", q.Get("status"))
		}
		if q.Get("resource_type") != "ssh" {
			t.Fatalf("resource_type query = %q, want ssh", q.Get("resource_type"))
		}
		if q.Get("mine") != "true" {
			t.Fatalf("mine query = %q, want true", q.Get("mine"))
		}
		if q.Get("limit") != "5" {
			t.Fatalf("limit query = %q, want 5", q.Get("limit"))
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"requests": []map[string]interface{}{
				{"id": "req_1", "status": "pending"},
			},
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	requests, err := client.ListAccessRequests(context.Background(), api.AccessRequestListOptions{
		Status:       "pending",
		ResourceType: "ssh",
		Mine:         true,
		Limit:        5,
	})
	if err != nil {
		t.Fatalf("ListAccessRequests failed: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(requests))
	}
	if requests[0].Identifier() != "req_1" {
		t.Fatalf("identifier = %q, want req_1", requests[0].Identifier())
	}
}

func TestReviewAccessRequestEndpoints(t *testing.T) {
	var approveCalled bool
	var denyCalled bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/access/requests/req_1/approve"):
			approveCalled = true
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request": map[string]interface{}{
					"id":     "req_1",
					"status": "approved",
				},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/access/requests/req_2/deny"):
			denyCalled = true
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["reason"] != "policy violation" {
				t.Fatalf("deny reason = %q, want policy violation", payload["reason"])
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"request": map[string]interface{}{
					"id":     "req_2",
					"status": "denied",
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	if _, err := client.ApproveAccessRequest(context.Background(), "req_1", "ok"); err != nil {
		t.Fatalf("ApproveAccessRequest failed: %v", err)
	}
	if _, err := client.DenyAccessRequest(context.Background(), "req_2", "policy violation"); err != nil {
		t.Fatalf("DenyAccessRequest failed: %v", err)
	}
	if !approveCalled {
		t.Fatalf("approve endpoint not called")
	}
	if !denyCalled {
		t.Fatalf("deny endpoint not called")
	}
}
