package main

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
)

// Headers parses comma-separated curl-style headers, for example
// "Authorization: Bearer token, X-Api-Key: key". Commas always separate headers;
// this example syntax does not support commas inside a header value.
type Headers http.Header

func (h *Headers) UnmarshalText(text []byte) error {
	parsed := make(http.Header)
	if input := strings.TrimSpace(string(text)); input != "" {
		for _, entry := range strings.Split(input, ",") {
			name, value, ok := strings.Cut(entry, ":")
			name, value = strings.TrimSpace(name), strings.TrimSpace(value)
			if !ok || name == "" || strings.ContainsAny(name, " \t\r\n") || strings.ContainsAny(value, "\r\n") {
				return fmt.Errorf("expected comma-separated headers in K: V form")
			}
			parsed.Add(name, value)
		}
	}
	*h = Headers(parsed)
	return nil
}

// MarshalText uses sorted names so Boa can compare the value consistently
// between its two secret-file resolution passes.
func (h Headers) MarshalText() ([]byte, error) {
	var entries []string
	for _, name := range slices.Sorted(maps.Keys(h)) {
		for _, value := range h[name] {
			entries = append(entries, name+": "+value)
		}
	}
	return []byte(strings.Join(entries, ", ")), nil
}
