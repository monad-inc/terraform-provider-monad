package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	monad "github.com/monad-inc/sdk/go"
)

// computedMetaAttributes are the read-only bookkeeping fields the API returns
// on every object (ENG-11406). created_at and managed_by never change, so they
// keep their state value across plans; updated_at changes on every write, so
// it is left without UseStateForUnknown and plans as "known after apply"
// whenever the resource has other changes (keeping a stale value would make
// the post-apply value inconsistent with the plan). A change to a write-only
// value alone is not such a change to the framework; planWriteOnlyChange
// covers it.
func computedMetaAttributes(withManagedBy bool) map[string]schema.Attribute {
	attrs := map[string]schema.Attribute{
		"created_at": schema.StringAttribute{
			MarkdownDescription: "When the object was created (RFC 3339, set by the API).",
			Computed:            true,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
		"updated_at": schema.StringAttribute{
			MarkdownDescription: "When the object was last changed (RFC 3339, set by the API).",
			Computed:            true,
		},
	}
	if withManagedBy {
		attrs["managed_by"] = schema.StringAttribute{
			MarkdownDescription: "Who manages the object: `customer` for objects you create, `system` or " +
				"`internal` for ones Monad maintains.",
			Computed:      true,
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
	}
	return attrs
}

// planWriteOnlyChange plans an update for a change to a write-only value:
// it marks the value's hash unknown, and updated_at with it. The framework
// marks updated_at unknown only when the plan differs from state, and a
// write-only value is null in both, so a change to it alone would otherwise
// plan the old updated_at and fail as an inconsistent result once the API
// bumps it.
func planWriteOnlyChange(ctx context.Context, plan *tfsdk.Plan, hashPath path.Path) diag.Diagnostics {
	diags := plan.SetAttribute(ctx, hashPath, types.StringUnknown())
	diags.Append(plan.SetAttribute(ctx, path.Root("updated_at"), types.StringUnknown())...)
	return diags
}

// withComputedMeta adds computedMetaAttributes to a schema's attributes.
func withComputedMeta(attrs map[string]schema.Attribute, withManagedBy bool) map[string]schema.Attribute {
	for k, v := range computedMetaAttributes(withManagedBy) {
		attrs[k] = v
	}
	return attrs
}

// metaString maps an API timestamp or enum to state, treating "" (absent, or
// no response object after an adopted create) as null rather than an empty
// string. A computed value must never be left unknown after apply.
func metaString(v string) types.String {
	if v == "" {
		return types.StringNull()
	}
	return types.StringValue(v)
}

func managedByString(m *monad.ModelsManagedBy) types.String {
	if m == nil {
		return types.StringNull()
	}
	return metaString(string(*m))
}
