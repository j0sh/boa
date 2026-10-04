// Load a typed secret URL from API_URL or --endpoint-file /run/secrets/api_url.
// API_HEADERS or --headers-file supplies comma-separated curl-style headers:
// "Authorization: Bearer token, X-Api-Key: key".
// Use printf '%s' "$API_URL" > api_url to write a file without a trailing newline.
package main

import (
	"fmt"
	"net/url"

	"github.com/j0sh/boa/pkg/boa"
	"github.com/spf13/cobra"
)

type Params struct {
	ConfigFile   string   `configfile:"true" optional:"true"`
	Endpoint     *url.URL `secret:"true" env:"API_URL" descr:"Private API endpoint"`
	EndpointFile string   `secretfor:"Endpoint" env:"API_URL_FILE" descr:"File containing the private API endpoint"`
	Headers      Headers  `secret:"true" env:"API_HEADERS" optional:"true" descr:"Comma-separated private request headers (K: V)"`
	HeadersFile  string   `secretfor:"Headers" env:"API_HEADERS_FILE" descr:"File containing private request headers"`
}

func endpointCommand() boa.Cmd[Params] {
	return boa.Cmd[Params]{
		Use: "secret-url",
		InitFuncCtx: func(ctx *boa.HookContext, p *Params, _ *cobra.Command) error {
			boa.Param(ctx, &p.Endpoint).SetCustomValidator(func(endpoint *url.URL) error {
				if endpoint == nil || endpoint.Scheme != "https" || endpoint.Host == "" {
					return fmt.Errorf("endpoint must be an absolute HTTPS URL")
				}
				return nil
			})
			return nil
		},
		RunFunc: func(p *Params, cmd *cobra.Command, _ []string) {
			// Use p.Endpoint and http.Header(p.Headers) to construct an API client.
			// Report only the number of header names, never their secret values.
			cmd.Printf("Private API endpoint loaded with %d header names.\n", len(p.Headers))
		},
	}
}

func main() { endpointCommand().Run() }
