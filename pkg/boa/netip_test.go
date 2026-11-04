package boa

import (
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Tests for IP address types.

func TestHTTPBindText(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"", ""},
		{":8080", "127.0.0.1:8080"},
		{":1", "127.0.0.1:1"},
		{":65535", "127.0.0.1:65535"},
		{"127.0.0.2:8081", "127.0.0.2:8081"},
		{"[::1]:8082", "[::1]:8082"},
		{"0.0.0.0:8080", "0.0.0.0:8080"},
		{"[::]:8080", "[::]:8080"},
		{"10.0.0.1:8080", "10.0.0.1:8080"},
		{"[::ffff:127.0.0.1]:8080", "[::ffff:127.0.0.1]:8080"},
		{"unsafe@0.0.0.0:8080", "unsafe@0.0.0.0:8080"},
		{"unsafe@[::1]:8080", "unsafe@[::1]:8080"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			// Start with an address so empty input must clear an existing value.
			bind := HTTPBind{AddrPort: netip.MustParseAddrPort("127.0.0.1:9000"), acknowledged: true}
			if err := bind.UnmarshalText([]byte(tc.input)); err != nil {
				t.Fatal(err)
			}
			if bind.IsValid() != (tc.want != "") {
				t.Fatalf("IsValid() = %v for %q", bind.IsValid(), tc.input)
			}
			if tc.want != "" && bind.String() != strings.TrimPrefix(tc.want, "unsafe@") {
				t.Fatalf("String() = %q, want %q", bind.String(), tc.want)
			}
			text, err := bind.MarshalText()
			if err != nil || string(text) != tc.want {
				t.Fatalf("MarshalText() = %q, %v; want %q", text, err, tc.want)
			}
			var roundTrip HTTPBind
			if err := roundTrip.UnmarshalText(text); err != nil || roundTrip != bind {
				t.Fatalf("round trip = %v, %v; want %v", roundTrip, err, bind)
			}
		})
	}

	var disabled HTTPBind
	if err := disabled.UnmarshalText(nil); err != nil || disabled.IsValid() {
		t.Fatalf("nil input = %v, %v; want disabled", disabled, err)
	}
}

func TestHTTPBindInvalid(t *testing.T) {
	for _, input := range []string{
		"true", "127.0.0.1", "127.0.0.1:", ":0", ":65536", ":http",
		"localhost:8080", "::1:8080", "127.0.0.1:0", "[::1]:0", " :8080",
		"unsafe@", "unsafe@unsafe@0.0.0.0:8080", "unsafe@:0", "unsafe@localhost:8080",
	} {
		t.Run(input, func(t *testing.T) {
			original := HTTPBind{AddrPort: netip.MustParseAddrPort("127.0.0.1:9000"), acknowledged: true}
			bind := original
			if err := bind.UnmarshalText([]byte(input)); err == nil {
				t.Fatalf("accepted %q", input)
			}
			if bind != original {
				t.Fatalf("error changed bind to %v, want %v", bind, original)
			}
		})
	}
}

func TestHTTPBindSources(t *testing.T) {
	type Params struct {
		ConfigFile string   `configfile:"true" optional:"true"`
		Bind       HTTPBind `env:"BOA_TEST_HTTP_BIND" default:":8000" json:"bind"`
	}
	for _, tc := range []struct {
		name, config, env string
		args              []string
		want              string
		invalid, allow    bool
	}{
		{name: "default", want: "127.0.0.1:8000"},
		{name: "config", config: `{"bind":":8001"}`, want: "127.0.0.1:8001"},
		{name: "env over config", config: `{"bind":":8001"}`, env: ":8002", want: "127.0.0.1:8002"},
		{name: "cli over env", config: `{"bind":":8001"}`, env: ":8002", args: []string{"--bind", ":8003"}, want: "127.0.0.1:8003"},
		{name: "empty config clears default", config: `{"bind":""}`},
		{name: "empty cli clears env and config", config: `{"bind":":8001"}`, env: ":8002", args: []string{"--bind="}},
		{name: "invalid config", config: `{"bind":":0"}`, invalid: true},
		{name: "invalid env", env: "localhost:8002", invalid: true},
		{name: "invalid cli", args: []string{"--bind", ":65536"}, invalid: true},
		{name: "exposed config", config: `{"bind":"0.0.0.0:8001"}`, invalid: true},
		{name: "exposed env", env: "10.0.0.1:8002", invalid: true},
		{name: "exposed cli", args: []string{"--bind", "[::]:8003"}, invalid: true},
		{name: "allowed config", config: `{"bind":"0.0.0.0:8001"}`, allow: true, want: "0.0.0.0:8001"},
		{name: "acknowledged env", env: "unsafe@10.0.0.1:8002", want: "unsafe@10.0.0.1:8002"},
		{name: "acknowledged cli", args: []string{"--bind", "unsafe@[::]:8003"}, want: "unsafe@[::]:8003"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BOA_TEST_HTTP_BIND", tc.env)
			var p Params
			if tc.config != "" {
				p.ConfigFile = writeTestConfigFile(t, tc.config)
			}
			err := (Cmd[Params]{Params: &p, RawArgs: append([]string{}, tc.args...), RejectUnknown: true, UnsafeHTTPBind: tc.allow}).Validate()
			if tc.invalid {
				if err == nil {
					t.Fatal("expected invalid bind to fail validation")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			text, err := p.Bind.MarshalText()
			if err != nil || string(text) != tc.want {
				t.Fatalf("bind = %q, %v; want %q", text, err, tc.want)
			}
		})
	}
}

func TestHTTPBindPolicy(t *testing.T) {
	type Params struct {
		Bind    HTTPBind   `default:"0.0.0.0:8000"`
		Binds   []HTTPBind `default:":8001"`
		Named   map[string]HTTPBind
		Pointer *HTTPBind
		Nested  [][]HTTPBind
	}
	yes, no := true, false
	for _, tc := range []struct {
		name     string
		allow    bool
		override *bool
		args     []string
		reject   bool
	}{
		{name: "default", reject: true},
		{name: "command allows", allow: true},
		{name: "field allows", override: &yes},
		{name: "field guards", allow: true, override: &no, reject: true},
		{name: "field isolation", override: &yes, args: []string{"--binds=0.0.0.0:8001"}, reject: true},
		{name: "slice", args: []string{"--bind=:8000", "--binds=0.0.0.0:8001"}, reject: true},
		{name: "map", args: []string{"--bind=:8000", "--named=public=0.0.0.0:8001"}, reject: true},
		{name: "pointer", args: []string{"--bind=:8000", "--pointer=0.0.0.0:8001"}, reject: true},
		{name: "nested", args: []string{"--bind=:8000", `--nested=[["0.0.0.0:8001"]]`}, reject: true},
		{name: "acknowledged collections", args: []string{"--bind=:8000", "--binds=unsafe@0.0.0.0:8001", "--named=public=unsafe@0.0.0.0:8001", "--pointer=unsafe@0.0.0.0:8001", `--nested=[["unsafe@0.0.0.0:8001"]]`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := Cmd[Params]{RawArgs: append([]string{}, tc.args...), UnsafeHTTPBind: tc.allow,
				InitFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command) error {
					if tc.override != nil {
						Param(ctx, &p.Bind).SetUnsafeHTTPBind(*tc.override)
					}
					return nil
				},
			}
			if err := cmd.Validate(); (err != nil) != tc.reject {
				t.Fatalf("Validate() = %v, reject = %v", err, tc.reject)
			}
		})
	}
}

func TestHTTPBindPreValidateReload(t *testing.T) {
	type Params struct {
		Bind HTTPBind `default:":8000"`
	}
	dir := t.TempDir()
	for _, allow := range []bool{false, true} {
		path := writeReloadFile(t, dir, "config.json", `{"Bind":":8000"}`)
		cmd := Cmd[Params]{RawArgs: []string{}, UnsafeHTTPBind: allow,
			PreValidateFunc: func(p *Params, _ *cobra.Command, _ []string) error { return LoadConfigFile(path, p, nil) },
		}
		cmd.RunFuncCtx = func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) {
			for _, input := range []string{"0.0.0.0:8001", "unsafe@0.0.0.0:8001"} {
				writeReloadFile(t, dir, "config.json", `{"Bind":"`+input+`"}`)
				fresh, err := Reload[Params](ctx)
				reject := !allow && !strings.HasPrefix(input, "unsafe@")
				if (err != nil) != reject || err == nil && fresh.Bind.String() != "0.0.0.0:8001" {
					t.Fatalf("reload = %v, %v; reject = %v", fresh, err, reject)
				}
			}
			if p.Bind.String() != "127.0.0.1:8000" {
				t.Fatalf("reload mutated original: %v", p.Bind)
			}
		}
		if err := cmd.RunE(); err != nil {
			t.Fatal(err)
		}
		writeReloadFile(t, dir, "config.json", `{"Bind":"0.0.0.0:8001"}`)
		if err := cmd.Validate(); (err != nil) != !allow {
			t.Fatalf("PreValidate config: %v; allow = %v", err, allow)
		}
	}
}

func TestNetIP_Required(t *testing.T) {
	type Params struct {
		Host net.IP `descr:"host IP address"`
	}

	wasRun := false

	Cmd[Params]{
		Use: "test",
		RunFunc: func(p *Params, cmd *cobra.Command, args []string) {
			wasRun = true
			expected := net.ParseIP("192.168.1.1")
			if !p.Host.Equal(expected) {
				t.Errorf("expected %v, got %v", expected, p.Host)
			}
		},
	}.RunArgs([]string{"--host", "192.168.1.1"})

	if !wasRun {
		t.Fatal("run func was not called")
	}
}

func TestNetIP_Optional(t *testing.T) {
	type Params struct {
		Host net.IP `descr:"host IP address" optional:"true"`
	}

	wasRun := false

	Cmd[Params]{
		Use: "test",
		RunFuncCtx: func(ctx *HookContext, p *Params, cmd *cobra.Command, args []string) {
			wasRun = true
			if !ctx.HasValue(&p.Host) {
				t.Error("expected host to have value")
			}
			expected := net.ParseIP("10.0.0.1")
			if !p.Host.Equal(expected) {
				t.Errorf("expected %v, got %v", expected, p.Host)
			}
		},
	}.RunArgs([]string{"--host", "10.0.0.1"})

	if !wasRun {
		t.Fatal("run func was not called")
	}
}

func TestNetIP_IPv6(t *testing.T) {
	type Params struct {
		Host net.IP `descr:"host IP address"`
	}

	wasRun := false

	Cmd[Params]{
		Use: "test",
		RunFunc: func(p *Params, cmd *cobra.Command, args []string) {
			wasRun = true
			expected := net.ParseIP("::1")
			if !p.Host.Equal(expected) {
				t.Errorf("expected %v, got %v", expected, p.Host)
			}
		},
	}.RunArgs([]string{"--host", "::1"})

	if !wasRun {
		t.Fatal("run func was not called")
	}
}

func TestNetIP_IPv6Full(t *testing.T) {
	type Params struct {
		Host net.IP `descr:"host IP address"`
	}

	wasRun := false

	Cmd[Params]{
		Use: "test",
		RunFunc: func(p *Params, cmd *cobra.Command, args []string) {
			wasRun = true
			expected := net.ParseIP("2001:0db8:85a3:0000:0000:8a2e:0370:7334")
			if !p.Host.Equal(expected) {
				t.Errorf("expected %v, got %v", expected, p.Host)
			}
		},
	}.RunArgs([]string{"--host", "2001:0db8:85a3:0000:0000:8a2e:0370:7334"})

	if !wasRun {
		t.Fatal("run func was not called")
	}
}

func TestNetIP_Raw(t *testing.T) {
	type Params struct {
		Host net.IP `descr:"host IP address" optional:"true"`
	}

	wasRun := false

	Cmd[Params]{
		Use: "test",
		RunFunc: func(p *Params, cmd *cobra.Command, args []string) {
			wasRun = true
			expected := net.ParseIP("172.16.0.1")
			if !p.Host.Equal(expected) {
				t.Errorf("expected %v, got %v", expected, p.Host)
			}
		},
	}.RunArgs([]string{"--host", "172.16.0.1"})

	if !wasRun {
		t.Fatal("run func was not called")
	}
}

func TestNetIP_EnvVar(t *testing.T) {
	type Params struct {
		Host net.IP `descr:"host IP address" env:"TEST_HOST_IP"`
	}

	t.Setenv("TEST_HOST_IP", "8.8.8.8")

	wasRun := false

	Cmd[Params]{
		Use: "test",
		RunFunc: func(p *Params, cmd *cobra.Command, args []string) {
			wasRun = true
			expected := net.ParseIP("8.8.8.8")
			if !p.Host.Equal(expected) {
				t.Errorf("expected %v, got %v", expected, p.Host)
			}
		},
	}.RunArgs([]string{})

	if !wasRun {
		t.Fatal("run func was not called")
	}
}

func TestNetIP_ParseFormats(t *testing.T) {
	type Params struct {
		Addr net.IP `descr:"IP address"`
	}

	testCases := []struct {
		input    string
		expected net.IP
	}{
		{"127.0.0.1", net.ParseIP("127.0.0.1")},
		{"0.0.0.0", net.ParseIP("0.0.0.0")},
		{"255.255.255.255", net.ParseIP("255.255.255.255")},
		{"::1", net.ParseIP("::1")},
		{"::", net.ParseIP("::")},
		{"fe80::1", net.ParseIP("fe80::1")},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			wasRun := false

			Cmd[Params]{
				Use: "test",
				RunFunc: func(p *Params, cmd *cobra.Command, args []string) {
					wasRun = true
					if !p.Addr.Equal(tc.expected) {
						t.Errorf("expected %v, got %v", tc.expected, p.Addr)
					}
				},
			}.RunArgs([]string{"--addr", tc.input})

			if !wasRun {
				t.Fatal("run func was not called")
			}
		})
	}
}
