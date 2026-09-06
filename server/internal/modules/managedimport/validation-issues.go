package managedimport

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
)

type ValidationIssues []library.InspectionIssue

func (issues ValidationIssues) Value() (driver.Value, error) {
	if issues == nil {
		return "[]", nil
	}
	data, err := json.Marshal(issues)
	if err != nil {
		return nil, fmt.Errorf("encode import validation issues: %w", err)
	}
	return string(data), nil
}

func (issues *ValidationIssues) Scan(value any) error {
	var data []byte
	switch stored := value.(type) {
	case string:
		data = []byte(stored)
	case []byte:
		data = stored
	case nil:
		*issues = nil
		return nil
	default:
		return fmt.Errorf("read import validation issues: unexpected database type %T", value)
	}
	if err := json.Unmarshal(data, issues); err != nil {
		return fmt.Errorf("decode import validation issues: %w", err)
	}
	return nil
}

func (validationErr *ValidationError) validationIssues() ValidationIssues {
	if len(validationErr.Issues) > 0 {
		return validationErr.Issues
	}
	return ValidationIssues{{Code: library.InspectionErrorCode(validationErr.Code), Field: validationErr.Field, Reason: strictValidationReason(validationErr)}}
}
