package testutil

import "debug/buildinfo"

// GoBuildArgs is the argument list for `go build` of a CLI a test will run.
// Race instrumentation is added only when this test binary was built with
// -race, so a plain `go test` keeps the current build cost.
func GoBuildArgs(output string, pkgs ...string) []string {
	args := make([]string, 0, 4+len(pkgs))
	args = append(args, "build")
	if RaceEnabled() {
		args = append(args, "-race")
	}
	args = append(args, "-o", output)
	args = append(args, pkgs...)
	return args
}

// BinaryHasRace reports whether path was linked with the race detector.
// The answer comes from the binary's own build settings, the same record
// `go version -m` prints, not from the command line that produced it.
func BinaryHasRace(path string) (bool, error) {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return false, err
	}
	for _, setting := range info.Settings {
		if setting.Key == "-race" {
			return setting.Value == "true", nil
		}
	}
	return false, nil
}
