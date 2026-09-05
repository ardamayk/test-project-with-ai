package dependencies_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/dependencies"
)

func TestProbeReportsEveryServerDependencyInStableOrder(t *testing.T) {
	probe := dependencies.Probe{
		LookPath: func(name string) (string, error) { return "/usr/bin/" + name, nil },
		Version: func(_ context.Context, path string, _ ...string) (string, error) {
			switch path {
			case "/usr/bin/ffmpeg":
				return "ffmpeg version n9.0.1 Copyright (c) 2000-2026 the FFmpeg developers\nbuilt with gcc", nil
			case "/usr/bin/ffprobe":
				return "ffprobe version 7.1.1 Copyright (c) 2007-2025 the FFmpeg developers", nil
			case "/usr/bin/fpcalc":
				return "fpcalc version 1.5.1 (FFmpeg Lavc60.31.102 Lavf60.16.100 SwR4.12.100)", nil
			}
			return "", errors.New("unexpected path " + path)
		},
	}

	report := probe.Run(context.Background())

	want := []dependencies.Dependency{
		{Name: "ffmpeg", Required: true, Available: true, Version: "n9.0.1"},
		{Name: "ffprobe", Required: true, Available: true, Version: "7.1.1"},
		{Name: "fpcalc", Required: false, Available: true, Version: "1.5.1"},
	}
	if len(report) != len(want) {
		t.Fatalf("report = %+v, want %d entries", report, len(want))
	}
	for index, dependency := range want {
		if report[index] != dependency {
			t.Fatalf("report[%d] = %+v, want %+v", index, report[index], dependency)
		}
	}
}

func TestProbeMarksMissingProgramUnavailableWithoutVersion(t *testing.T) {
	probe := dependencies.Probe{
		LookPath: func(name string) (string, error) {
			if name == "fpcalc" {
				return "", errors.New("executable file not found in $PATH")
			}
			return "/usr/bin/" + name, nil
		},
		Version: func(_ context.Context, path string, _ ...string) (string, error) {
			return "ffmpeg version 7.0", nil
		},
	}

	report := probe.Run(context.Background())

	fpcalc := report[2]
	if fpcalc.Name != "fpcalc" || fpcalc.Available || fpcalc.Version != "" {
		t.Fatalf("fpcalc = %+v, want unavailable without version", fpcalc)
	}
	if !report.Has("ffmpeg") || report.Has("fpcalc") {
		t.Fatalf("Has() = ffmpeg %v fpcalc %v", report.Has("ffmpeg"), report.Has("fpcalc"))
	}
}

func TestProbeKeepsProgramAvailableWhenVersionCommandFails(t *testing.T) {
	probe := dependencies.Probe{
		LookPath: func(name string) (string, error) { return "/opt/" + name, nil },
		Version: func(_ context.Context, _ string, _ ...string) (string, error) {
			return "", errors.New("exit status 1")
		},
	}

	report := probe.Run(context.Background())

	if !report[0].Available || report[0].Version != "" {
		t.Fatalf("ffmpeg = %+v, want available with empty version", report[0])
	}
}

func TestSystemProbeFindsProgramsOnThisHost(t *testing.T) {
	report := dependencies.SystemProbe().Run(context.Background())
	if len(report) != 3 {
		t.Fatalf("report = %+v", report)
	}
	for _, dependency := range report {
		if dependency.Available && dependency.Version == "" {
			t.Fatalf("%s available but version empty", dependency.Name)
		}
	}
}
