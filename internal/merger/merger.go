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
	out.RequestBody = mergeRequestBody(inferred.RequestBody, declared.RequestBody)
	out.Responses = mergeResponses(inferred.Responses, declared.Responses)
	if declared.Security != nil {
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

// mergeResponses folds the declared responses into the inferred ones,
// keyed by status code. A declared code that also exists in the inferred
// set is merged field-by-field (see mergeResponse) so declaring, say, an
// example under "200" doesn't discard the schema and description gota
// inferred for it; a declared code with no inferred counterpart is added;
// an inferred code the comment doesn't mention is kept.
func mergeResponses(inferred, declared map[string]model.Response) map[string]model.Response {
	if len(declared) == 0 {
		return inferred
	}
	out := make(map[string]model.Response, len(inferred)+len(declared))
	for code, resp := range inferred {
		out[code] = resp
	}
	for code, dresp := range declared {
		if iresp, ok := out[code]; ok {
			out[code] = mergeResponse(iresp, dresp)
		} else {
			out[code] = dresp
		}
	}
	return out
}

// mergeResponse merges one declared response into its inferred counterpart:
// the declared description wins when non-empty, and content is merged per
// media type (see mergeContent).
func mergeResponse(inferred, declared model.Response) model.Response {
	out := inferred
	if declared.Description != "" {
		out.Description = declared.Description
	}
	out.Content = mergeContent(inferred.Content, declared.Content)
	return out
}

// mergeRequestBody applies the same field-level merge to the request body,
// so a declared example or description doesn't erase the inferred schema.
// Either side may be nil.
func mergeRequestBody(inferred, declared *model.RequestBody) *model.RequestBody {
	if declared == nil {
		return inferred
	}
	if inferred == nil {
		return declared
	}
	out := *inferred
	if declared.Description != "" {
		out.Description = declared.Description
	}
	if declared.Required {
		out.Required = declared.Required
	}
	out.Content = mergeContent(inferred.Content, declared.Content)
	return &out
}

// mergeContent merges declared media types into inferred ones, keyed by
// media-type string (e.g. "application/json"). A shared key is merged
// field-by-field (see mergeMediaType); keys present on only one side are
// carried through.
func mergeContent(inferred, declared map[string]model.MediaType) map[string]model.MediaType {
	if len(declared) == 0 {
		return inferred
	}
	out := make(map[string]model.MediaType, len(inferred)+len(declared))
	for mt, media := range inferred {
		out[mt] = media
	}
	for mt, dmedia := range declared {
		if imedia, ok := out[mt]; ok {
			out[mt] = mergeMediaType(imedia, dmedia)
		} else {
			out[mt] = dmedia
		}
	}
	return out
}

// mergeMediaType merges the fields of one media type: the declared schema,
// example and examples each win when set, while an unset one leaves the
// inferred value in place — so a declared example coexists with an
// inferred schema.
func mergeMediaType(inferred, declared model.MediaType) model.MediaType {
	out := inferred
	if declared.Schema != nil {
		out.Schema = declared.Schema
	}
	if declared.Example != nil {
		out.Example = declared.Example
	}
	if declared.Examples != nil {
		out.Examples = declared.Examples
	}
	return out
}
