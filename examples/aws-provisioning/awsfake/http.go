package awsfake

import "net/http"

// dummyHTTPResponse is a placeholder HTTP response used by the fake deserializer.
// The AWS SDK expects a non-nil response object even when we intercept at the
// middleware level.
var dummyHTTPResponse = http.Response{
	StatusCode: http.StatusOK,
	Header:     http.Header{},
}
