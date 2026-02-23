package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// APIError represents an error returned by the control plane API.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Details    string
}

func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Code != "" {
		return fmt.Sprintf("api error (%s): %s", e.Code, e.Message)
	}
	return fmt.Sprintf("api error: %s", e.Message)
}

func parseAPIError(resp *http.Response) *APIError {
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	errPayload := struct {
		Error   string      `json:"error"`
		Code    string      `json:"code"`
		Message string      `json:"message"`
		Details interface{} `json:"details"`
	}{}
	if len(body) > 0 {
		if json.Unmarshal(body, &errPayload) != nil {
			// Non-JSON response (e.g. HTML error page from a proxy/CDN).
			// Return a concise message instead of dumping raw markup.
			if looksLikeHTML(body) {
				return &APIError{
					StatusCode: resp.StatusCode,
					Message:    fmt.Sprintf("server returned %d (%s)", resp.StatusCode, http.StatusText(resp.StatusCode)),
				}
			}
			return &APIError{
				StatusCode: resp.StatusCode,
				Message:    string(body),
			}
		}
	}

	msg := errPayload.Message
	if msg == "" {
		msg = errPayload.Error
	}
	if msg == "" {
		msg = resp.Status
	}

	var details string
	switch v := errPayload.Details.(type) {
	case string:
		details = v
	case fmt.Stringer:
		details = v.String()
	case map[string]interface{}, []interface{}:
		if data, err := json.Marshal(v); err == nil {
			details = string(data)
		}
	}

	return &APIError{
		StatusCode: resp.StatusCode,
		Code:       errPayload.Code,
		Message:    msg,
		Details:    details,
	}
}

// looksLikeHTML reports whether body appears to be an HTML document.
func looksLikeHTML(body []byte) bool {
	s := strings.TrimSpace(string(body))
	return strings.HasPrefix(s, "<!") || strings.HasPrefix(s, "<html") || strings.HasPrefix(s, "<HTML")
}
