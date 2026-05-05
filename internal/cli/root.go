package cli

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const defaultVersion = "dev"

type runtime struct {
	opts   Options
	viper  *viper.Viper
	config Config
	logger *slog.Logger
}

// ExecuteContext constructs and executes the imgcli root command.
func ExecuteContext(ctx context.Context, opts Options) error {
	cmd, err := NewRootCommand(opts)
	if err != nil {
		return err
	}

	return cmd.ExecuteContext(ctx)
}

// NewRootCommand constructs the imgcli Cobra command tree.
func NewRootCommand(opts Options) (*cobra.Command, error) {
	rt := newRuntime(opts)

	root := &cobra.Command{
		Use:           "imgcli",
		Short:         "Build disk image artifacts from configuration",
		Version:       rt.opts.version(),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return rt.initialize(cmd)
		},
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetIn(rt.opts.stdin())
	root.SetOut(rt.opts.stdout())
	root.SetErr(rt.opts.stderr())

	if err := rt.registerGlobalFlags(root); err != nil {
		return nil, err
	}

	addCommands(
		root,
		newPlanCommand(rt),
		newBuildCommand(rt),
		newPublishCommand(rt),
		newVersionCommand(rt),
	)

	return root, nil
}

func newRuntime(opts Options) *runtime {
	vp := viper.New()
	configureViper(vp)

	return &runtime{
		opts:   opts,
		viper:  vp,
		logger: slog.New(slog.DiscardHandler),
	}
}

func addCommands(root *cobra.Command, commands ...*cobra.Command) {
	root.AddCommand(commands...)
}

func (rt *runtime) initialize(_ *cobra.Command) error {
	cfg, err := loadConfig(rt.viper)
	if err != nil {
		return err
	}

	logger, err := newLogger(cfg, rt.opts.stderr(), rt.opts.environ())
	if err != nil {
		return fmt.Errorf("configure logger: %w", err)
	}

	rt.config = cfg
	rt.logger = logger
	return nil
}
