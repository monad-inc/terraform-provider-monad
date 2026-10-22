package provider

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// typeField is one configuration field from a connector, operation or alert
// type's schema, flattened to a dotted path (PRO-718).
//
// The API describes fields as a tree: a field may have `children`, and a
// `one_of` field has a `discriminator` whose variants each carry their own
// children. Children flatten to `parent.child`; a variant's children flatten
// to `parent.<variant>.child`, because that is where the configuration puts
// them (alert rules write `metric_config = { type = "records", records = {
// threshold = 10 } }`). The discriminator itself becomes `parent.<field>`
// with the variant names as its allowed values.
type typeField struct {
	Path          string
	Name          string
	Type          string
	ItemType      string
	Required      bool
	Immutable     bool
	Default       *string // JSON-encoded, nil when the API gives none
	Description   string
	AllowedValues []string
}

// flattenTypeFields walks a field map (`settings`, `secrets`, `arguments`).
func flattenTypeFields(meta map[string]any) []typeField {
	var out []typeField
	flattenInto(&out, meta, "")
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func flattenInto(out *[]typeField, meta map[string]any, prefix string) {
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		m, ok := meta[key].(map[string]any)
		if !ok {
			continue
		}
		path := prefix + key
		f := typeField{
			Path:        path,
			Name:        str(m["name"]),
			Type:        str(m["type"]),
			ItemType:    firstNonEmpty(str(m["item_type"]), str(m["sub_type"])),
			Required:    m["required"] == true,
			Immutable:   m["immutable"] == true,
			Description: str(m["description"]),
		}
		if d, ok := m["default"]; ok && d != nil {
			if b, err := json.Marshal(d); err == nil {
				s := string(b)
				f.Default = &s
			}
		}
		f.AllowedValues = allowedValues(m)
		*out = append(*out, f)

		if children, ok := m["children"].(map[string]any); ok {
			flattenInto(out, children, path+".")
		}
		if disc, ok := m["discriminator"].(map[string]any); ok {
			variants, _ := disc["one_of"].(map[string]any)
			names := make([]string, 0, len(variants))
			for v := range variants {
				names = append(names, v)
			}
			sort.Strings(names)
			if field := str(disc["field"]); field != "" {
				*out = append(*out, typeField{
					Path:          path + "." + field,
					Name:          str(disc["name"]),
					Type:          "string",
					Required:      true,
					Description:   fmt.Sprintf("Selects which variant of %s applies.", path),
					AllowedValues: names,
				})
			}
			for _, v := range names {
				vm, _ := variants[v].(map[string]any)
				if children, ok := vm["children"].(map[string]any); ok {
					flattenInto(out, children, path+"."+v+".")
				}
			}
		}
	}
}

// allowedValues reads `enum` (plain values) or `named_enum` ({name, value}).
func allowedValues(m map[string]any) []string {
	var out []string
	if e, ok := m["enum"].([]any); ok {
		for _, v := range e {
			out = append(out, scalarToString(v))
		}
	}
	if ne, ok := m["named_enum"].([]any); ok {
		for _, v := range ne {
			if nm, ok := v.(map[string]any); ok {
				out = append(out, scalarToString(nm["value"]))
			}
		}
	}
	return out
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// typeFieldAttrTypes is the object type of one element of a fields list.
var typeFieldAttrTypes = map[string]attr.Type{
	"path":           types.StringType,
	"name":           types.StringType,
	"type":           types.StringType,
	"item_type":      types.StringType,
	"required":       types.BoolType,
	"immutable":      types.BoolType,
	"default":        types.StringType,
	"description":    types.StringType,
	"allowed_values": types.ListType{ElemType: types.StringType},
}

// typeFieldsValue converts flattened fields to a Terraform list of objects.
func typeFieldsValue(fields []typeField) (types.List, error) {
	elemType := types.ObjectType{AttrTypes: typeFieldAttrTypes}
	elems := make([]attr.Value, 0, len(fields))
	for _, f := range fields {
		allowed := make([]attr.Value, 0, len(f.AllowedValues))
		for _, a := range f.AllowedValues {
			allowed = append(allowed, types.StringValue(a))
		}
		def := types.StringNull()
		if f.Default != nil {
			def = types.StringValue(*f.Default)
		}
		obj, diags := types.ObjectValue(typeFieldAttrTypes, map[string]attr.Value{
			"path":           types.StringValue(f.Path),
			"name":           types.StringValue(f.Name),
			"type":           types.StringValue(f.Type),
			"item_type":      types.StringValue(f.ItemType),
			"required":       types.BoolValue(f.Required),
			"immutable":      types.BoolValue(f.Immutable),
			"default":        def,
			"description":    types.StringValue(f.Description),
			"allowed_values": types.ListValueMust(types.StringType, allowed),
		})
		if diags.HasError() {
			return types.ListNull(elemType), fmt.Errorf("building field %s: %v", f.Path, diags)
		}
		elems = append(elems, obj)
	}
	list, diags := types.ListValue(elemType, elems)
	if diags.HasError() {
		return types.ListNull(elemType), fmt.Errorf("building fields: %v", diags)
	}
	return list, nil
}
