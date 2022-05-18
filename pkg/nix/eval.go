package nix

func EvalFlake(path string, expr string, options Opts) (string, error) {
	flake := NewFlake(path, options)
	return flake.Eval(expr)
}
