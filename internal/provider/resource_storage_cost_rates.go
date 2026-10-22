package provider

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	monad "github.com/monad-inc/sdk/go"
	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

var _ resource.Resource = &ResourceStorageCostRates{}
var _ resource.ResourceWithConfigure = &ResourceStorageCostRates{}
var _ resource.ResourceWithImportState = &ResourceStorageCostRates{}
var _ resource.ResourceWithValidateConfig = &ResourceStorageCostRates{}

// ResourceStorageCostRates manages the organization's storage cost rates — the
// $/GB prices the Storage Costs view applies (PRO-717). It is a singleton (one
// set of rates per organization, id = the organization ID) and it is
// non-authoritative: it manages only the storage types and outputs it declares,
// and leaves any other rate in the organization alone.
//
// The API cannot delete a per-type rate, only overwrite it, and a type with no
// rate is priced at $0. So a per-type rate removed from the configuration, or
// on destroy, is set to 0 — the same cost as never having been set. Per-output
// rates can be removed, and are.
type ResourceStorageCostRates struct {
	client *client.Client
}

type ResourceStorageCostRatesModel struct {
	Timeouts    timeouts.Value `tfsdk:"timeouts"`
	ID          types.String   `tfsdk:"id"`
	Costs       types.Map      `tfsdk:"costs"`
	OutputCosts types.Map      `tfsdk:"output_costs"`
}

type storageOutputCost struct {
	StorageType types.String  `tfsdk:"storage_type"`
	CostPerGB   types.Float64 `tfsdk:"cost_per_gb"`
}

// maxCostPerGB is the API's upper bound (SetStorageTypeCost rejects more).
const maxCostPerGB = 1000.0

func NewResourceStorageCostRates() resource.Resource {
	return &ResourceStorageCostRates{}
}

func (r *ResourceStorageCostRates) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_storage_cost_rates"
}

func (r *ResourceStorageCostRates) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	clientData, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *ClientData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	r.client = clientData
}

// storageOutputCostType is the element type of output_costs.
var storageOutputCostType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"storage_type": types.StringType,
	"cost_per_gb":  types.Float64Type,
}}

func (r *ResourceStorageCostRates) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Monad Storage Cost Rates",
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true}),
		},
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The organization ID (there is one set of rates per organization).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"costs": schema.MapAttribute{
				MarkdownDescription: "Default price in $/GB per storage type, keyed by output type ID (`dev-null`, `kv-lookup`, …). Applies to every output of that type without its own entry in `output_costs`.",
				Optional:            true,
				ElementType:         types.Float64Type,
			},
			"output_costs": schema.MapNestedAttribute{
				MarkdownDescription: "Prices for specific outputs, keyed by output ID; they override the storage type's default.",
				Optional:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"storage_type": schema.StringAttribute{
							MarkdownDescription: "The output's type ID, such as `dev-null`.",
							Required:            true,
						},
						"cost_per_gb": schema.Float64Attribute{
							MarkdownDescription: "Price in $/GB.",
							Required:            true,
						},
					},
				},
			},
		},
	}
}

// ValidateConfig enforces the API's 0–1000 $/GB range at plan time.
func (r *ResourceStorageCostRates) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data ResourceStorageCostRatesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	check := func(p path.Path, v types.Float64) {
		if v.IsNull() || v.IsUnknown() {
			return
		}
		f := v.ValueFloat64()
		if f < 0 || f > maxCostPerGB || math.IsNaN(f) || math.IsInf(f, 0) {
			resp.Diagnostics.AddAttributeError(p, "Cost out of range",
				fmt.Sprintf("cost per GB must be between 0 and %.0f; got %v.", maxCostPerGB, f))
		}
	}
	if !data.Costs.IsNull() && !data.Costs.IsUnknown() {
		for k, v := range data.Costs.Elements() {
			if f, ok := v.(types.Float64); ok {
				check(path.Root("costs").AtMapKey(k), f)
			}
		}
	}
	if !data.OutputCosts.IsNull() && !data.OutputCosts.IsUnknown() {
		var outs map[string]storageOutputCost
		resp.Diagnostics.Append(data.OutputCosts.ElementsAs(ctx, &outs, false)...)
		for k, o := range outs {
			check(path.Root("output_costs").AtMapKey(k).AtName("cost_per_gb"), o.CostPerGB)
		}
	}
}

func (r *ResourceStorageCostRates) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ResourceStorageCostRatesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := withOperationTimeout(ctx, data.Timeouts.Create, r.client.RequestTimeout, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}

	costs, outputs := r.decode(ctx, data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.put(ctx, costs, outputs, nil); err != nil {
		resp.Diagnostics.AddError("Unable to set storage cost rates", err.Error())
		return
	}

	// Nothing to adopt after a timeout: the "object" is the organization's rate
	// table, and a PUT is idempotent, so a retried apply converges.
	data.ID = types.StringValue(r.client.OrganizationID)
	tflog.Trace(ctx, "set storage cost rates")
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceStorageCostRates) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ResourceStorageCostRatesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := withOperationTimeout(ctx, data.Timeouts.Read, r.client.RequestTimeout, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}

	cfg, httpResp, err := r.client.OrganizationsStorageCostAPI.GetStorageTypeCost(ctx, r.client.OrganizationID).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Client Error",
			fmt.Sprintf("Unable to read storage cost rates: %s. Response: %s", err, getResponseBody(httpResp)))
		return
	}
	apiTypes, apiOutputs := splitCostEntries(cfg.Costs)

	// Import (no prior state): manage every rate the organization has.
	// Otherwise refresh only the keys this resource manages, so rates set
	// elsewhere never read as drift — and a managed key the API no longer has
	// drops out, so the plan shows it being set again.
	importing := data.Costs.IsNull() && data.OutputCosts.IsNull()

	priorCosts := map[string]float64{}
	if !data.Costs.IsNull() {
		resp.Diagnostics.Append(data.Costs.ElementsAs(ctx, &priorCosts, false)...)
	}
	newCosts := map[string]float64{}
	for t, v := range apiTypes {
		if importing {
			newCosts[t] = v
			continue
		}
		if p, ok := priorCosts[t]; ok {
			// Keep the configured spelling when the API's float32 rounding is
			// the only difference.
			if sameCost(p, v) {
				newCosts[t] = p
			} else {
				newCosts[t] = v
			}
		}
	}

	var priorOutputs map[string]storageOutputCost
	if !data.OutputCosts.IsNull() {
		resp.Diagnostics.Append(data.OutputCosts.ElementsAs(ctx, &priorOutputs, false)...)
	}
	newOutputs := map[string]storageOutputCost{}
	for id, o := range apiOutputs {
		p, managed := priorOutputs[id]
		if !importing && !managed {
			continue
		}
		cost := o.cost
		if managed && sameCost(p.CostPerGB.ValueFloat64(), cost) {
			cost = p.CostPerGB.ValueFloat64()
		}
		newOutputs[id] = storageOutputCost{StorageType: types.StringValue(o.storageType), CostPerGB: types.Float64Value(cost)}
	}

	if resp.Diagnostics.HasError() {
		return
	}
	data.ID = types.StringValue(r.client.OrganizationID)
	data.Costs = keepNullIfEmpty(ctx, data.Costs, newCosts, importing, &resp.Diagnostics)
	data.OutputCosts = keepNullOutputsIfEmpty(ctx, data.OutputCosts, newOutputs, importing, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceStorageCostRates) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ResourceStorageCostRatesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := withOperationTimeout(ctx, plan.Timeouts.Update, r.client.RequestTimeout, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}

	costs, outputs := r.decode(ctx, plan, &resp.Diagnostics)
	oldCosts, oldOutputs := r.decode(ctx, state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// A per-type rate dropped from the configuration cannot be deleted; set it
	// to 0, the cost of an unset type.
	for t := range oldCosts {
		if _, kept := costs[t]; !kept {
			costs[t] = 0
		}
	}
	// A per-output rate dropped from the configuration, or whose storage type
	// changed, is cleared first. The API applies clears after upserts within
	// one request, so the clear must be its own request.
	var clear []string
	for id, o := range oldOutputs {
		n, kept := outputs[id]
		if !kept || n.storageType != o.storageType {
			clear = append(clear, id)
		}
	}
	sort.Strings(clear)
	if len(clear) > 0 {
		if err := r.put(ctx, nil, nil, clear); err != nil {
			resp.Diagnostics.AddError("Unable to clear output cost rates", err.Error())
			return
		}
	}
	if err := r.put(ctx, costs, outputs, nil); err != nil {
		resp.Diagnostics.AddError("Unable to set storage cost rates", err.Error())
		return
	}

	plan.ID = types.StringValue(r.client.OrganizationID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ResourceStorageCostRates) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ResourceStorageCostRatesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := withOperationTimeout(ctx, data.Timeouts.Delete, r.client.RequestTimeout, &resp.Diagnostics)
	defer cancel()
	if resp.Diagnostics.HasError() {
		return
	}

	costs, outputs := r.decode(ctx, data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	zero := map[string]float64{}
	for t := range costs {
		zero[t] = 0
	}
	clear := make([]string, 0, len(outputs))
	for id := range outputs {
		clear = append(clear, id)
	}
	sort.Strings(clear)
	if err := r.put(ctx, zero, nil, clear); err != nil {
		resp.Diagnostics.AddError("Unable to reset storage cost rates", err.Error())
	}
}

// ImportState takes the organization ID, which must be the provider's.
func (r *ResourceStorageCostRates) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if r.client != nil && req.ID != r.client.OrganizationID {
		resp.Diagnostics.AddError("Wrong organization",
			fmt.Sprintf("Import ID %q is not the provider's organization %q. Storage cost rates belong to the organization the provider is configured for.",
				req.ID, r.client.OrganizationID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

type outputCost struct {
	storageType string
	cost        float64
}

func (r *ResourceStorageCostRates) decode(ctx context.Context, m ResourceStorageCostRatesModel, diags *diag.Diagnostics) (map[string]float64, map[string]outputCost) {
	costs := map[string]float64{}
	if !m.Costs.IsNull() && !m.Costs.IsUnknown() {
		diags.Append(m.Costs.ElementsAs(ctx, &costs, false)...)
	}
	outputs := map[string]outputCost{}
	if !m.OutputCosts.IsNull() && !m.OutputCosts.IsUnknown() {
		var outs map[string]storageOutputCost
		diags.Append(m.OutputCosts.ElementsAs(ctx, &outs, false)...)
		for id, o := range outs {
			outputs[id] = outputCost{storageType: o.StorageType.ValueString(), cost: o.CostPerGB.ValueFloat64()}
		}
	}
	return costs, outputs
}

// put sends one SetStorageTypeCost request.
func (r *ResourceStorageCostRates) put(ctx context.Context, costs map[string]float64, outputs map[string]outputCost, clear []string) error {
	req := monad.RoutesV2SetStorageTypeCostRequest{ClearOutputCosts: clear}
	if len(costs) > 0 {
		req.Costs = make(map[string]monad.RoutesV2SetStorageTypeCostEntry, len(costs))
		for t, c := range costs {
			v := float32(c)
			req.Costs[t] = monad.RoutesV2SetStorageTypeCostEntry{CostPerGb: &v}
		}
	}
	ids := make([]string, 0, len(outputs))
	for id := range outputs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		o := outputs[id]
		v := float32(o.cost)
		id, st := id, o.storageType
		req.OutputCosts = append(req.OutputCosts, monad.RoutesV2SetOutputCostEntry{OutputId: &id, StorageType: &st, CostPerGb: &v})
	}
	_, httpResp, err := r.client.OrganizationsStorageCostAPI.
		SetStorageTypeCost(ctx, r.client.OrganizationID).
		SetStorageTypeCostRequest(monad.RoutesV2SetStorageTypeCostRequestAsSetStorageTypeCostRequest(&req)).
		Execute()
	if err != nil {
		return fmt.Errorf("%s. Response: %s", err, getResponseBody(httpResp))
	}
	return nil
}

// splitCostEntries separates the API's flat list into per-type defaults (no
// output_id) and per-output rates.
func splitCostEntries(entries []monad.ModelsStorageTypeCostEntry) (map[string]float64, map[string]outputCost) {
	typesOut := map[string]float64{}
	outputs := map[string]outputCost{}
	for _, e := range entries {
		cost := float32ToFloat64(e.GetCostPerGb())
		if id := e.GetOutputId(); id != "" {
			outputs[id] = outputCost{storageType: e.GetStorageType(), cost: cost}
			continue
		}
		typesOut[e.GetStorageType()] = cost
	}
	return typesOut, outputs
}

// float32ToFloat64 widens the SDK's float32 by its shortest decimal form, so
// 0.001 stays 0.001 instead of becoming 0.0010000000474974513.
func float32ToFloat64(f float32) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(float64(f), 'g', -1, 32), 64)
	return v
}

// sameCost compares two prices at the precision the API stores (float32).
func sameCost(a, b float64) bool {
	return float32(a) == float32(b)
}

// keepNullIfEmpty returns the refreshed costs map. An attribute the
// configuration never set stays null rather than becoming an empty map (which
// would read as a change); a managed map that lost every entry becomes empty,
// so the plan shows the entries being set again.
func keepNullIfEmpty(ctx context.Context, prior types.Map, m map[string]float64, importing bool, diags *diag.Diagnostics) types.Map {
	if len(m) == 0 && (prior.IsNull() || importing) {
		return types.MapNull(types.Float64Type)
	}
	v, d := types.MapValueFrom(ctx, types.Float64Type, m)
	diags.Append(d...)
	return v
}

// keepNullOutputsIfEmpty is keepNullIfEmpty for output_costs.
func keepNullOutputsIfEmpty(ctx context.Context, prior types.Map, m map[string]storageOutputCost, importing bool, diags *diag.Diagnostics) types.Map {
	if len(m) == 0 && (prior.IsNull() || importing) {
		return types.MapNull(storageOutputCostType)
	}
	v, d := types.MapValueFrom(ctx, storageOutputCostType, m)
	diags.Append(d...)
	return v
}
