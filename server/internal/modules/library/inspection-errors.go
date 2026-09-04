package library

import "errors"

// InspectionIssue describes one actionable failure without internal error details.
type InspectionIssue struct {
	Code   InspectionErrorCode `json:"code"`
	Field  string              `json:"field"`
	Reason string              `json:"reason"`
}

// InspectionIssues flattens independent validation failures in check order.
func InspectionIssues(err error) []InspectionIssue {
	if err == nil {
		return nil
	}
	if inspectionErr, ok := err.(*InspectionError); ok {
		return []InspectionIssue{{Code: inspectionErr.Code, Field: inspectionErr.Field, Reason: inspectionErr.Reason}}
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var issues []InspectionIssue
		for _, child := range joined.Unwrap() {
			issues = append(issues, InspectionIssues(child)...)
		}
		return issues
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return InspectionIssues(wrapped.Unwrap())
	}
	return nil
}

// Do not report a missing value when its source field could not be parsed.
func excludeInspectionFields(err error, fields []string) error {
	if inspectionErr, ok := err.(*InspectionError); ok {
		for _, field := range fields {
			if inspectionErr.Field == field {
				return nil
			}
		}
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var remaining []error
		for _, child := range joined.Unwrap() {
			remaining = append(remaining, excludeInspectionFields(child, fields))
		}
		return errors.Join(remaining...)
	}
	return err
}
