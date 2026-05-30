package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
)

// httpPostForm posts form data and returns the response.
func httpPostForm(endpoint string, data url.Values) (*http.Response, error) {
	resp, err := http.PostForm(endpoint, data)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// decodeJSON decodes a JSON response body.
func decodeJSON(body io.Reader, v any) error {
	return json.NewDecoder(body).Decode(v)
}
