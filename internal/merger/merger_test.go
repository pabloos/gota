package merger_test

import (
	"testing"

	"github.com/pabloos/gota/internal/merger"
	"github.com/pabloos/gota/pkg/model"
)

func TestMerge_NilHandling(t *testing.T) {
	declared := &model.Operation{Summary: "declared"}
	inferred := &model.Operation{Summary: "inferred"}

	t.Run("both nil", func(t *testing.T) {
		if got := merger.Merge(nil, nil); got != nil {
			t.Errorf("Merge(nil, nil) = %+v, want nil", got)
		}
	})

	t.Run("inferred nil returns declared as-is", func(t *testing.T) {
		if got := merger.Merge(nil, declared); got != declared {
			t.Errorf("Merge(nil, declared) = %p, want the declared pointer %p", got, declared)
		}
	})

	t.Run("declared nil returns inferred as-is", func(t *testing.T) {
		if got := merger.Merge(inferred, nil); got != inferred {
			t.Errorf("Merge(inferred, nil) = %p, want the inferred pointer %p", got, inferred)
		}
	})
}

func TestMerge_DeclaredWinsWhenSet(t *testing.T) {
	inferred := &model.Operation{
		Summary:     "inferred summary",
		Description: "inferred description",
		OperationID: "InferredID",
		Tags:        []string{"inferred-tag"},
		Parameters:  []model.Parameter{{Name: "inferred-param", In: "query"}},
		RequestBody: &model.RequestBody{Description: "inferred body"},
		Responses:   map[string]model.Response{"200": {Description: "inferred OK"}},
	}
	declared := &model.Operation{
		Summary:     "declared summary",
		Description: "declared description",
		OperationID: "DeclaredID",
		Tags:        []string{"declared-tag"},
		Parameters:  []model.Parameter{{Name: "declared-param", In: "path"}},
		RequestBody: &model.RequestBody{Description: "declared body"},
		Responses:   map[string]model.Response{"201": {Description: "declared Created"}},
		Deprecated:  true,
	}

	out := merger.Merge(inferred, declared)

	if out.Summary != "declared summary" {
		t.Errorf("Summary = %q, want declared value", out.Summary)
	}
	if out.Description != "declared description" {
		t.Errorf("Description = %q, want declared value", out.Description)
	}
	if out.OperationID != "DeclaredID" {
		t.Errorf("OperationID = %q, want declared value", out.OperationID)
	}
	if len(out.Tags) != 1 || out.Tags[0] != "declared-tag" {
		t.Errorf("Tags = %+v, want declared value", out.Tags)
	}
	if len(out.Parameters) != 1 || out.Parameters[0].Name != "declared-param" {
		t.Errorf("Parameters = %+v, want declared value", out.Parameters)
	}
	if out.RequestBody == nil || out.RequestBody.Description != "declared body" {
		t.Errorf("RequestBody = %+v, want declared value", out.RequestBody)
	}
	// Responses merge per status code: the declared 201 is added, and the
	// inferred 200 the comment doesn't mention is kept (not wiped).
	if resp, ok := out.Responses["200"]; !ok || resp.Description != "inferred OK" {
		t.Errorf("Responses = %+v, want the unmentioned inferred 200 preserved", out.Responses)
	}
	if resp, ok := out.Responses["201"]; !ok || resp.Description != "declared Created" {
		t.Errorf("Responses = %+v, want the declared 201 added", out.Responses)
	}
	if !out.Deprecated {
		t.Errorf("Deprecated = false, want true from declared")
	}
}

func TestMerge_DeclaredSecurity(t *testing.T) {
	// Security is not inferable from code, so a declared requirement is
	// simply taken over an operation that has none inferred.
	inferred := &model.Operation{Summary: "inferred"}
	declared := &model.Operation{
		Security: &[]model.SecurityRequirement{{"BearerAuth": {}}},
	}

	out := merger.Merge(inferred, declared)

	if out.Security == nil || len(*out.Security) != 1 {
		t.Fatalf("Security = %+v, want the declared requirement", out.Security)
	}
	if _, ok := (*out.Security)[0]["BearerAuth"]; !ok {
		t.Errorf("Security[0] = %+v, want a BearerAuth key", (*out.Security)[0])
	}
	if out.Summary != "inferred" {
		t.Errorf("Summary = %q, want the inferred value preserved", out.Summary)
	}
}

func TestMerge_DeclaredEmptySecurityRoundTrips(t *testing.T) {
	// An explicitly declared empty requirement list (security: []) marks
	// the operation public and must survive as a non-nil empty slice —
	// distinct from an absent one, which would inherit the document default.
	inferred := &model.Operation{Summary: "inferred"}
	declared := &model.Operation{Security: &[]model.SecurityRequirement{}}

	out := merger.Merge(inferred, declared)

	if out.Security == nil {
		t.Fatalf("Security is nil, want a declared empty (non-nil) list")
	}
	if len(*out.Security) != 0 {
		t.Errorf("Security = %+v, want empty", *out.Security)
	}
}

func TestMerge_DeclaredExampleKeepsInferredSchema(t *testing.T) {
	// Declaring an example under a response must not erase the schema and
	// description gota inferred for that same status code — the fields merge.
	inferred := &model.Operation{
		Responses: map[string]model.Response{
			"200": {
				Description: "OK",
				Content: map[string]model.MediaType{
					"application/json": {Schema: &model.Schema{Ref: "#/components/schemas/Sig"}},
				},
			},
		},
	}
	declared := &model.Operation{
		Responses: map[string]model.Response{
			"200": {
				Content: map[string]model.MediaType{
					"application/json": {Example: map[string]any{"id": "sig_2f8a"}},
				},
			},
		},
	}

	out := merger.Merge(inferred, declared)

	resp := out.Responses["200"]
	if resp.Description != "OK" {
		t.Errorf("Description = %q, want the inferred %q preserved", resp.Description, "OK")
	}
	mt := resp.Content["application/json"]
	if mt.Schema == nil || mt.Schema.Ref != "#/components/schemas/Sig" {
		t.Errorf("Schema = %+v, want the inferred $ref preserved", mt.Schema)
	}
	if mt.Example == nil {
		t.Errorf("Example is nil, want the declared example added")
	}
}

func TestMerge_InferredFillsGapsWhenDeclaredEmpty(t *testing.T) {
	inferred := &model.Operation{
		Summary:     "inferred summary",
		Description: "inferred description",
		OperationID: "InferredID",
		Tags:        []string{"inferred-tag"},
		Parameters:  []model.Parameter{{Name: "id", In: "path"}},
		RequestBody: &model.RequestBody{Description: "inferred body"},
		Responses:   map[string]model.Response{"200": {Description: "inferred OK"}},
	}
	// declared only sets OperationID; everything else is left at zero
	// value, simulating a "gota:" comment that only overrides one field.
	declared := &model.Operation{OperationID: "DeclaredID"}

	out := merger.Merge(inferred, declared)

	if out.Summary != "inferred summary" {
		t.Errorf("Summary = %q, want the inferred value to survive", out.Summary)
	}
	if out.Description != "inferred description" {
		t.Errorf("Description = %q, want the inferred value to survive", out.Description)
	}
	if out.OperationID != "DeclaredID" {
		t.Errorf("OperationID = %q, want the declared value", out.OperationID)
	}
	if len(out.Tags) != 1 || out.Tags[0] != "inferred-tag" {
		t.Errorf("Tags = %+v, want the inferred value to survive", out.Tags)
	}
	if len(out.Parameters) != 1 || out.Parameters[0].Name != "id" {
		t.Errorf("Parameters = %+v, want the inferred value to survive", out.Parameters)
	}
	if out.RequestBody == nil || out.RequestBody.Description != "inferred body" {
		t.Errorf("RequestBody = %+v, want the inferred value to survive", out.RequestBody)
	}
	if resp, ok := out.Responses["200"]; !ok || resp.Description != "inferred OK" {
		t.Errorf("Responses = %+v, want the inferred value to survive", out.Responses)
	}
	if out.Deprecated {
		t.Errorf("Deprecated = true, want false (neither side set it)")
	}
}

func TestMerge_DoesNotMutateInputs(t *testing.T) {
	inferred := &model.Operation{Summary: "inferred summary", OperationID: "InferredID"}
	declared := &model.Operation{Summary: "declared summary"}

	out := merger.Merge(inferred, declared)
	out.OperationID = "mutated after merge"

	if inferred.OperationID != "InferredID" {
		t.Errorf("mutating the merged result changed inferred.OperationID to %q", inferred.OperationID)
	}
	if declared.Summary != "declared summary" {
		t.Errorf("declared was mutated: %q", declared.Summary)
	}
}

// TestMerge_DeprecatedCannotBeUnset documents a real quirk: Deprecated is a
// bare bool, so there is no way to represent "the comment explicitly set
// deprecated: false" as distinct from "the comment didn't mention
// deprecated at all". Once something upstream infers Deprecated = true, a
// comment cannot un-deprecate an operation. This never triggers in
// practice today because internal/inference never sets Deprecated, but is
// pinned here so a future change is deliberate.
func TestMerge_DeprecatedCannotBeUnset(t *testing.T) {
	inferred := &model.Operation{Deprecated: true}
	declared := &model.Operation{Deprecated: false}

	out := merger.Merge(inferred, declared)

	if !out.Deprecated {
		t.Fatalf("Deprecated = false; this test's premise (declared:false can't override inferred:true) no longer holds — update the doc comment on Merge if this was fixed intentionally")
	}
}
