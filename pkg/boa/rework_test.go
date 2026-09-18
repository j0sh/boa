package boa

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestReworkConfigTargetFollowsFieldPath(t *testing.T) {
	type Child struct {
		Value string `optional:"true"`
	}
	type Params struct {
		Child      Child
		ConfigFile string `configfile:"true"`
		Host       string `optional:"true"`
	}
	p := Params{}
	err := (Cmd[Params]{Use: "test", Params: &p, RunFunc: func(*Params, *cobra.Command, []string) {}}).
		RunArgsE([]string{"--config-file", writeTestConfigFile(t, `{"Host":"configured"}`)})
	if err != nil || p.Host != "configured" {
		t.Fatalf("Host=%q error=%v", p.Host, err)
	}
}

type reworkChild struct {
	Calls int    `boa:"ignore"`
	Value string `optional:"true"`
}

func (p *reworkChild) Init() error { p.Calls++; return nil }

type reworkRoot struct {
	Calls int `boa:"ignore"`
	Child reworkChild
}

func (p *reworkRoot) Init() error { p.Calls++; return nil }

func TestReworkInitRunsOnEachReceiverOnce(t *testing.T) {
	p := reworkRoot{}
	_, err := (Cmd[reworkRoot]{Use: "test", Params: &p}).ToCobraE()
	if err != nil || p.Calls != 1 || p.Child.Calls != 1 {
		t.Fatalf("root calls=%d child calls=%d error=%v", p.Calls, p.Child.Calls, err)
	}
}

type reworkActions struct {
	Actions int `boa:"ignore"`
	Value   int `default:"1"`
}

func (p *reworkActions) PreExecute() error                { p.Actions++; return nil }
func (p *reworkActions) PreExecuteCtx(*HookContext) error { p.Actions++; return nil }

func TestReworkReloadSkipsAllActions(t *testing.T) {
	commandActions := 0
	err := (Cmd[reworkActions]{
		Use:            "test",
		PreExecuteFunc: func(*reworkActions, *cobra.Command, []string) error { commandActions++; return nil },
		RunFuncCtx: func(ctx *HookContext, p *reworkActions, _ *cobra.Command, _ []string) {
			fresh, err := Reload[reworkActions](ctx)
			if err != nil {
				t.Fatal(err)
			}
			if fresh.Actions != 0 || p.Actions != 2 || commandActions != 1 {
				t.Fatalf("fresh actions=%d original=%d command=%d", fresh.Actions, p.Actions, commandActions)
			}
		},
	}).RunArgsE([]string{})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReworkValidateAcceptsEveryRunnerAndSkipsActions(t *testing.T) {
	for _, variant := range []string{"RunFunc", "RunFuncE", "RunFuncCtx", "RunFuncCtxE"} {
		t.Run(variant, func(t *testing.T) {
			p := reworkActions{}
			b := Cmd[reworkActions]{Use: "test", Params: &p, RawArgs: []string{},
				PreExecuteFunc: func(*reworkActions, *cobra.Command, []string) error { t.Error("command action ran"); return nil },
			}
			switch variant {
			case "RunFunc":
				b.RunFunc = func(*reworkActions, *cobra.Command, []string) { t.Error("runner ran") }
			case "RunFuncE":
				b.RunFuncE = func(*reworkActions, *cobra.Command, []string) error { t.Error("runner ran"); return nil }
			case "RunFuncCtx":
				b.RunFuncCtx = func(*HookContext, *reworkActions, *cobra.Command, []string) { t.Error("runner ran") }
			case "RunFuncCtxE":
				b.RunFuncCtxE = func(*HookContext, *reworkActions, *cobra.Command, []string) error { t.Error("runner ran"); return nil }
			}
			if err := b.Validate(); err != nil {
				t.Fatal(err)
			}
			if p.Value != 1 || p.Actions != 0 {
				t.Fatalf("params=%+v", p)
			}
		})
	}
}

func TestReworkHelpOmitsFalseBooleanDefaults(t *testing.T) {
	type Params struct {
		Quiet   bool
		Enabled bool `default:"true"`
	}
	cmd, err := (Cmd[Params]{Use: "test"}).ToCobraE()
	if err != nil {
		t.Fatal(err)
	}
	help := cmd.UsageString()
	if strings.Contains(help, "(default false)") || !strings.Contains(help, "(default true)") {
		t.Fatalf("unexpected help:\n%s", help)
	}
}

func TestReworkEnumCompletionSuppressesFiles(t *testing.T) {
	type Params struct {
		Mode string `alts:"fast,slow" optional:"true"`
	}
	for _, dynamic := range []bool{false, true} {
		cmd, err := (Cmd[Params]{Use: "test", InitFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command) error {
			if dynamic {
				Param(ctx, &p.Mode).SetAlternativesFunc(func(*cobra.Command, []string, string) []string { return []string{"dynamic"} })
			}
			return nil
		}}).ToCobraE()
		if err != nil {
			t.Fatal(err)
		}
		complete, ok := cmd.GetFlagCompletionFunc("mode")
		if !ok {
			t.Fatal("missing completion")
		}
		values, directive := complete(cmd, nil, "")
		want := []string{"fast", "slow"}
		if dynamic {
			want = []string{"dynamic"}
		}
		if !reflect.DeepEqual(values, want) || directive&cobra.ShellCompDirectiveNoFileComp == 0 {
			t.Fatalf("values=%v directive=%v", values, directive)
		}
	}
}

func TestReworkMisspelledSubcommandSuggestsMatch(t *testing.T) {
	err := (Cmd[NoParams]{Use: "app", SubCmds: []*cobra.Command{{Use: "status", Run: func(*cobra.Command, []string) {}}}}).
		RunArgsE([]string{"statsu"})
	if err == nil || !strings.Contains(err.Error(), "Did you mean") || !strings.Contains(err.Error(), "status") {
		t.Fatalf("expected command suggestion, got %v", err)
	}
}

func TestReworkReloadCapturesLeafInvocationAndPreservesTree(t *testing.T) {
	type Params struct {
		Names []string `collection:"array" optional:"true"`
		Arg   string   `positional:"true"`
	}
	var context *HookContext
	var original *Params
	child := (Cmd[Params]{Use: "serve", RunFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) { context, original = ctx, p }}).ToCobra()
	root := &cobra.Command{Use: "root"}
	root.AddCommand(child)
	root.SetArgs([]string{"serve", "--names", "a,b", "--names", "c", "input"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	oldArgs := os.Args
	os.Args = []string{"different-program", "--invalid"}
	t.Cleanup(func() { os.Args = oldArgs })
	fresh, err := Reload[Params](context)
	if err != nil {
		t.Fatal(err)
	}
	if fresh == original || fresh.Arg != "input" || !reflect.DeepEqual(fresh.Names, []string{"a,b", "c"}) {
		t.Fatalf("fresh=%+v", fresh)
	}
	if child.Parent() != root {
		t.Fatal("reload reparented original command")
	}
	fresh.Names[0] = "changed after reload"
	again, err := Reload[Params](context)
	if err != nil || again.Names[0] != "a,b" {
		t.Fatalf("replay did not retain the original CLI values: %+v, %v", again, err)
	}

}

func TestReworkReloadRefreshesWatchedFiles(t *testing.T) {
	type Params struct {
		Config string `configfile:"true" env:"BOA_RELOAD_CONFIG"`
		Value  int    `optional:"true"`
	}
	first := writeTestConfigFile(t, `{"Value":1}`)
	second := writeTestConfigFile(t, `{"Value":2}`)
	t.Setenv("BOA_RELOAD_CONFIG", first)
	var ctx *HookContext
	command := Cmd[Params]{Use: "test", RunFuncCtx: func(c *HookContext, _ *Params, _ *cobra.Command, _ []string) { ctx = c }}
	if err := command.RunArgsE([]string{}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOA_RELOAD_CONFIG", second)
	fresh, err := Reload[Params](ctx)
	if err != nil || fresh.Value != 2 {
		t.Fatalf("fresh=%+v err=%v", fresh, err)
	}
	if !reflect.DeepEqual(ctx.WatchedConfigFiles(), []string{second}) {
		t.Fatalf("watched=%v", ctx.WatchedConfigFiles())
	}
	if err := os.Remove(second); err != nil {
		t.Fatal(err)
	}
	if _, err := Reload[Params](ctx); err == nil {
		t.Fatal("missing file accepted")
	}
	if !reflect.DeepEqual(ctx.WatchedConfigFiles(), []string{second}) {
		t.Fatal("failed reload changed watch list")
	}
}

func TestReworkConcurrentReloadsUseIndependentState(t *testing.T) {
	type Params struct {
		Counts map[string]float64 `default:"a=1.5"`
	}
	var contexts []*HookContext
	for range 2 {
		err := (Cmd[Params]{Use: "test", RunFuncCtx: func(ctx *HookContext, _ *Params, _ *cobra.Command, _ []string) { contexts = append(contexts, ctx) }}).RunArgsE([]string{})
		if err != nil {
			t.Fatal(err)
		}
	}
	results := make(chan error, 12)
	for i := range 12 {
		go func() {
			p, err := Reload[Params](contexts[i%2])
			if err == nil && p.Counts["a"] != 1.5 {
				err = fmt.Errorf("unexpected values: %v", p.Counts)
			}
			_ = contexts[i%2].WatchedConfigFiles()
			results <- err
		}()
	}
	for range 12 {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
}

func TestReworkValidateDoesNotRunOrReparentChildren(t *testing.T) {
	child := &cobra.Command{Use: "child", Run: func(*cobra.Command, []string) { t.Error("child ran") }}
	parent := &cobra.Command{Use: "original"}
	parent.AddCommand(child)
	if err := (Cmd[NoParams]{Use: "test", SubCmds: []*cobra.Command{child}, RawArgs: []string{}}).Validate(); err != nil {
		t.Fatal(err)
	}
	if child.Parent() != parent {
		t.Fatal("Validate reparented child")
	}
}

func TestReworkReassignedGroupsKeepConfigPresence(t *testing.T) {
	type Inner struct {
		Value int `optional:"true"`
	}
	type Group struct{ Inner *Inner }
	type Params struct {
		Group  *Group
		Config string `configfile:"true"`
	}
	p := Params{}
	command := Cmd[Params]{Use: "test", Params: &p, RawArgs: []string{"--config", writeTestConfigFile(t, `{"Group":{"Inner":{}}}`)},
		PostCreateFunc: func(p *Params, _ *cobra.Command) error { p.Group = &Group{Inner: &Inner{}}; return nil },
	}
	if err := command.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.Group == nil || p.Group.Inner == nil {
		t.Fatalf("explicit group was cleared: %+v", p)
	}
}

func TestReworkRecursiveGroupsReturnConstructionError(t *testing.T) {
	type Node struct{ Next *Node }
	_, err := (Cmd[Node]{Use: "test"}).ToCobraE()
	if err == nil || !strings.Contains(err.Error(), "recursive parameter group") {
		t.Fatalf("error=%v", err)
	}
}
