package tritonhttp

import (
	"bufio"
	"strings"
	"fmt"
)

type Request struct {
	Method string // e.g. "GET"
	URL    string // e.g. "/path/to/a/file"
	Proto  string // e.g. "HTTP/1.1"

	// Header stores misc headers excluding "Host" and "Connection",
	// which are stored in special fields below.
	// Header keys are case-incensitive, and should be stored
	// in the canonical format in this map.
	Header map[string]string

	Host  string // determine from the "Host" header
	Close bool   // determine from the "Connection" header
}

// ReadRequest tries to read the next valid request from br.
//
// If it succeeds, it returns the valid request read. In this case,
// bytesReceived should be true, and err should be nil.
//
// If an error occurs during the reading, it returns the error,
// and a nil request. In this case, bytesReceived indicates whether or not
// some bytes are received before the error occurs. This is useful to determine
// the timeout with partial request received condition.
func ReadRequest(br *bufio.Reader) (req *Request, bytesReceived bool, err error) {
	// TODO: error handling
	req = &Request{}
	req.Header = make(map[string]string)

	// Read start line
	line, err := ReadLine(br)
	if err != nil {
		return nil, false, err
	}

	req.Method, req.URL, req.Proto, err = parseRequestLine(line)
	if err != nil {
		return nil, true, err
	}
	if req.Method != "GET" || req.URL[0] != '/' {
		return nil, true, fmt.Errorf("400")
	}
	// if req.URL[len(req.URL) - 1] == '/' {
	// 	req.URL = req.URL + "index.html"
	// }

	// Read headers
	hasHost := false
	req.Close = false
	for {
		line, err := ReadLine(br)
		//fmt.Printf(line)
		if err != nil {
			return nil, true, err
		}
		if line == "" {
			// This marks header end
			break
		}
		// Check required headers
		fields := strings.SplitN(line, ": ", 2)
		if len(fields) != 2 {
			return nil, true, fmt.Errorf("400")
		}
		key := CanonicalHeaderKey(strings.TrimSpace(fields[0]))
		value := strings.TrimSpace(fields[1])

		// Handle special headers
		if key == "Host" {
			req.Host = value
			hasHost = true
		} else if key == "Connection" && value == "close" {
			req.Close = true
		} else {
			req.Header[key] = value
		}
	}

	if !hasHost {
		return nil, true, fmt.Errorf("400")
	}

	return req, true, nil
}

func parseRequestLine(line string) (Method string, URL string, Proto string, err error) {
	fields := strings.SplitN(line, " ", 3)
	if len(fields) != 3 {
		return "", "", "", fmt.Errorf("could not parse the request line, got fields %v", fields)
	}
	return fields[0], fields[1], fields[2], nil
}