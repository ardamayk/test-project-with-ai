package searchdata

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"modernc.org/sqlite"
)

var versionSections = regexp.MustCompile(`\(([^()]*)\)`)

// Registration precedes opening connections, including migration connections.
var registrationError = errors.Join(
	sqlite.RegisterDeterministicScalarFunction("library_search_text", 2, encodeText),
	sqlite.RegisterDeterministicScalarFunction("library_search_version", 0, func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
		return int64(RULES_VERSION), nil
	}),
)

func CheckRegistration() error {
	if registrationError != nil {
		return fmt.Errorf("register Library Search normalization: %w", registrationError)
	}
	return nil
}

func encodeText(_ *sqlite.FunctionContext, arguments []driver.Value) (driver.Value, error) {
	value, isText := arguments[0].(string)
	kind, isKind := arguments[1].(string)
	if !isText || !isKind {
		return nil, fmt.Errorf("library search requires text and entity kind")
	}
	text := prepareText(value)
	if kind == "track" || kind == "album" {
		text.Versions = titleVersions(value)
	}
	encoded, err := json.Marshal(text)
	if err != nil {
		return nil, fmt.Errorf("encode Library Search %s text: %w", kind, err)
	}
	return string(encoded), nil
}

func titleVersions(value string) []string {
	sections := []string{}
	for _, match := range versionSections.FindAllStringSubmatch(value, -1) {
		sections = append(sections, match[1])
	}
	if _, suffix, found := strings.Cut(value, " - "); found {
		sections = append(sections, suffix)
	}
	versions := []string{}
	for _, section := range sections {
		for _, version := range findVersions(prepareText(section).Folded) {
			if !slices.Contains(versions, version) {
				versions = append(versions, version)
			}
		}
	}
	return versions
}
