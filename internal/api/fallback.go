package api

import (
	"errors"
	"net/http"
)

// isEndpointUnavailable returns true for API errors that indicate an endpoint
// is not present in this deployment and a fallback endpoint should be tried.
func isEndpointUnavailable(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}

	switch apiErr.StatusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	default:
		return false
	}
}
