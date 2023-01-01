package util

import (
	"fmt"
	"io"
	"net/http"
)

const (
	// The maximum size of an HTTP response body to read.
	maxHTTPBodySize = 100 * 1024 * 1024
)

// Performs an HTTP GET request and returns the response body, limited to the specified maximum size.
// If the response body exceeds the maximum size, an error is returned.
// If the maximum size is 0 or greater than maxHTTPBodySize, maxHTTPBodySize is used instead.
func HTTPLimitedGet(url string, maxSize int64) ([]byte, error) {
	if maxSize == 0 || maxSize > maxHTTPBodySize {
		maxSize = maxHTTPBodySize
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d", resp.StatusCode)
	}
	reader := io.LimitReader(resp.Body, maxSize+1)
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if len(body) > int(maxSize) {
		return nil, fmt.Errorf("response body exceeded maximum size of %d bytes", maxSize)
	}
	return body, nil
}
