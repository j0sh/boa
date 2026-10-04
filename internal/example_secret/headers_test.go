package main

import (
	"reflect"
	"testing"
)

func TestHeadersUnmarshalText(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        Headers
	}{
		{"empty", "", Headers{}},
		{"whitespace", " \n\t", Headers{}},
		{"multiple", " Authorization : Bearer token , X-Api-Key: key ", Headers{"Authorization": {"Bearer token"}, "X-Api-Key": {"key"}}},
		{"repeated", "X-Tag: first, x-tag: second", Headers{"X-Tag": {"first", "second"}}},
		{"colon in value", "Referer: https://example.com:8443/path", Headers{"Referer": {"https://example.com:8443/path"}}},
		{"empty value", "X-Empty:", Headers{"X-Empty": {""}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got Headers
			if err := got.UnmarshalText([]byte(tc.input)); err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("headers=%v, want %v, error=%v", got, tc.want, err)
			}
			text, err := got.MarshalText()
			if err != nil {
				t.Fatal(err)
			}
			var roundTrip Headers
			if err := roundTrip.UnmarshalText(text); err != nil || !reflect.DeepEqual(roundTrip, got) {
				t.Fatalf("headers failed to round-trip: %v", err)
			}
		})
	}
}

func TestHeadersRejectMalformedInput(t *testing.T) {
	for _, input := range []string{"missing colon", ": value", "X-Good: ok, bad", "X-Good: ok,", "X-Bad Name: value", "X-Bad: first\r\nInjected: value"} {
		t.Run(input, func(t *testing.T) {
			got := Headers{"X-Existing": {"preserved"}}
			if err := got.UnmarshalText([]byte(input)); err == nil {
				t.Fatal("malformed headers accepted")
			}
			if !reflect.DeepEqual(got, Headers{"X-Existing": {"preserved"}}) {
				t.Fatal("failed parse changed existing headers")
			}
		})
	}
}

func TestHeadersMarshalTextOrder(t *testing.T) {
	h := Headers{"X-Tag": {"first", "second"}, "Authorization": {"Bearer token"}}
	text, err := h.MarshalText()
	if err != nil || string(text) != "Authorization: Bearer token, X-Tag: first, X-Tag: second" {
		t.Fatalf("unexpected header serialization: %q, %v", text, err)
	}
}
