package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	monad "github.com/monad-inc/sdk/go"
	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

var _ resource.Resource = &ResourceAlertRule{}
var _ resource.ResourceWithConfigure = &ResourceAlertRule{}
var _ resource.ResourceWithImportState = &ResourceAlertRule{}

type ResourceAlertRule struct {
	client *client.Client
}

type ResourceAlertRuleModel struct {
	ID          types.String  `tfsdk:"id"`
	Name        types.String  `tfsdk:"name"`
	Description types.String  `tfsdk:"description"`
	Type        types.String  `tfsdk:"type"`
	Severity    types.String  `tfsdk:"severity"`
	Active      types.Bool    `tfsdk:"active"`
	PipelineIDs types.Set     `tfsdk:"pipeline_ids"`
	RuleConfig  types.Dynamic `tfsdk:"rule_config"`
}

func NewResourceAlertRule() resource.Resource {
	return &ResourceAlertRule{}
}

func (r *ResourceAlertRule) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_alert_rule"
}

func (r *ResourceAlertRule) Configure(
	ctx context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	if req.ProviderData == nil {
		return
	}

	clientData, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf(
				"Expected *ClientData, got: %T. Please report this issue to the provider developers.",
				req.ProviderData,
			),
		)
		return
	}

	r.client = clientData
}

func (r *ResourceAlertRule) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Monad Alert Rule",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Alert rule identifier",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the alert rule",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the alert rule",
				Optional:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Alert rule type (e.g. `threshold-alert`). Immutable on the " +
					"API — changing it forces resource replacement.",
				Required: true,
				// The API rejects a type change on update, so a changed type
				// must be a destroy-and-recreate rather than an in-place update.
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"severity": schema.StringAttribute{
				MarkdownDescription: "Severity of the alert rule. Required by the API, which " +
					"validates it against a fixed set: `critical`, `high`, `medium`, `low`, `info`.",
				Required: true,
			},
			"active": schema.BoolAttribute{
				MarkdownDescription: "Whether the alert rule is active. Defaults to `true`.",
				Optional:            true,
				// Computed so the server's value populates on import and an
				// omitted attribute adopts the server default instead of
				// showing a spurious change (mirrors monad_pipeline.enabled,
				// ENG-9221). UseStateForUnknown avoids churn on later plans.
				Computed: true,
				Default:  booldefault.StaticBool(true),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"pipeline_ids": schema.SetAttribute{
				MarkdownDescription: "IDs of the pipelines this rule watches. Optional — " +
					"org-level alert types (e.g. `billing-metrics-cost-budget`) have none. " +
					"Modeled as a set because order is not significant.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"rule_config": schema.DynamicAttribute{
				MarkdownDescription: "Type-specific alert rule configuration. Each alert type has " +
					"its own settings schema, validated by the API on write, so this is modeled as " +
					"a free-form dynamic/JSON value (like monad_transform.config). Build it with " +
					"`jsondecode(jsonencode({ ... }))`.",
				Required: true,
				// Keep prior state when the config is semantically equal, so the
				// first plan after `terraform import` is clean instead of
				// re-adding empty fields the API drops (omitempty), matching the
				// monad_transform.config treatment (ENG-9263).
				PlanModifiers: []planmodifier.Dynamic{
					dynamicConfigSemanticEqual{},
				},
			},
		},
	}
}

func (r *ResourceAlertRule) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data ResourceAlertRuleModel

	// Config.Get (not Plan.Get) so an omitted Optional+Computed `active`
	// reads as null here rather than unknown, matching monad_pipeline.
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	active := true
	if !data.Active.IsNull() {
		active = data.Active.ValueBool()
	}

	pipelineIDs, diags := alertRulePipelineIDs(ctx, data.PipelineIDs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	ruleConfig, err := tfDynamicToMapAny(data.RuleConfig)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to parse alert rule config",
			fmt.Sprintf("Error parsing rule_config: %s", err.Error()),
		)
		return
	}

	request := monad.RoutesV3CreateAlertRuleRequest{
		Name:        data.Name.ValueStringPointer(),
		Description: data.Description.ValueStringPointer(),
		Type:        data.Type.ValueStringPointer(),
		Severity:    data.Severity.ValueStringPointer(),
		Active:      &active,
		PipelineIds: pipelineIDs,
		RuleConfig:  ruleConfig,
	}

	rule, monadResp, err := r.client.AlertRulesAPI.
		CreateAlertRule(ctx, r.client.OrganizationID).
		CreateAlertRuleRequest(
			monad.RoutesV3CreateAlertRuleRequestAsCreateAlertRuleRequest(&request),
		).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to create alert rule, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	// Only computed values are taken from the response: the generated `id`, and
	// `active` resolved from its default when the attribute was omitted. Every
	// other attribute is plan-known and preserved verbatim (apply-consistency).
	// rule_config in particular is a Dynamic whose planned cty type must not be
	// rebuilt from the response.
	data.ID = types.StringValue(rule.GetId())
	data.Active = types.BoolValue(active)

	tflog.Trace(ctx, "created an alert rule resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceAlertRule) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data ResourceAlertRuleModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rule, monadResp, err := r.client.AlertRulesAPI.
		GetAlertRuleByID(ctx, r.client.OrganizationID, data.ID.ValueString()).
		Execute()
	if err != nil {
		body := getResponseBody(monadResp)
		if isNotFoundResponse(monadResp, body) {
			// Deleted outside Terraform — drop from state so the next plan
			// recreates it instead of erroring on refresh (ENG-9259).
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to read alert rule, got error: %s. Response: %s",
				err,
				body,
			),
		)
		return
	}

	data.ID = types.StringValue(rule.GetId())
	data.Name = types.StringValue(rule.GetName())
	data.Type = types.StringValue(rule.GetType())
	data.Severity = types.StringValue(rule.GetSeverity())
	data.Active = types.BoolValue(rule.GetActive())

	// description is the one genuinely-optional string scalar: convert an API ""
	// back to null so an omitted attribute doesn't read as drift (the API drops
	// empty strings via omitempty; see monad_output.description).
	data.Description = optionalString(rule.Description)

	// pipeline_ids is a set (order-insensitive); preserve a null when the rule
	// has none so an org-level rule with no pipelines doesn't churn null↔[].
	pipelineIDs, diags := reconcileAlertRulePipelineIDs(ctx, data.PipelineIDs, rule.PipelineIds)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.PipelineIDs = pipelineIDs

	// Reconcile rule_config for drift without cty-type churn: keep the prior
	// state value when the API-derived config is semantically equal, adopting
	// the API value only on genuine drift. On import prior state is null, so the
	// API value populates.
	config, err := reconcileDynamic(data.RuleConfig, rule.RuleConfig)
	if err != nil {
		resp.Diagnostics.AddError("Failed to reconcile alert rule config", err.Error())
		return
	}
	data.RuleConfig = config

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceAlertRule) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var data ResourceAlertRuleModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	active := true
	if !data.Active.IsNull() {
		active = data.Active.ValueBool()
	}

	pipelineIDs, diags := alertRulePipelineIDs(ctx, data.PipelineIDs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	ruleConfig, err := tfDynamicToMapAny(data.RuleConfig)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to parse alert rule config",
			fmt.Sprintf("Error parsing rule_config: %s", err.Error()),
		)
		return
	}

	// The alert-rule update endpoint is a full replace (PUT): omitted fields
	// reset to their defaults. Send the complete desired state — every plan
	// value — so `active`/`severity`/`pipeline_ids` are never silently cleared.
	// `type` is immutable and carries RequiresReplace, so it never reaches here
	// changed; it is not part of the update request model.
	request := monad.RoutesV3UpdateAlertRuleRequest{
		Name:        data.Name.ValueStringPointer(),
		Description: data.Description.ValueStringPointer(),
		Severity:    data.Severity.ValueStringPointer(),
		Active:      &active,
		PipelineIds: pipelineIDs,
		RuleConfig:  ruleConfig,
	}

	_, monadResp, err := r.client.AlertRulesAPI.
		UpdateAlertRule(ctx, r.client.OrganizationID, data.ID.ValueString()).
		UpdateAlertRuleRequest(
			monad.RoutesV3UpdateAlertRuleRequestAsUpdateAlertRuleRequest(&request),
		).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to update alert rule, got error: %s. Response: %s",
				err,
				getResponseBody(monadResp),
			),
		)
		return
	}

	// Preserve plan-known values (see Create): `data` already holds them from
	// the plan; only `active` is resolved to a known value here.
	data.Active = types.BoolValue(active)

	tflog.Trace(ctx, "updated an alert rule resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceAlertRule) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data ResourceAlertRuleModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	monadResp, err := r.client.AlertRulesAPI.
		DeleteAlertRule(ctx, r.client.OrganizationID, data.ID.ValueString()).
		Execute()
	if err != nil {
		body := getResponseBody(monadResp)
		if isNotFoundResponse(monadResp, body) {
			// Already gone remotely — nothing to delete.
			return
		}
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf(
				"Unable to delete alert rule, got error: %s. Response: %s",
				err,
				body,
			),
		)
		return
	}
}

func (r *ResourceAlertRule) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// optionalString converts an API string pointer into a types.String, mapping
// both nil and "" to null so an omitted Optional scalar doesn't read as drift
// against the API's omitempty serialization.
func optionalString(in *string) types.String {
	if in == nil || *in == "" {
		return types.StringNull()
	}
	return types.StringValue(*in)
}

// alertRulePipelineIDs converts the pipeline_ids set into a plain []string for
// an API request. A null/unknown set yields a nil slice, which the SDK omits
// (omitempty) — correct for org-level alert types that watch no pipeline.
func alertRulePipelineIDs(ctx context.Context, set types.Set) ([]string, diag.Diagnostics) {
	if set.IsNull() || set.IsUnknown() {
		return nil, nil
	}
	var ids []string
	diags := set.ElementsAs(ctx, &ids, false)
	return ids, diags
}

// reconcileAlertRulePipelineIDs refreshes pipeline_ids from the API for drift
// detection while avoiding a null↔empty-set churn. When the rule watches no
// pipelines and prior state was null (an org-level rule, or an omitted
// attribute), the null is preserved; otherwise the API set populates. Set
// element order is insignificant, so no order preservation is needed.
func reconcileAlertRulePipelineIDs(
	ctx context.Context,
	prior types.Set,
	apiIDs []string,
) (types.Set, diag.Diagnostics) {
	if len(apiIDs) == 0 && prior.IsNull() {
		return prior, nil
	}
	return types.SetValueFrom(ctx, types.StringType, apiIDs)
}
