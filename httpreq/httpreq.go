package httpreq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/tidwall/gjson"
)

// Request is a struct that represents a request to the server.
type Request struct {
	Client  *http.Client
	URL     string
	Method  string
	Header  http.Header
	Body    io.Reader
	Timeout time.Duration
}

// New creates a new Request.
func New(url, method string) *Request {
	return &Request{
		Client: http.DefaultClient,
		URL:    url,
		Method: method,
		Header: make(http.Header),
	}
}

// NewGet creates a new Request with the GET method.
func NewGet(url string) *Request {
	return New(url, http.MethodGet)
}

// NewPost creates a new Request with the POST method.
func NewPost(url string) *Request {
	return New(url, http.MethodPost)
}

// SetHeader sets a header field in the request.
func (r *Request) SetHeader(key, value string) *Request {
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	r.Header.Set(key, value)
	return r
}

// SetBody sets the body of the request.
func (r *Request) SetBody(body io.Reader) *Request {
	r.Body = body
	return r
}

// SetJSONBody sets the body of the request to a JSON-encoded representation of the given data.
func (r *Request) SetJSONBody(data any) *Request {
	jsonBody, err := EncodeToJSONReader(data)
	if err == nil {
		r.SetBody(jsonBody)
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}

// SetClient sets the client of the request.
func (r *Request) SetClient(client *http.Client) *Request {
	r.Client = client
	return r
}

// SetTimeout sets the timeout of the request.
func (r *Request) SetTimeout(timeout time.Duration) *Request {
	r.Timeout = timeout
	return r
}

func (r *Request) AddQuery(key, value string) *Request {
	u, err := url.Parse(r.URL)
	if err != nil {
		return r
	}
	q := u.Query()
	q.Add(key, value)
	u.RawQuery = q.Encode()
	r.URL = u.String()
	return r
}

// Do send the request to the server and returns the response and any error encountered.
//
// CAUTION: You must close the response body after using it.
func (r *Request) Do() (*http.Response, error) {
	var req *http.Request
	var err error
	if r.Timeout > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), r.Timeout)
		defer cancel()
		req, err = http.NewRequestWithContext(ctx, r.Method, r.URL, r.Body)
	} else {
		req, err = http.NewRequest(r.Method, r.URL, r.Body)
	}
	if err != nil {
		return nil, err
	}
	req.Header = r.Header
	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// DoJSON sends the request to the server and returns the response as JSON.
func (r *Request) DoJSON() (gjson.Result, error) {
	resp, err := r.Do()
	if err != nil {
		return gjson.Result{}, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return gjson.Result{}, fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}

	jsonResult, err := ParseJSON(resp.Body)
	if err != nil {
		return gjson.Result{}, err
	}
	return jsonResult, nil
}

// ParseJSON can read the response body and returns the content as JSON.
func ParseJSON(r io.Reader) (gjson.Result, error) {
	buf := bytes.NewBuffer(nil)
	if _, err := io.Copy(buf, r); err != nil {
		return gjson.Result{}, err
	}
	return gjson.ParseBytes(buf.Bytes()), nil
}

// EncodeToJSONReader encodes the given value to JSON and returns an io.Reader.
func EncodeToJSONReader(p any) (io.Reader, error) {
	buf := bytes.NewBuffer(nil)

	switch v := p.(type) {
	case string:
		buf.WriteString(v)
	case []byte:
		buf.Write(v)
	default:
		if err := json.NewEncoder(buf).Encode(p); err != nil {
			return nil, err
		}
	}

	return bytes.NewReader(buf.Bytes()), nil
}
