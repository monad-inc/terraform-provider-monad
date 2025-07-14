package provider

import (
	"context"
	"fmt"
	"strings"

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

type ConnectorSecret struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Value       types.String `tfsdk:"value"`
}

type ConnectorResourceModel interface {
	GetComponentSubType() string
	GetBaseModel() *BaseConnectorModel
	GetSettingsAndSecrets(ctx context.Context) (*BaseConnectorConfig, error)
	UpdateFromAPIResponse(connector any) error
}

func getConnectorTypeName(providerTypeName, componentType, componentSubType string) string {
	name := fmt.Sprintf("%s_%s", providerTypeName, componentType)
	if componentSubType != "" {
		name += "_" + componentSubType
	}
	return strings.ReplaceAll(name, "-", "_")
}
