package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// immutableNoReplace rejects a change to an attribute the API cannot update,
// instead of planning a replacement.
//
// RequiresReplace is the usual answer for a create-only field, but it is wrong
// when destroying the resource has side effects the practitioner never asked
// for. Deleting an SSO connection cascades (ON DELETE CASCADE) to its
// organization link, user roles, auth providers and tokens, so a replace would
// lock every user out of the organization. Failing the plan forces the change
// to be made deliberately, outside Terraform.
type immutableNoReplace struct {
	// reason is appended to the error and explains why the change is refused.
	reason string
}

var (
	_ planmodifier.Set    = immutableNoReplace{}
	_ planmodifier.String = immutableNoReplace{}
)

func (immutableNoReplace) Description(context.Context) string {
	return "Fails the plan when the value changes on an existing resource, instead of replacing it."
}

func (m immutableNoReplace) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m immutableNoReplace) PlanModifySet(ctx context.Context, req planmodifier.SetRequest, resp *planmodifier.SetResponse) {
	m.check(req.Path, req.State.Raw.IsNull(), req.Plan.Raw.IsNull(), req.StateValue, req.PlanValue, &resp.Diagnostics)
}

func (m immutableNoReplace) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	m.check(req.Path, req.State.Raw.IsNull(), req.Plan.Raw.IsNull(), req.StateValue, req.PlanValue, &resp.Diagnostics)
}

func (m immutableNoReplace) check(p path.Path, stateNull, planNull bool, stateValue, planValue attr.Value, diags *diag.Diagnostics) {
	// Create (no prior state) and destroy (no plan) are not changes.
	if stateNull || planNull {
		return
	}
	if planValue.IsUnknown() {
		// An unknown value might differ from state at apply time, and Update
		// has no way to send it. Refuse rather than let state and the API drift.
		diags.AddAttributeError(p, "Value must be known at plan time",
			fmt.Sprintf("%s cannot be changed on an existing resource, so its value must be known during plan. %s", p, m.reason))
		return
	}
	if planValue.Equal(stateValue) {
		return
	}
	diags.AddAttributeError(p, "Attribute cannot be changed",
		fmt.Sprintf("%s cannot be changed on an existing resource, and this provider will not replace it. %s", p, m.reason))
}
