package testutil

import (
	"encoding/json"
	"github.com/lefinal/meh"
	"io"
	"net/http"
	"net/http/httptest"
)

// HTTPRequestProps are the props used in DoHTTPRequestMust.
type HTTPRequestProps struct {
	// Server is the http.Handler that is used in order to serve the HTTP request.
	Server http.Handler
	// Method is the HTTP method to use.
	Method string
	// URL is the called URL.
	URL string
	// Body is the optional body. If not provided, set it to nil.
	Body io.Reader
}

// DoHTTPRequestMust creates a new http.Request using the given properties and
// serves HTTP with the http.Handler. The response is recorded and returned
// using an httptest.ResponseRecorder.
func DoHTTPRequestMust(props HTTPRequestProps) *httptest.ResponseRecorder {
	req, err := http.NewRequest(props.Method, props.URL, props.Body)
	if err != nil {
		panic(meh.NewInternalErrFromErr(err, "new http request", nil).Error())
	}
	// Do request.
	rr := httptest.NewRecorder()
	props.Server.ServeHTTP(rr, req)
	return rr
}

// MarshalJSONMust calls json.Marshal for the given data and panics in case of
// failure.
func MarshalJSONMust(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(meh.NewInternalErrFromErr(err, "marshal json", nil))
	}
	return b
}
