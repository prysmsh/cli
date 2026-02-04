package api

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAPIErrorError(t *testing.T) {
	tests := []struct {
		name string
		err  *APIError
		want string
	}{
		{
			name: "nil error",
			err:  nil,
			want: "<nil>",
		},
		{
			name: "error with code",
			err: &APIError{
				StatusCode: 401,
				Code:       "AUTH_INVALID_TOKEN",
				Message:    "Token expired",
			},
			want: "api error (AUTH_INVALID_TOKEN): Token expired",
		},
		{
			name: "error without code",
			err: &APIError{
				StatusCode: 500,
				Message:    "Internal server error",
			},
			want: "api error: Internal server error",
		},
		{
			name: "error with details",
			err: &APIError{
				StatusCode: 400,
				Code:       "VALIDATION_ERROR",
				Message:    "Invalid input",
				Details:    `{"field": "email"}`,
			},
			want: "api error (VALIDATION_ERROR): Invalid input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseAPIError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantCode   string
		wantMsg    string
	}{
		{
			name:       "json with code and message",
			statusCode: 401,
			body:       `{"code": "UNAUTHORIZED", "message": "Invalid credentials"}`,
			wantCode:   "UNAUTHORIZED",
			wantMsg:    "Invalid credentials",
		},
		{
			name:       "json with error field",
			statusCode: 400,
			body:       `{"error": "Bad request"}`,
			wantCode:   "",
			wantMsg:    "Bad request",
		},
		{
			name:       "json with both error and message",
			statusCode: 403,
			body:       `{"error": "forbidden", "message": "Access denied", "code": "ACCESS_DENIED"}`,
			wantCode:   "ACCESS_DENIED",
			wantMsg:    "Access denied",
		},
		{
			name:       "empty body",
			statusCode: 500,
			body:       "",
			wantCode:   "",
			wantMsg:    "Internal Server Error",
		},
		{
			name:       "invalid json",
			statusCode: 502,
			body:       "Bad Gateway",
			wantCode:   "",
			wantMsg:    "Bad Gateway",
		},
		{
			name:       "json with string details",
			statusCode: 400,
			body:       `{"code": "ERROR", "message": "Failed", "details": "extra info"}`,
			wantCode:   "ERROR",
			wantMsg:    "Failed",
		},
		{
			name:       "json with object details",
			statusCode: 400,
			body:       `{"code": "VALIDATION", "message": "Invalid", "details": {"field": "email"}}`,
			wantCode:   "VALIDATION",
			wantMsg:    "Invalid",
		},
		{
			name:       "json with array details",
			statusCode: 400,
			body:       `{"code": "MULTI_ERROR", "message": "Multiple errors", "details": ["err1", "err2"]}`,
			wantCode:   "MULTI_ERROR",
			wantMsg:    "Multiple errors",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.statusCode,
				Status:     http.StatusText(tt.statusCode),
				Body:       io.NopCloser(bytes.NewBufferString(tt.body)),
			}
			if resp.Status == "" {
				resp.Status = "500 Internal Server Error"
			} else {
				resp.Status = strings.TrimSpace(strings.Split(http.StatusText(tt.statusCode), " ")[0] + " " + http.StatusText(tt.statusCode))
			}

			err := parseAPIError(resp)

			if err.StatusCode != tt.statusCode {
				t.Errorf("StatusCode = %d, want %d", err.StatusCode, tt.statusCode)
			}
			if err.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", err.Code, tt.wantCode)
			}
			if !strings.Contains(err.Message, tt.wantMsg) && err.Message != tt.wantMsg {
				t.Errorf("Message = %q, want to contain %q", err.Message, tt.wantMsg)
			}
		})
	}
}

func TestParseAPIErrorDetails(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantDetails string
	}{
		{
			name:        "string details",
			body:        `{"code": "ERR", "message": "msg", "details": "detail string"}`,
			wantDetails: "detail string",
		},
		{
			name:        "object details",
			body:        `{"code": "ERR", "message": "msg", "details": {"key": "value"}}`,
			wantDetails: `{"key":"value"}`,
		},
		{
			name:        "array details",
			body:        `{"code": "ERR", "message": "msg", "details": ["a", "b"]}`,
			wantDetails: `["a","b"]`,
		},
		{
			name:        "no details",
			body:        `{"code": "ERR", "message": "msg"}`,
			wantDetails: "",
		},
		{
			name:        "null details",
			body:        `{"code": "ERR", "message": "msg", "details": null}`,
			wantDetails: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: 400,
				Status:     "400 Bad Request",
				Body:       io.NopCloser(bytes.NewBufferString(tt.body)),
			}

			err := parseAPIError(resp)

			if err.Details != tt.wantDetails {
				t.Errorf("Details = %q, want %q", err.Details, tt.wantDetails)
			}
		})
	}
}

func TestAPIErrorImplementsError(t *testing.T) {
	var _ error = &APIError{}
}
