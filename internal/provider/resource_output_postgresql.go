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
	"github.com/hashicorp/terraform-plugin-log/tflog"

	monad "github.com/monad-inc/sdk/go"
	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

var _ resource.Resource = &ResourceOutputPostgreSQL{}
var _ resource.ResourceWithImportState = &ResourceOutputPostgreSQL{}

func NewResourceOutputPostgreSQL() resource.Resource {
	return &ResourceOutputPostgreSQL{}
}

type ResourceOutputPostgreSQL struct {
	client *client.Client
}

type ResourceOutputPostgreSQLModel struct {
	ID          types.String                    `tfsdk:"id"`
	Name        types.String                    `tfsdk:"name"`
	Description types.String                    `tfsdk:"description"`
	Config      *ResourceOutputPostgreSQLConfig `tfsdk:"config"`
}

type ResourceOutputPostgreSQLConfig struct {
	Settings *ResourceOutputPostgreSQLConfigSettings `tfsdk:"settings"`
	Secrets  *ResourceOutputPostgreSQLConfigSecrets  `tfsdk:"secrets"`
}

type ResourceOutputPostgreSQLConfigSettings struct {
	Host        types.String   `tfsdk:"host"`
	Port        types.Int64    `tfsdk:"port"`
	Database    types.String   `tfsdk:"database"`
	Table       types.String   `tfsdk:"table"`
	User        types.String   `tfsdk:"user"`
	ColumnNames []types.String `tfsdk:"column_names"`
}

type ResourceOutputPostgreSQLConfigSecrets struct {
	ConnectionString types.String `tfsdk:"connection_string"`
	Password         types.String `tfsdk:"password"`
}

func (r *ResourceOutputPostgreSQL) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_output_postgresql"
}

func (r *ResourceOutputPostgreSQL) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "PostgreSQL output resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Output identifier",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the output",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the output",
				Optional:            true,
			},
		},

		Blocks: map[string]schema.Block{
			"config": schema.SingleNestedBlock{
				MarkdownDescription: "PostgreSQL output configuration",
				Blocks: map[string]schema.Block{
					"settings": schema.SingleNestedBlock{
						MarkdownDescription: "PostgreSQL settings configuration",
						Attributes: map[string]schema.Attribute{
							"host": schema.StringAttribute{
								MarkdownDescription: "The host of the PostgreSQL database",
								Required:            true,
							},
							"port": schema.Int64Attribute{
								MarkdownDescription: "The port of the PostgreSQL database",
								Optional:            true,
							},
							"database": schema.StringAttribute{
								MarkdownDescription: "The database name to connect to",
								Required:            true,
							},
							"table": schema.StringAttribute{
								MarkdownDescription: "The table name to write data to",
								Required:            true,
							},
							"user": schema.StringAttribute{
								MarkdownDescription: "The user to connect to the PostgreSQL database",
								Required:            true,
							},
							"column_names": schema.ListAttribute{
								MarkdownDescription: "The column names to write data to, must match the root fields of the data",
								ElementType:         types.StringType,
								Optional:            true,
							},
						},
					},
					"secrets": schema.SingleNestedBlock{
						MarkdownDescription: "PostgreSQL secrets configuration",
						Attributes: map[string]schema.Attribute{
							"connection_string": schema.StringAttribute{
								MarkdownDescription: "Complete PostgreSQL connection string",
								Optional:            true,
								Sensitive:           true,
							},
							"password": schema.StringAttribute{
								MarkdownDescription: "Password for the PostgreSQL user",
								Optional:            true,
								Sensitive:           true,
							},
						},
					},
				},
			},
		},
	}
}

func (r *ResourceOutputPostgreSQL) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ResourceOutputPostgreSQL) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ResourceOutputPostgreSQLModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.Config == nil {
		resp.Diagnostics.AddError("Missing Config", "PostgreSQL output configuration is required.")
		return
	}

	settings, secrets := r.getSettingsAndSecretsFromConfig(&data)

	request := monad.RoutesV2CreateOutputRequest{
		Name:        data.Name.ValueStringPointer(),
		Description: data.Description.ValueStringPointer(),
		OutputType:  ptr("postgresql"),
		Config: &monad.SecretProcessesorOutputConfig{
			Settings: &monad.SecretProcessesorOutputConfigSettings{
				MapmapOfStringAny: &settings,
			},
			Secrets: &monad.SecretProcessesorOutputConfigSecrets{
				MapmapOfStringAny: &secrets,
			},
		},
	}

	output, _, err := r.client.OrganizationOutputsAPI.
		V2OrganizationIdOutputsPost(ctx, r.client.OrganizationID).
		RoutesV2CreateOutputRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create PostgreSQL output, got error: %s", err))
		return
	}

	data.ID = types.StringValue(*output.Id)

	tflog.Trace(ctx, "created an HTTP output resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceOutputPostgreSQL) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ResourceOutputPostgreSQLModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	output, _, err := r.client.OrganizationOutputsAPI.
		V1OrganizationIdOutputsOutputIdGet(ctx, r.client.OrganizationID, data.ID.ValueString()).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read PostgreSQL output, got error: %s", err))
		return
	}

	columns := make([]types.String, 0, len(output.Config.Settings["column_names"].([]string)))
	for _, col := range output.Config.Settings["column_names"].([]string) {
		columns = append(columns, types.StringValue(col))
	}

	data.ID = types.StringValue(*output.Id)
	data.Name = types.StringValue(*output.Name)
	data.Description = types.StringValue(*output.Description)
	data.Config = &ResourceOutputPostgreSQLConfig{
		Settings: &ResourceOutputPostgreSQLConfigSettings{
			Host:        types.StringValue(output.Config.Settings["host"].(string)),
			Port:        types.Int64Value(output.Config.Settings["port"].(int64)),
			Database:    types.StringValue(output.Config.Settings["database"].(string)),
			Table:       types.StringValue(output.Config.Settings["table"].(string)),
			User:        types.StringValue(output.Config.Settings["user"].(string)),
			ColumnNames: columns,
		},
		Secrets: &ResourceOutputPostgreSQLConfigSecrets{
			ConnectionString: types.StringValue(output.Config.Secrets["connection_string"].(string)),
			Password:         types.StringValue(output.Config.Secrets["password"].(string)),
		},
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceOutputPostgreSQL) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ResourceOutputPostgreSQLModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.Config == nil {
		resp.Diagnostics.AddError("Missing Config", "The config block is required for the PostgreSQL output resource.")
		return
	}

	settings, secrets := r.getSettingsAndSecretsFromConfig(&data)

	request := monad.RoutesV2PutOutputRequest{
		Name:        data.Name.ValueStringPointer(),
		Description: data.Description.ValueStringPointer(),
		OutputType:  ptr("http"),
		Config: &monad.SecretProcessesorOutputConfig{
			Settings: &monad.SecretProcessesorOutputConfigSettings{
				MapmapOfStringAny: &settings,
			},
			Secrets: &monad.SecretProcessesorOutputConfigSecrets{
				MapmapOfStringAny: &secrets,
			},
		},
	}

	output, _, err := r.client.OrganizationOutputsAPI.
		V2OrganizationIdOutputsOutputIdPut(ctx, r.client.OrganizationID, data.ID.ValueString()).
		RoutesV2PutOutputRequest(request).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update HTTP output, got error: %s", err))
		return
	}

	columns := make([]types.String, 0, len(output.Config.Settings["column_names"].([]string)))
	for _, col := range output.Config.Settings["column_names"].([]string) {
		columns = append(columns, types.StringValue(col))
	}

	data.ID = types.StringValue(*output.Id)
	data.Name = types.StringValue(*output.Name)
	data.Description = types.StringValue(*output.Description)
	data.Config = &ResourceOutputPostgreSQLConfig{
		Settings: &ResourceOutputPostgreSQLConfigSettings{
			Host:        types.StringValue(output.Config.Settings["host"].(string)),
			Port:        types.Int64Value(output.Config.Settings["port"].(int64)),
			Database:    types.StringValue(output.Config.Settings["database"].(string)),
			Table:       types.StringValue(output.Config.Settings["table"].(string)),
			User:        types.StringValue(output.Config.Settings["user"].(string)),
			ColumnNames: columns,
		},
		Secrets: &ResourceOutputPostgreSQLConfigSecrets{
			ConnectionString: types.StringValue(output.Config.Secrets["connection_string"].(string)),
			Password:         types.StringValue(output.Config.Secrets["password"].(string)),
		},
	}

	tflog.Trace(ctx, "updated an PostgreSQL output resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ResourceOutputPostgreSQL) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ResourceOutputPostgreSQLModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, _, err := r.client.OrganizationOutputsAPI.
		V1OrganizationIdOutputsOutputIdDelete(ctx, r.client.OrganizationID, data.ID.ValueString()).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete PostgreSQL output, got error: %s", err))
		return
	}
}

func (r *ResourceOutputPostgreSQL) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *ResourceOutputPostgreSQL) getSettingsAndSecretsFromConfig(config *ResourceOutputPostgreSQLModel) (map[string]any, map[string]any) {
	settings := make(map[string]any)
	secrets := make(map[string]any)

	if config.Config.Settings != nil {
		if !config.Config.Settings.Host.IsNull() {
			settings["host"] = config.Config.Settings.Host.ValueString()
		}
		if !config.Config.Settings.Port.IsNull() {
			settings["port"] = config.Config.Settings.Port.ValueInt64()
		}
		if !config.Config.Settings.Database.IsNull() {
			settings["database"] = config.Config.Settings.Database.ValueString()
		}
		if !config.Config.Settings.Table.IsNull() {
			settings["table"] = config.Config.Settings.Table.ValueString()
		}
		if !config.Config.Settings.User.IsNull() {
			settings["user"] = config.Config.Settings.User.ValueString()
		}
		if config.Config.Settings.ColumnNames != nil {
			columnNames := make([]string, len(config.Config.Settings.ColumnNames))
			for i, col := range config.Config.Settings.ColumnNames {
				columnNames[i] = col.ValueString()
			}
			settings["column_names"] = columnNames
		}
	}

	if config.Config.Secrets != nil {
		secrets := make(map[string]interface{})

		if !config.Config.Secrets.ConnectionString.IsNull() {
			secrets["connection_string"] = config.Config.Secrets.ConnectionString.ValueString()
		}
		if !config.Config.Secrets.Password.IsNull() {
			secrets["password"] = config.Config.Secrets.Password.ValueString()
		}
	}

	return settings, secrets
}
