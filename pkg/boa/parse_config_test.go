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
		t.Run(data, func(t *testing.T) {
			manual, err := Defaults[config]()
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(data), &manual); err != nil {
				t.Fatal(err)
			}
			manualErr := Validate(manual)
			calls := 0
			wantPort := 80
			parsed, err := ParseConfigBytes[config]([]byte(data), func(data []byte, dst any) error {
				calls++
				if !reflect.DeepEqual(dst, &config{Count: 3, Enabled: true, Name: "fresh", Port: &wantPort, Mode: "tcp"}) {
					t.Fatalf("decoder did not receive defaults: %+v", dst)
				}
				return json.Unmarshal(data, dst)
			})
			if calls != 1 || !reflect.DeepEqual(parsed, manual) {
				t.Fatalf("pipeline differs: %+v / %+v, calls %d", parsed, manual, calls)
			}
			if manualErr == nil {
				if err != nil {
					t.Fatal(err)
				}
			} else if !reflect.DeepEqual(failures(t, err).Fields, failures(t, manualErr).Fields) || err.Error() != manualErr.Error() {
				t.Fatalf("structured validation error changed: %v / %v", err, manualErr)
			}
		})
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
