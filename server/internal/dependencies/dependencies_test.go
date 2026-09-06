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
			}
			return "", errors.New("unexpected path " + path)
		},
	}

	report := probe.Run(context.Background())

	want := []dependencies.Dependency{
		{Name: "ffmpeg", Required: true, Available: true, Version: "n9.0.1"},
		{Name: "ffprobe", Required: true, Available: true, Version: "7.1.1"},
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
			if name == "ffprobe" {
				return "", errors.New("executable file not found in $PATH")
			}
			return "/usr/bin/" + name, nil
		},
		Version: func(_ context.Context, path string, _ ...string) (string, error) {
			return "ffmpeg version 7.0", nil
		},
	}

	report := probe.Run(context.Background())

	ffprobe := report[1]
	if ffprobe.Name != "ffprobe" || ffprobe.Available || ffprobe.Version != "" {
		t.Fatalf("ffprobe = %+v, want unavailable without version", ffprobe)
	}
	if !report.Has("ffmpeg") || report.Has("ffprobe") {
		t.Fatalf("Has() = ffmpeg %v ffprobe %v", report.Has("ffmpeg"), report.Has("ffprobe"))
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
	if len(report) != 2 || report[0].Name != "ffmpeg" || report[1].Name != "ffprobe" {
		t.Fatalf("report = %+v", report)
	}
}
