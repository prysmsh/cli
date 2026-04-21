package cmd

import "testing"

func TestParseHTTPRequestLine(t *testing.T) {
	cases := []struct {
		name     string
		data     []byte
		wantOK   bool
		wantM, P string
	}{
		{"get", []byte("GET /foo HTTP/1.1\r\nHost: x\r\n\r\n"), true, "GET", "/foo"},
		{"post", []byte("POST /api/v1/things?a=b HTTP/1.0\r\n\r\n"), true, "POST", "/api/v1/things?a=b"},
		{"no crlf", []byte("GET /foo HTTP/1.1"), false, "", ""},
		{"lowercase method", []byte("get /foo HTTP/1.1\r\n"), false, "", ""},
		{"not http", []byte("\x16\x03\x01\x00\xff\r\n"), false, "", ""},
		{"binary", []byte{0, 1, 2, 3, 4, 5, '\r', '\n'}, false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, p, ok := parseHTTPRequestLine(tc.data)
			if ok != tc.wantOK || m != tc.wantM || p != tc.P {
				t.Fatalf("got (%q,%q,%v) want (%q,%q,%v)", m, p, ok, tc.wantM, tc.P, tc.wantOK)
			}
		})
	}
}

func TestParseHTTPStatusLine(t *testing.T) {
	cases := []struct {
		name   string
		data   []byte
		wantOK bool
		want   int
	}{
		{"200", []byte("HTTP/1.1 200 OK\r\n"), true, 200},
		{"404", []byte("HTTP/1.0 404 Not Found\r\n"), true, 404},
		{"500 no reason", []byte("HTTP/1.1 500 \r\n"), true, 500},
		{"not http", []byte("GET /foo HTTP/1.1\r\n"), false, 0},
		{"garbage code", []byte("HTTP/1.1 abc XX\r\n"), false, 0},
		{"out of range", []byte("HTTP/1.1 42 nope\r\n"), false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseHTTPStatusLine(tc.data)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("got (%d,%v) want (%d,%v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
