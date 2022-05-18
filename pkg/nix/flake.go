package nix

import (
	"fmt"
	"os/exec"
	"strings"
)

// Wrapper around Nix commands for a flake
type Flake struct {
	path    string
	options Opts
}

func NewFlake(path string, options Opts) Flake {
	// temp hack
	options.Binary = "nix"

	return Flake{
		path:    path,
		options: options,
	}
}

func (f Flake) invokeNix(args ...string) (string, error) {
	// append our flake overrides to the args
	for key, path := range f.options.FlakeOverrides {
		args = append(args, []string{"--override-input", key, path}...)
	}

	out, err := exec.Command(f.options.Binary, args...).Output()

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			fullCmd := strings.Join(args, " ")
			return "", fmt.Errorf("invoking Nix with parameters `%s`:\n%s", fullCmd, exitError.Stderr)
		} else {
			return "", err
		}
	}

	return string(out), nil
}

// Evaluates a Nix expression
func (f Flake) Eval(key string) (string, error) {
	uri := fmt.Sprintf("%s#%s", f.path, key)
	args := []string{"eval", uri, "--json"}
	out, err := f.invokeNix(args...)
	if err != nil {
		return "", err
	}

	return out, nil
}
