package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
)

// DEFAULT_ACOUSTID_API_KEY is the AcoustID application key that ships with
// the Music Server (ADR 0017). AcoustID issues keys per application, not per
// operator, so one embedded key lets an installation identify recordings
// without any account. Operators may replace it through ACOUSTID_API_KEY.
//
// The value is empty until the project registers its application at
// https://acoustid.org/new-application; while empty, Recording Identification
// reports a missing key and stays inactive.
const DEFAULT_ACOUSTID_API_KEY = ""

const (
	DEFAULT_RECORDING_IDENTIFICATION_MIN_SCORE = 0.90
	DEFAULT_ACOUSTID_BASE_URL                  = "https://api.acoustid.org"
	DEFAULT_MUSICBRAINZ_BASE_URL               = "https://musicbrainz.org"
)

type AcoustIDAPIKeySource string

const (
	ACOUSTID_API_KEY_SOURCE_EMBEDDED AcoustIDAPIKeySource = "embedded"
	ACOUSTID_API_KEY_SOURCE_OPERATOR AcoustIDAPIKeySource = "operator"
	ACOUSTID_API_KEY_SOURCE_MISSING  AcoustIDAPIKeySource = "missing"
)

// RecordingIdentificationConfig controls the AcoustID and MusicBrainz lookup
// that Managed Import runs per Import Batch (ADR 0017).
type RecordingIdentificationConfig struct {
	Enabled              bool
	MinScore             float64
	AcoustIDAPIKey       string
	AcoustIDAPIKeySource AcoustIDAPIKeySource
	AcoustIDBaseURL      string
	MusicBrainzBaseURL   string
}

func loadRecordingIdentificationConfig() (RecordingIdentificationConfig, error) {
	enabled, err := getEnvBool("RECORDING_IDENTIFICATION_ENABLED", true)
	if err != nil {
		return RecordingIdentificationConfig{}, err
	}
	minScore, err := getEnvUnitFloat("RECORDING_IDENTIFICATION_MIN_SCORE", DEFAULT_RECORDING_IDENTIFICATION_MIN_SCORE)
	if err != nil {
		return RecordingIdentificationConfig{}, err
	}
	acoustIDBaseURL, err := getEnvHTTPURL("ACOUSTID_BASE_URL", DEFAULT_ACOUSTID_BASE_URL)
	if err != nil {
		return RecordingIdentificationConfig{}, err
	}
	musicBrainzBaseURL, err := getEnvHTTPURL("MUSICBRAINZ_BASE_URL", DEFAULT_MUSICBRAINZ_BASE_URL)
	if err != nil {
		return RecordingIdentificationConfig{}, err
	}
	apiKey, source := resolveAcoustIDAPIKey(os.Getenv("ACOUSTID_API_KEY"))
	return RecordingIdentificationConfig{
		Enabled:              enabled,
		MinScore:             minScore,
		AcoustIDAPIKey:       apiKey,
		AcoustIDAPIKeySource: source,
		AcoustIDBaseURL:      acoustIDBaseURL,
		MusicBrainzBaseURL:   musicBrainzBaseURL,
	}, nil
}

func resolveAcoustIDAPIKey(operatorKey string) (string, AcoustIDAPIKeySource) {
	if operatorKey != "" {
		return operatorKey, ACOUSTID_API_KEY_SOURCE_OPERATOR
	}
	if DEFAULT_ACOUSTID_API_KEY != "" {
		return DEFAULT_ACOUSTID_API_KEY, ACOUSTID_API_KEY_SOURCE_EMBEDDED
	}
	return "", ACOUSTID_API_KEY_SOURCE_MISSING
}

func getEnvBool(key string, fallback bool) (bool, error) {
	rawValue := os.Getenv(key)
	if rawValue == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(rawValue)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return value, nil
}

func getEnvUnitFloat(key string, fallback float64) (float64, error) {
	rawValue := os.Getenv(key)
	if rawValue == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(rawValue, 64)
	if err != nil || value < 0 || value > 1 {
		return 0, fmt.Errorf("%s must be a number between 0 and 1", key)
	}
	return value, nil
}

func getEnvHTTPURL(key, fallback string) (string, error) {
	rawValue := os.Getenv(key)
	if rawValue == "" {
		return fallback, nil
	}
	parsed, err := url.Parse(rawValue)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("%s must be an absolute http or https URL", key)
	}
	return rawValue, nil
}
