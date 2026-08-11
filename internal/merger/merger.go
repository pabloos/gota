// Package merger combines an inferred Operation with a declared ("gota:"
// comment) Operation. The rule is field-level precedence: whatever the
// comment sets wins outright; whatever it leaves empty is filled in from
// what was inferred.
package merger

import "github.com/pabloos/gota/pkg/model"

// Merge returns a new Operation with declared's fields taking precedence
// over inferred's. Either argument may be nil.
func Merge(inferred, declared *model.Operation) *model.Operation {
	if inferred == nil {
		return declared
	}
	if declared == nil {
		return inferred
	}

	out := *inferred

	if declared.Summary != "" {
		out.Summary = declared.Summary
	}
	if declared.Description != "" {
		out.Description = declared.Description
	}
	if declared.OperationID != "" {
		out.OperationID = declared.OperationID
	}
	if len(declared.Tags) > 0 {
		out.Tags = declared.Tags
	}
	if len(declared.Parameters) > 0 {
		out.Parameters = declared.Parameters
	}
	if declared.RequestBody != nil {
		out.RequestBody = declared.RequestBody
	}
	if len(declared.Responses) > 0 {
		out.Responses = declared.Responses
	}
	if len(declared.Security) > 0 {
		out.Security = declared.Security
	}
	if declared.Deprecated {
		out.Deprecated = declared.Deprecated
	}
	if declared.Skip {
		out.Skip = declared.Skip
	}

	return &out
}
