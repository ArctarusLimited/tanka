package tanka

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"

	"github.com/grafana/tanka/pkg/jsonnet"
	"github.com/grafana/tanka/pkg/jsonnet/jpath"
	"github.com/grafana/tanka/pkg/nix"
)

// EvalCommon detects the evaluation type based on the contents of the directory
func evalDetect(path string, opts LoaderOpts) (raw string, err error) {
	_, err = os.Stat(filepath.Join(path, "flake.nix"))
	if os.IsNotExist(err) {
		return evalJsonnet(path, opts.JsonnetOpts)
	} else if err != nil {
		return "", err
	}

	return evalNix(path, "kube.tanka", opts.Nix)
}

// EvalJsonnet evaluates the jsonnet environment at the given file system path
func evalJsonnet(path string, opts jsonnet.Opts) (raw string, err error) {
	// Can't provide env as extVar, as we need to evaluate Jsonnet first to know it
	opts.ExtCode.Set(environmentExtCode, `error "Using tk.env and std.extVar('tanka.dev/environment') is only supported for static environments. Directly access this data using standard Jsonnet instead."`)

	entrypoint, err := jpath.Entrypoint(path)
	if err != nil {
		return "", err
	}

	// evaluate Jsonnet
	if opts.EvalScript != "" {
		var tla []string
		for k := range opts.TLACode {
			tla = append(tla, k+"="+k)
		}
		evalScript := fmt.Sprintf(`
  local main = (import '%s');
  %s
`, entrypoint, opts.EvalScript)

		if len(tla) != 0 {
			tlaJoin := strings.Join(tla, ", ")
			evalScript = fmt.Sprintf(`
function(%s)
  local main = (import '%s')(%s);
  %s
`, tlaJoin, entrypoint, tlaJoin, opts.EvalScript)
		}

		raw, err = jsonnet.Evaluate(path, evalScript, opts)
		if err != nil {
			return "", errors.Wrap(err, "evaluating jsonnet")
		}
		return raw, nil
	}

	raw, err = jsonnet.EvaluateFile(entrypoint, opts)
	if err != nil {
		return "", errors.Wrap(err, "evaluating jsonnet")
	}
	return raw, nil
}

// EvalNix evaluates the Nix flake at the given file system path
func evalNix(path string, expr string, opts nix.Opts) (raw string, err error) {
	return nix.EvalFlake(path, expr, opts)
}

const PatternEvalScript = "main.%s"

// MetadataEvalScript finds the Environment object (without its .data object)
const MetadataEvalScript = `
local noDataEnv(object) =
  std.prune(
    if std.isObject(object)
    then
      if std.objectHas(object, 'apiVersion')
         && std.objectHas(object, 'kind')
      then
        if object.kind == 'Environment'
        then object { data+:: {} }
        else {}
      else
        std.mapWithKey(
          function(key, obj)
            noDataEnv(obj),
          object
        )
    else if std.isArray(object)
    then
      std.map(
        function(obj)
          noDataEnv(obj),
        object
      )
    else {}
  );

noDataEnv(main)
`

// MetadataSingleEnvEvalScript returns a Single Environment object
const MetadataSingleEnvEvalScript = `
local singleEnv(object) =
  std.prune(
    if std.isObject(object)
    then
      if std.objectHas(object, 'apiVersion')
         && std.objectHas(object, 'kind')
      then
        if object.kind == 'Environment'
        && object.metadata.name == '%s'
        then object { data:: super.data }
        else {}
      else
        std.mapWithKey(
          function(key, obj)
            singleEnv(obj),
          object
        )
    else if std.isArray(object)
    then
      std.map(
        function(obj)
          singleEnv(obj),
        object
      )
    else {}
  );

singleEnv(main)
`

// SingleEnvEvalScript returns a Single Environment object
const SingleEnvEvalScript = `
local singleEnv(object) =
  if std.isObject(object)
  then
    if std.objectHas(object, 'apiVersion')
       && std.objectHas(object, 'kind')
    then
      if object.kind == 'Environment'
      && std.member(object.metadata.name, '%s')
      then object
      else {}
    else
      std.mapWithKey(
        function(key, obj)
          singleEnv(obj),
        object
      )
  else if std.isArray(object)
  then
    std.map(
      function(obj)
        singleEnv(obj),
      object
    )
  else {};

singleEnv(main)
`
