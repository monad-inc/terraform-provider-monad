package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var RegisteredConnectorResources = make([]func() resource.Resource, 0)

type BaseConnectorModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

type BaseConnectorConfig struct {
	Settings map[string]any
	Secrets  map[string]any
}

type ConnectorResourceModel interface {
	GetBaseModel() *BaseConnectorModel
	GetSettingsAndSecrets() BaseConnectorConfig
	UpdateFromAPIResponse(connector any) error
}
