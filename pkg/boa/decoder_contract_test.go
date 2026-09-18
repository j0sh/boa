package boa

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

type validatedJSONConfig struct {
	Timeout time.Duration `json:"timeout"`
	Limit   int           `json:"limit"`
	Calls   int           `json:"-"`
}

func (v *validatedJSONConfig) UnmarshalJSON(data []byte) error {
	v.Calls++
	type plain validatedJSONConfig
	if err := UnmarshalJSON(data, (*plain)(v)); err != nil {
		return err
	}
	if v.Limit > 10 {
		return errors.New("limit exceeds 10")
	}
	return nil
}

func TestJSONCustomValidationRunsOnce(t *testing.T) {
	for _, limit := range []int{10, 11} {
		var p validatedJSONConfig
		err := LoadConfigBytes(fmt.Appendf(nil, `{"timeout":"2.5h","limit":%d}`, limit), ".json", &p, nil)
		if p.Calls != 1 {
			t.Fatalf("decoder called %d times", p.Calls)
		}
		if limit == 11 && (err == nil || !strings.Contains(err.Error(), "limit exceeds 10")) {
			t.Fatalf("validation lost: %v", err)
		}
		if limit == 10 && (err != nil || p.Timeout != 150*time.Minute) {
			t.Fatalf("p=%+v err=%v", p, err)
		}
	}
}

func TestCustomConfigDecoderIsAuthoritative(t *testing.T) {
	sentinel := errors.New("decoder rejected document")
	calls := 0
	decoder := func(data []byte, target any) error {
		calls++
		_ = json.Unmarshal(data, target)
		return sentinel
	}
	var p struct{ Timeout time.Duration }
	err := LoadConfigBytes([]byte(`{"Timeout":"2.5h"}`), ".custom", &p, decoder)
	if !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}

func TestJSONHooksComposeWithStrictDecoder(t *testing.T) {
	decode := func(data []byte, target any) error {
		return jsonv2.Unmarshal(data, target, jsonv2.RejectUnknownMembers(true), jsonv2.WithUnmarshalers(JSONUnmarshalers()))
	}
	var p struct {
		Timeout time.Duration `json:"timeout"`
	}
	if err := LoadConfigBytes([]byte(`{"timeout":"2.5h"}`), ".json", &p, decode); err != nil || p.Timeout != 150*time.Minute {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	if err := LoadConfigBytes([]byte(`{"timeout":"2.5h","unknown":11}`), ".json", &p, decode); err == nil {
		t.Fatal("strict decoding bypassed")
	}
}

func TestTextUsesOneParserAcrossSources(t *testing.T) {
	type Params struct {
		Timeout Text[time.Duration] `env:"BOA_TEST_TIMEOUT" default:"1h"`
	}
	for _, source := range []string{"default", "env", "cli", "config"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("BOA_TEST_TIMEOUT", "")
			p := Params{}
			command := Cmd[Params]{Use: "test", Params: &p, RawArgs: []string{}}
			switch source {
			case "default":
			case "env":
				t.Setenv("BOA_TEST_TIMEOUT", "2.5h")
			case "cli":
				command.RawArgs = []string{"--timeout", "2.5h"}
			case "config":
				if err := LoadConfigBytes([]byte(`{"Timeout":"2.5h"}`), ".json", &p, json.Unmarshal); err != nil {
					t.Fatal(err)
				}
			}
			if err := command.Validate(); err != nil {
				t.Fatal(err)
			}
			want := 150 * time.Minute
			if source == "default" {
				want = time.Hour
			}
			if p.Timeout.Value != want {
				t.Fatalf("timeout=%v want=%v", p.Timeout.Value, want)
			}
		})
	}
}

func TestRegisteredPointerTypeOwnsItsParsing(t *testing.T) {
	type Point struct{ X int }
	registerTypeCleanup(t, TypeDef[*Point]{
		Parse: func(text string) (*Point, error) {
			p := new(Point)
			_, err := fmt.Sscanf(text, "x=%d", &p.X)
			return p, err
		},
		Format: func(p *Point) string { return fmt.Sprintf("x=%d", p.X) },
	})
	type Params struct {
		Point *Point `default:"x=1" env:"BOA_POINT"`
	}
	for _, source := range []string{"default", "env", "cli", "config"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("BOA_POINT", "")
			p := Params{}
			command := Cmd[Params]{Use: "test", Params: &p, RawArgs: []string{}}
			switch source {
			case "env":
				t.Setenv("BOA_POINT", "x=2")
			case "cli":
				command.RawArgs = []string{"--point", "x=2"}
			case "config":
				if err := LoadConfigBytes([]byte(`{"Point":"x=2"}`), ".json", &p, nil); err != nil {
					t.Fatal(err)
				}
			}
			if err := command.Validate(); err != nil {
				t.Fatal(err)
			}
			want := 2
			if source == "default" {
				want = 1
			}
			if p.Point == nil || p.Point.X != want {
				t.Fatalf("point=%+v want x=%d", p.Point, want)
			}
		})
	}
}

func TestPreValidateSeesParsedTextValues(t *testing.T) {
	type Params struct {
		At      time.Time
		Timeout Text[time.Duration]
	}
	err := (Cmd[Params]{Use: "test", RawArgs: []string{"--at", "2026-09-17", "--timeout", "2.5h"},
		PreValidateFunc: func(p *Params, _ *cobra.Command, _ []string) error {
			if p.At.Format(time.DateOnly) != "2026-09-17" || p.Timeout.Value != 150*time.Minute {
				return fmt.Errorf("hook saw unparsed fields: %+v", p)
			}
			return nil
		},
	}).Validate()
	if err != nil {
		t.Fatal(err)
	}
}
