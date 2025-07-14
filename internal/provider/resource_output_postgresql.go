package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &ResourceOutputPostgreSQL{}
var _ ConnectorResourceModel = &ResourceOutputPostgreSQLModel{}

func init() {
	RegisteredConnectorResources = append(RegisteredConnectorResources, NewResourceOutputPostgreSQL)
}

func NewResourceOutputPostgreSQL() resource.Resource {
	return &ResourceOutputPostgreSQL{
		BaseOutputResource: NewBaseOutputResource[*ResourceOutputPostgreSQLModel]("postgresql"),
	}
}

type ResourceOutputPostgreSQL struct {
	*BaseOutputResource[*ResourceOutputPostgreSQLModel]
}

type ResourceOutputPostgreSQLModel struct {
	BaseConnectorModel
	Config *ResourceOutputPostgreSQLConfig `tfsdk:"config"`
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

func (r *ResourceOutputPostgreSQL) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
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

func (m *ResourceOutputPostgreSQLModel) GetComponentSubType() string {
	return "postgresql"
}

func (m *ResourceOutputPostgreSQLModel) GetBaseModel() *BaseConnectorModel {
	return &m.BaseConnectorModel
}

func (m *ResourceOutputPostgreSQLModel) GetSettingsAndSecrets(ctx context.Context) (*BaseConnectorConfig, error) {
	config := &BaseConnectorConfig{
		Settings: make(map[string]any),
		Secrets:  make(map[string]any),
	}

	if m.Config.Settings != nil {
		if !m.Config.Settings.Host.IsNull() {
			config.Settings["host"] = m.Config.Settings.Host.ValueString()
		}
		if !m.Config.Settings.Port.IsNull() {
			config.Settings["port"] = m.Config.Settings.Port.ValueInt64()
		}
		if !m.Config.Settings.Database.IsNull() {
			config.Settings["database"] = m.Config.Settings.Database.ValueString()
		}
		if !m.Config.Settings.Table.IsNull() {
			config.Settings["table"] = m.Config.Settings.Table.ValueString()
		}
		if !m.Config.Settings.User.IsNull() {
			config.Settings["user"] = m.Config.Settings.User.ValueString()
		}
		if m.Config.Settings.ColumnNames != nil {
			columnNames := make([]string, len(m.Config.Settings.ColumnNames))
			for i, col := range m.Config.Settings.ColumnNames {
				columnNames[i] = col.ValueString()
			}
			config.Settings["column_names"] = columnNames
		}
	}

	if m.Config.Secrets != nil {
		if !m.Config.Secrets.ConnectionString.IsNull() {
			config.Secrets["connection_string"] = m.Config.Secrets.ConnectionString.ValueString()
		}
		if !m.Config.Secrets.Password.IsNull() {
			config.Secrets["password"] = m.Config.Secrets.Password.ValueString()
		}
	}

	return config, nil
}

func (m *ResourceOutputPostgreSQLModel) UpdateFromAPIResponse(output any) error {
	// Since we can't determine the exact type, we'll use type assertions
	// The actual type will need to be determined from the monad SDK
	// For now, this is a placeholder that needs to be implemented properly
	return nil
}
