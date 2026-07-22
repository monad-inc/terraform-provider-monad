package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceConnectorModel struct {
	ID            types.String             `tfsdk:"id"`
	Name          types.String             `tfsdk:"name"`
	Description   types.String             `tfsdk:"description"`
	ComponentType types.String             `tfsdk:"type"`
	Config        *ResourceConnectorConfig `tfsdk:"config"`
}

type ResourceConnectorConfig struct {
	Settings    types.Dynamic `tfsdk:"settings"`
	Secrets     types.Dynamic `tfsdk:"secrets"`
	SecretsHash types.String  `tfsdk:"secrets_hash"`
}

func (m *ResourceConnectorModel) getSettingsAndSecrets() (map[string]any, map[string]any, error) {
	settings := make(map[string]any)
	secrets := make(map[string]any)

	if m.Config == nil {
		return settings, secrets, nil
	}

	var err error
	if !m.Config.Settings.IsNull() {
		settings, err = tfDynamicToMapAny(m.Config.Settings)
		if err != nil {
			return nil, nil, err
		}
	}
	if !m.Config.Secrets.IsNull() {
		secrets, err = tfDynamicToMapAny(m.Config.Secrets)
		if err != nil {
			return nil, nil, err
		}
	}

	return settings, secrets, nil
}

func getConnectorSchema() schema.Schema {
	return schema.Schema{
		MarkdownDescription: "Monad Connector",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Monad ConnectorIdentifier",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the connector",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the connector",
				Optional:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Type of the connector component",
				Required:            true,
			},
		},

		Blocks: map[string]schema.Block{
			"config": schema.SingleNestedBlock{
				MarkdownDescription: "Connector configuration",
				Attributes: map[string]schema.Attribute{
					"settings": schema.DynamicAttribute{
						MarkdownDescription: "Settings for the connector",
						Optional:            true,
					},
					"secrets": schema.DynamicAttribute{
						MarkdownDescription: "Secrets for the connector. Write-only: the " +
							"value is sent to the Monad API but never persisted in " +
							"Terraform state. Rotation is detected via `secrets_hash`.",
						Optional:  true,
						Sensitive: true,
						WriteOnly: true,
					},
					"secrets_hash": schema.StringAttribute{
						MarkdownDescription: "HMAC fingerprint of `secrets`, used to " +
							"detect when the write-only secret values change. Managed " +
							"by the provider.",
						Computed: true,
					},
				},
			},
		},
	}
}

// finalizeConnectorSecrets prepares the connector config for state after a
// Create/Update. The write-only `secrets` value must never be persisted, so it
// is nulled, and `secrets_hash` is set to the fingerprint of the secrets that
// were sent to the API (null when there are none).
func finalizeConnectorSecrets(ctx context.Context, orgID string, data *ResourceConnectorModel, secrets map[string]any) error {
	if data.Config == nil {
		return nil
	}

	data.Config.Secrets = types.DynamicNull()

	hash, err := computeSecretsHash(ctx, orgID, secrets)
	if err != nil {
		return err
	}
	if hash == "" {
		data.Config.SecretsHash = types.StringNull()
	} else {
		data.Config.SecretsHash = types.StringValue(hash)
	}
	return nil
}

// refreshConnectorSettings updates the config block in state during Read.
// `settings` is reconciled against prior state so genuine drift surfaces while
// the practitioner-authored cty representation is preserved when nothing
// changed. The write-only `secrets` stays null and `secrets_hash` is left as it
// was in prior state (both null on import, where the config block is absent).
func refreshConnectorSettings(data *ResourceConnectorModel, apiSettings map[string]any) error {
	prior := types.DynamicNull()
	if data.Config != nil {
		prior = data.Config.Settings
	}

	reconciled, err := reconcileDynamic(prior, apiSettings)
	if err != nil {
		return err
	}

	if data.Config == nil {
		// The resource has no config block. Only synthesize one when the API
		// actually returned settings to store (e.g. on import); otherwise leave
		// it absent so a block-less resource (like a dev-null sink) does not get
		// a perpetual "remove config" diff.
		if reconciled.IsNull() {
			return nil
		}
		data.Config = &ResourceConnectorConfig{
			SecretsHash: types.StringNull(),
		}
	}
	data.Config.Settings = reconciled
	data.Config.Secrets = types.DynamicNull()
	return nil
}

// modifyConnectorPlanForSecrets surfaces a plan diff when the write-only
// `secrets` change. Write-only values are null in state and cannot produce a
// diff on their own, so without this the provider would never notice a rotated
// secret. It compares a fresh hash of the configured secrets against the stored
// `secrets_hash`; on a mismatch it marks `secrets_hash` unknown, which both
// triggers Update and lets Update recompute the hash without violating apply
// consistency (an unknown planned value accepts any final value).
func modifyConnectorPlanForSecrets(ctx context.Context, orgID string, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// No prior state means a create (or destroy); the hash is computed in Create.
	if req.State.Raw.IsNull() {
		return
	}
	// A planned destroy has a null plan; nothing to reconcile.
	if req.Plan.Raw.IsNull() {
		return
	}

	secretsPath := path.Root("config").AtName("secrets")
	hashPath := path.Root("config").AtName("secrets_hash")

	var secretsDyn types.Dynamic
	if diags := req.Config.GetAttribute(ctx, secretsPath, &secretsDyn); diags.HasError() {
		// config block absent or not a dynamic — nothing to reconcile.
		return
	}

	secrets, err := tfDynamicToMapAny(secretsDyn)
	if err != nil {
		// A malformed secrets value must not silently disable rotation
		// detection — surface it so the practitioner can fix the value.
		resp.Diagnostics.AddAttributeWarning(
			secretsPath,
			"Secret rotation detection skipped",
			fmt.Sprintf(
				"Could not read the configured secrets to detect a rotation: %s. "+
					"A change to `config.secrets` may not trigger an update until this is resolved.",
				err,
			),
		)
		return
	}

	newHash, err := computeSecretsHash(ctx, orgID, secrets)
	if err != nil {
		resp.Diagnostics.AddAttributeWarning(
			secretsPath,
			"Secret rotation detection skipped",
			fmt.Sprintf(
				"Could not hash the configured secrets to detect a rotation: %s. "+
					"A change to `config.secrets` may not trigger an update until this is resolved.",
				err,
			),
		)
		return
	}

	var stateHash types.String
	if diags := req.State.GetAttribute(ctx, hashPath, &stateHash); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	current := ""
	if !stateHash.IsNull() {
		current = stateHash.ValueString()
	}

	if newHash != current {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, hashPath, types.StringUnknown())...)
	}
}
