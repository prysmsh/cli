package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// FlexibleID decodes either a JSON string or number into a string identifier.
type FlexibleID string

func (f *FlexibleID) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" || trimmed == "null" {
		*f = ""
		return nil
	}

	var asString string
	if err := json.Unmarshal(b, &asString); err == nil {
		*f = FlexibleID(strings.TrimSpace(asString))
		return nil
	}

	var asInt int64
	if err := json.Unmarshal(b, &asInt); err == nil {
		*f = FlexibleID(strconv.FormatInt(asInt, 10))
		return nil
	}

	var asFloat float64
	if err := json.Unmarshal(b, &asFloat); err == nil {
		*f = FlexibleID(strconv.FormatInt(int64(asFloat), 10))
		return nil
	}

	return fmt.Errorf("invalid flexible id: %s", trimmed)
}

func (f FlexibleID) String() string {
	return string(f)
}
