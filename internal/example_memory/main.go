// Example memory uses representative fields from Clankery's app schema and
// Node's KafkaConfig without depending on, or migrating, either application.
package main

import (
	"fmt"

	"github.com/j0sh/boa/pkg/boa"
)

type HTTP struct {
	Port int `json:"port" min:"1" max:"65535"`
}

type Readiness struct {
	Type             string `json:"type" default:"tcp" alts:"http,tcp"`
	FailureThreshold int    `json:"failure_threshold" default:"3" min:"1" max:"10"`
}

type Instances struct {
	Mode                string `json:"mode" default:"reusable" alts:"reusable,single-session"`
	Replicas            int    `json:"replicas" default:"1" min:"1" max:"100"`
	CapacityPerInstance int    `json:"capacity_per_instance" default:"1" min:"1" max:"10000"`
}

// App is a subset, not a substitute for Clankery's complete schema validator.
type App struct {
	HTTP      HTTP      `json:"http"`
	Readiness Readiness `json:"readiness"`
	Instances Instances `json:"instances"`
}

// KafkaSettings retains the corresponding Node field tags verbatim.
// The real KafkaConfig still needs broker, topic, path and credential checks.
type KafkaSettings struct {
	AuthMethod     string `name:"auth-method" default:"scram-sha-512" alts:"plain,scram-sha-256,scram-sha-512" strict:"true"`
	OutboxDB       string `name:"outbox-db" file:"optional" default:"events.sqlite"`
	OutboxMaxBytes int64  `name:"outbox-max-bytes" default:"268435456" min:"1"`
}

func defaultApp() (App, error) {
	return boa.Defaults[App]()
}

func validateApp(a App) error {
	return boa.Validate(a)
}

func main() {
	app, err := defaultApp()
	if err != nil {
		panic(err)
	}
	// A decoder can replace defaults with explicit zero, false or empty values.
	if err := boa.LoadConfigBytes([]byte(`{"http":{"port":8080},"instances":{"replicas":2}}`), ".json", &app, nil); err != nil {
		panic(err)
	}
	if err := validateApp(app); err != nil {
		panic(err)
	}
	fmt.Printf("%s readiness, %d replicas\n", app.Readiness.Type, app.Instances.Replicas)
	kafka, err := boa.Defaults[KafkaSettings]()
	if err != nil {
		panic(err)
	}
	if err := boa.Validate(kafka); err != nil {
		panic(err)
	}
	fmt.Printf("%s, %d outbox bytes\n", kafka.AuthMethod, kafka.OutboxMaxBytes)
}
