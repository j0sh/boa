package main

import (
	"errors"
	"testing"

	"github.com/j0sh/boa/pkg/boa"
)

// The pre-extraction baseline covers exactly the six sample scalar rules.
// It is retained only as a test fixture for measuring and verifying reuse.
func legacyDefaults() App {
	return App{
		Readiness: Readiness{Type: "tcp", FailureThreshold: 3},
		Instances: Instances{Mode: "reusable", Replicas: 1, CapacityPerInstance: 1},
	}
}

func legacyValidate(a App) error {
	if a.HTTP.Port < 1 || a.HTTP.Port > 65535 {
		return errors.New("invalid port")
	}
	if a.Readiness.Type != "http" && a.Readiness.Type != "tcp" {
		return errors.New("invalid readiness type")
	}
	if a.Readiness.FailureThreshold < 1 || a.Readiness.FailureThreshold > 10 {
		return errors.New("invalid failure threshold")
	}
	if a.Instances.Mode != "reusable" && a.Instances.Mode != "single-session" {
		return errors.New("invalid instance mode")
	}
	if a.Instances.Replicas < 1 || a.Instances.Replicas > 100 {
		return errors.New("invalid replicas")
	}
	if a.Instances.CapacityPerInstance < 1 || a.Instances.CapacityPerInstance > 10000 {
		return errors.New("invalid capacity")
	}
	return nil
}

func TestAppReuse(t *testing.T) {
	app, err := defaultApp()
	if err != nil || app != legacyDefaults() {
		t.Fatalf("defaults differ: %+v, %v", app, err)
	}
	app.HTTP.Port = 8080
	for _, edit := range []func(*App){
		func(*App) {},
		func(a *App) { a.HTTP.Port = 0 },
		func(a *App) { a.HTTP.Port = 65536 },
		func(a *App) { a.Readiness.Type = "exec" },
		func(a *App) { a.Readiness.FailureThreshold = 0 },
		func(a *App) { a.Readiness.FailureThreshold = 11 },
		func(a *App) { a.Instances.Mode = "bad" },
		func(a *App) { a.Instances.Replicas = -1 },
		func(a *App) { a.Instances.Replicas = 0 },
		func(a *App) { a.Instances.Replicas = 101 },
		func(a *App) { a.Instances.CapacityPerInstance = 0 },
		func(a *App) { a.Instances.CapacityPerInstance = 10001 },
	} {
		value := app
		edit(&value)
		if (validateApp(value) == nil) != (legacyValidate(value) == nil) {
			t.Fatalf("validation differs: %+v", value)
		}
	}
}

func TestKafkaReuse(t *testing.T) {
	p, err := boa.Defaults[KafkaSettings]()
	if err != nil || p.AuthMethod != "scram-sha-512" || p.OutboxDB != "events.sqlite" || p.OutboxMaxBytes != 268435456 {
		t.Fatalf("Kafka defaults: %+v, %v", p, err)
	}
	if err := boa.Validate(p); err != nil {
		t.Fatal(err)
	}
	p.OutboxMaxBytes = 0
	var failures *boa.ValidationError
	if err := boa.Validate(p); !errors.As(err, &failures) || failures.Fields[0].Path != "OutboxMaxBytes" || failures.Fields[0].Code != boa.CodeMin {
		t.Fatalf("missing reused outbox rule: %v", err)
	}
	p.OutboxMaxBytes = 1
	p.AuthMethod = ""
	if err := boa.Validate(p); !errors.As(err, &failures) || failures.Fields[0].Code != boa.CodeAlternatives {
		t.Fatalf("expected strict auth enum: %v", err)
	}
}
