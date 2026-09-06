// Package dependencies reports the Server Dependencies: external programs the
// Music Server calls at runtime. Dependency availability is separate from Server Capabilities.
package dependencies

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	FFMPEG  = "ffmpeg"
	FFPROBE = "ffprobe"

	versionTimeout = 5 * time.Second
)

// Dependency is one probed program. Version is empty when the program is
// missing or its version output could not be parsed.
type Dependency struct {
	Name      string
	Required  bool
	Available bool
	Version   string
}

// Report lists every Server Dependency in a stable order.
type Report []Dependency

// Has reports whether the named program was found.
func (report Report) Has(name string) bool {
	for _, dependency := range report {
		if dependency.Name == name {
			return dependency.Available
		}
	}
	return false
}

type program struct {
	name        string
	required    bool
	versionArgs []string
}

var programs = []program{
	{name: FFMPEG, required: true, versionArgs: []string{"-version"}},
	{name: FFPROBE, required: true, versionArgs: []string{"-version"}},
}

// Probe locates programs and reads their versions through injectable
// functions so the report can be tested without the real binaries.
type Probe struct {
	LookPath func(name string) (string, error)
	Version  func(ctx context.Context, path string, args ...string) (string, error)
}

// SystemProbe uses PATH lookup and runs each program for its version.
func SystemProbe() Probe {
	return Probe{LookPath: exec.LookPath, Version: runVersionCommand}
}

// Run probes every program once. It never fails: a missing program is
// reported as unavailable and a failing version command as available without
// a version.
func (probe Probe) Run(ctx context.Context) Report {
	report := make(Report, 0, len(programs))
	for _, candidate := range programs {
		dependency := Dependency{Name: candidate.name, Required: candidate.required}
		if path, err := probe.LookPath(candidate.name); err == nil {
			dependency.Available = true
			if output, versionErr := probe.Version(ctx, path, candidate.versionArgs...); versionErr == nil {
				dependency.Version = parseVersion(candidate.name, output)
			}
		}
		report = append(report, dependency)
	}
	return report
}

func runVersionCommand(ctx context.Context, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, args...).Output()
	return string(output), err
}

var versionPattern = regexp.MustCompile(`^(\S+) version (\S+)`)

// parseVersion reads "<name> version <version> ..." from the first line, the
// format shared by ffmpeg and ffprobe.
func parseVersion(name, output string) string {
	firstLine, _, _ := strings.Cut(strings.TrimSpace(output), "\n")
	match := versionPattern.FindStringSubmatch(firstLine)
	if match == nil || match[1] != name {
		return ""
	}
	return match[2]
}
