package boa

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestParseConfigBytesManualPipeline(t *testing.T) {
	type config struct {
		Count   int    `default:"3" min:"0"`
		Enabled bool   `default:"true"`
		Name    string `default:"fresh"`
		Port    *int   `default:"80" min:"1"`
		Mode    string `default:"tcp" alts:"tcp,http"`
	}
	for _, data := range []string{`{}`, `{"Count":0,"Enabled":false,"Name":"","Port":null}`, `{"Count":-1,"Mode":"bad"}`} {
		manual, err := Defaults[config]()
		requireNoError(t, err)
		requireNoError(t, json.Unmarshal([]byte(data), &manual))
		manualErr := Validate(manual)
		parsed, err := ParseConfigBytes[config]([]byte(data), json.Unmarshal)
		if !reflect.DeepEqual(parsed, manual) || !reflect.DeepEqual(err, manualErr) {
			t.Fatalf("pipeline differs for %s: %+v, %v / %+v, %v", data, parsed, err, manual, manualErr)
		}
	}
}

func TestParseConfigBytesErrors(t *testing.T) {
	type config struct {
		Count int `default:"3"`
	}
	sentinel := errors.New("decoder failure")
	value, err := ParseConfigBytes[config](nil, func(_ []byte, dst any) error { dst.(*config).Count = 4; return sentinel })
	if err != sentinel || value.Count != 4 {
		t.Fatalf("decoder error or partial value changed: %+v, %v", value, err)
	}
	if _, err := ParseConfigBytes[config](nil, nil); err == nil {
		t.Fatal("expected missing decoder error")
	}
	if _, err := ParseConfigBytes[struct {
		Count int `default:"bad"`
	}](nil, func([]byte, any) error { t.Fatal("decoded after default error"); return nil }); err == nil {
		t.Fatal("expected default error")
	}
}
