package tanka

import (
	"fmt"
	"log"

	"github.com/fatih/color"

	"github.com/grafana/tanka/pkg/kubernetes"
	"github.com/grafana/tanka/pkg/kubernetes/client"
	"github.com/grafana/tanka/pkg/kubernetes/manifest"
	"github.com/grafana/tanka/pkg/term"
)

// ApplyOpts specify additional properties for the Apply action
type ApplyOpts struct {
	Opts

	// AutoApprove skips the interactive approval
	AutoApprove bool
	// DiffStrategy to use for printing the diff before approval
	DiffStrategy string
	// ApplyStrategy decides how kubectl will apply the manifest
	ApplyStrategy string
	// Force ignores any warnings kubectl might have
	Force bool
	// Validate set to false ignores invalid Kubernetes schemas
	Validate bool
	// DryRun string passed to kubectl as --dry-run=<DryRun>
	DryRun string
	// ServerSide bool passed to kubectl as --server-side
	ServerSide bool
}

// ErrorApplyStrategyUnknown occurs when an apply-strategy is requested that does
// not exist. Unlike ErrorDiffStrategyUnknown, this needs to be used before things
// reach the `kube.Apply` function.
type ErrorApplyStrategyUnknown struct {
	Requested string
}

func (e ErrorApplyStrategyUnknown) Error() string {
	return fmt.Sprintf("apply strategy `%s` does not exist. Pick one of: [server, client].", e.Requested)
}

// Apply parses the environment at the given directory (a `baseDir`) and applies
// the evaluated jsonnet to the Kubernetes cluster defined in the environments
// `spec.json`.
func Apply(baseDir string, opts ApplyOpts) error {
	l, err := Load(baseDir, opts.Opts)
	if err != nil {
		return err
	}

	// If the apply strategy was not set on the command-line, draw from spec or use default
	if opts.ApplyStrategy == "" {
		if l.Env.Spec.ApplyStrategy != "" {
			opts.ApplyStrategy = l.Env.Spec.ApplyStrategy
		} else {
			opts.ApplyStrategy = "client"
		}
	}
	if opts.ApplyStrategy != "client" && opts.ApplyStrategy != "server" {
		return ErrorApplyStrategyUnknown{Requested: opts.ApplyStrategy}
	}

	// Default to `server` diff in server apply mode
	if opts.ApplyStrategy == "server" && opts.DiffStrategy == "" && l.Env.Spec.DiffStrategy == "" {
		l.Env.Spec.DiffStrategy = "server"
	}

	kube, err := l.Connect()
	if err != nil {
		return err
	}
	defer kube.Close()

	if opts.DiffStrategy != "none" {
		// show diff
		diff, err := kube.Diff(l.Resources, kubernetes.DiffOpts{Strategy: opts.DiffStrategy})
		switch {
		case err != nil:
			// This is not fatal, the diff is not strictly required
			log.Println("Error diffing:", err)
		case diff == nil:
			tmp := "Warning: There are no differences. Your apply may not do anything at all."
			diff = &tmp
		}

		// in case of non-fatal error diff may be nil
		if diff != nil {
			b := term.Colordiff(*diff)
			fmt.Print(b.String())
		}
	}

	// prompt for confirmation
	if opts.AutoApprove || opts.DryRun != "" {
	} else if err := confirmPrompt("Applying to", l.Env.Spec.Namespace, kube.Info()); err != nil {
		return err
	}

	return kube.Apply(l.Resources, kubernetes.ApplyOpts{
		Force:         opts.Force,
		Validate:      opts.Validate,
		DryRun:        opts.DryRun,
		ApplyStrategy: opts.ApplyStrategy,
	})
}

// confirmPrompt asks the user for confirmation before apply
func confirmPrompt(action, namespace string, info client.Info) error {
	alert := color.New(color.FgRed, color.Bold).SprintFunc()

	return term.Confirm(
		fmt.Sprintf(`%s namespace '%s' of cluster '%s' at '%s' using context '%s'.`, action,
			alert(namespace),
			alert(info.Kubeconfig.Cluster.Name),
			alert(info.Kubeconfig.Cluster.Cluster.Server),
			alert(info.Kubeconfig.Context.Name),
		),
		"yes",
	)
}

// DiffOpts specify additional properties for the Diff action
type DiffOpts struct {
	Opts

	// Strategy must be one of "native", "validate", "subset" or "server"
	Strategy string
	// Summarize prints a summary, instead of the actual diff
	Summarize bool
	// WithPrune includes objects to be deleted by prune command in the diff
	WithPrune bool
	// Exit with 0 even when differences are found
	ExitZero bool
}

// Diff parses the environment at the given directory (a `baseDir`) and returns
// the differences from the live cluster state in `diff(1)` format. If the
// `WithDiffSummarize` modifier is used, a histogram created using `diffstat(1)`
// is returned instead.
// The cluster information is retrieved from the environments `spec.json`.
// NOTE: This function requires on `diff(1)`, `kubectl(1)` and perhaps `diffstat(1)`
func Diff(baseDir string, opts DiffOpts) (*string, error) {
	l, err := Load(baseDir, opts.Opts)
	if err != nil {
		return nil, err
	}
	kube, err := l.Connect()
	if err != nil {
		return nil, err
	}
	defer kube.Close()

	return kube.Diff(l.Resources, kubernetes.DiffOpts{
		Summarize: opts.Summarize,
		Strategy:  opts.Strategy,
		WithPrune: opts.WithPrune,
	})
}

// DeleteOpts specify additional properties for the Delete operation
type DeleteOpts struct {
	Opts

	// AutoApprove skips the interactive approval
	AutoApprove bool

	// Force ignores any warnings kubectl might have
	Force bool
	// Validate set to false ignores invalid Kubernetes schemas
	Validate bool
	// DryRun string passed to kubectl as --dry-run=<DryRun>
	DryRun string
}

// Delete parses the environment at the given directory (a `baseDir`) and deletes
// the generated objects from the Kubernetes cluster defined in the environment's
// `spec.json`.
func Delete(baseDir string, opts DeleteOpts) error {
	l, err := Load(baseDir, opts.Opts)
	if err != nil {
		return err
	}
	kube, err := l.Connect()
	if err != nil {
		return err
	}
	defer kube.Close()

	if opts.DryRun == "" {
		// show diff
		// static differ will never fail and always return something if input is not nil
		diff, err := kubernetes.StaticDiffer(false)(l.Resources)

		if err != nil {
			fmt.Println("Error diffing:", err)
		}

		// in case of non-fatal error diff may be nil
		if diff != nil {
			b := term.Colordiff(*diff)
			fmt.Print(b.String())
		}
	}

	// prompt for confirmation
	if opts.AutoApprove || opts.DryRun != "" {
	} else if err := confirmPrompt("Deleting from", l.Env.Spec.Namespace, kube.Info()); err != nil {
		return err
	}

	return kube.Delete(l.Resources, kubernetes.DeleteOpts{
		Force:    opts.Force,
		Validate: opts.Validate,
		DryRun:   opts.DryRun,
	})
}

// Show parses the environment at the given directory (a `baseDir`) and returns
// the list of Kubernetes objects.
// Tip: use the `String()` function on the returned list to get the familiar yaml stream
func Show(baseDir string, opts Opts) (manifest.List, error) {
	l, err := Load(baseDir, opts)
	if err != nil {
		return nil, err
	}

	return l.Resources, nil
}
