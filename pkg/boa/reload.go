package boa

import (
	"fmt"
	"slices"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// prepareReload snapshots the parsed invocation before source loading and hooks
// can change flag values. Replay builds only this command, never its shared
// Cobra children, and calls the source/validation pipeline directly.
func (b command) prepareReload(ctx *processingContext, cmd *cobra.Command, args []string) {
	if b.newParams == nil {
		return
	}
	args = slices.Clone(args)
	type flagValue struct {
		name, text string
		items      []string
		isSlice    bool
	}
	var flags []flagValue
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		value := flagValue{name: flag.Name, text: flag.Value.String()}
		if slice, ok := flag.Value.(pflag.SliceValue); ok {
			value.items, value.isSlice = slices.Clone(slice.GetSlice()), true
		}
		flags = append(flags, value)
	})
	ctx.reloadFactory = func() (any, error) {
		ctx.reloadMu.Lock()
		defer ctx.reloadMu.Unlock()
		next := b
		next.Params, next.SubCmds, next.RawArgs, next.validateOnly = b.newParams(), nil, []string{}, true
		replay, freshCtx, err := next.toCobraBase()
		if err != nil {
			return nil, err
		}
		for _, value := range flags {
			flag := replay.Flags().Lookup(value.name)
			if flag == nil {
				flag = replay.PersistentFlags().Lookup(value.name)
			}
			if flag == nil {
				continue
			} // Inherited flags belong to another command's params.
			if value.isSlice {
				slice, ok := flag.Value.(pflag.SliceValue)
				if !ok {
					return nil, fmt.Errorf("boa: reload flag %s changed type", value.name)
				}
				err = slice.Replace(slices.Clone(value.items))
			} else {
				err = flag.Value.Set(value.text)
			}
			if err != nil {
				return nil, err
			}
			flag.Changed = true
		}
		// Register persistent flags on the local FlagSet for presence detection.
		replay.Flags().AddFlagSet(replay.PersistentFlags())
		if err := replay.ValidateArgs(args); err != nil {
			return nil, err
		}
		if err := next.loadAndValidate(freshCtx, replay, args); err != nil {
			return nil, err
		}
		ctx.watchMu.Lock()
		ctx.LoadedConfigFiles = slices.Clone(freshCtx.LoadedConfigFiles)
		ctx.ExtraWatchedConfigFiles = slices.Clone(freshCtx.ExtraWatchedConfigFiles)
		ctx.watchMu.Unlock()
		return next.Params, nil
	}
}
