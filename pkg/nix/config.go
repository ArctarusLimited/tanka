package nix

type Opts struct {
	Binary string

	// Override paths for flakes
	FlakeOverrides map[string]string
}
