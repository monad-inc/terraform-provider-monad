package provider

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"

	monad "github.com/monad-inc/sdk/go"
	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

// storedSecretsFunc reads a component's current config.secrets from the API.
type storedSecretsFunc func(ctx context.Context) (map[string]any, *http.Response, error)

// secretsForUpdate returns the secrets to send on a connector update: the
// configured ones, with inline secrets reused in place where possible (see
// reuseInlineSecretsInPlace). The component is only read when an inline
// secret is configured.
func secretsForUpdate(
	ctx context.Context,
	c *client.Client,
	kind string,
	selfID string,
	configured map[string]any,
	readStored storedSecretsFunc,
) (map[string]any, diag.Diagnostics) {
	if !hasInlineSecrets(configured) {
		return configured, nil
	}

	stored, httpResp, err := readStored(ctx)
	if err != nil {
		var diags diag.Diagnostics
		diags.AddError("Client Error", fmt.Sprintf(
			"Unable to read %s before update, got error: %s. Response: %s",
			kind, err, getResponseBody(httpResp),
		))
		return nil, diags
	}
	return reuseInlineSecretsInPlace(ctx, c, configured, stored, selfID)
}

// The API turns every inline secret ({ name, value, description }) on a
// connector write into a new secret, and refuses a name that already exists
// with a 409. So re-sending an unchanged inline secret on update — after an
// import, or when only its value changed — always failed (ENG-11264).
//
// reuseInlineSecretsInPlace rewrites the secrets about to be sent on update:
// an inline secret whose name matches the secret that slot already references
// updates that secret in place (value and description) and is sent as an
// { id } reference. Anything else is sent unchanged, so a new name still
// creates a new secret and a name owned by an unrelated secret still gets the
// API's 409 rather than being overwritten.
//
// configured is never modified; the caller hashes it for rotation detection.
// stored is the component's current config.secrets as the API returns it
// (slot → { id }, or one level of nested slots). selfID is the component's
// own id, excluded when warning that a secret is shared.
func reuseInlineSecretsInPlace(
	ctx context.Context,
	c *client.Client,
	configured, stored map[string]any,
	selfID string,
) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	out := reuseInlineSecretsAt(ctx, c, configured, stored, selfID, path.Root("config").AtName("secrets"), &diags)
	return out, diags
}

func reuseInlineSecretsAt(
	ctx context.Context,
	c *client.Client,
	configured, stored map[string]any,
	selfID string,
	at path.Path,
	diags *diag.Diagnostics,
) map[string]any {
	out := make(map[string]any, len(configured))
	for key, value := range configured {
		slotPath := at.AtMapKey(key)
		storedValue, _ := stored[key]

		if secret, ok := inlineSecret(value); ok {
			out[key] = value
			storedID := secretRefID(storedValue)
			if storedID == "" {
				continue
			}
			if id, reused := updateReferencedSecret(ctx, c, secret, storedID, selfID, slotPath, diags); reused {
				out[key] = map[string]any{"id": id}
			}
			continue
		}

		// One level of nested slots (a map of secrets), as the API allows.
		if nested, ok := value.(map[string]any); ok && !isSecretRef(nested) {
			storedNested, _ := storedValue.(map[string]any)
			out[key] = reuseInlineSecretsAt(ctx, c, nested, storedNested, selfID, slotPath, diags)
			continue
		}

		out[key] = value
	}
	return out
}

// updateReferencedSecret updates the secret the slot already references when
// its name matches the configured inline secret, and reports whether the slot
// can be sent as a reference to it.
func updateReferencedSecret(
	ctx context.Context,
	c *client.Client,
	want inlineSecretValue,
	storedID, selfID string,
	slotPath path.Path,
	diags *diag.Diagnostics,
) (string, bool) {
	current, httpResp, err := c.SecretsAPI.GetSecret(ctx, c.OrganizationID, storedID).Execute()
	if err != nil {
		body := getResponseBody(httpResp)
		if isNotFoundResponse(httpResp, body) {
			// The referenced secret is gone; let the API create the inline one.
			return "", false
		}
		diags.AddAttributeError(slotPath, "Client Error", fmt.Sprintf(
			"Unable to read secret %s currently referenced by this slot, got error: %s. Response: %s",
			storedID, err, body,
		))
		return "", false
	}

	if current.GetName() != want.name {
		// A renamed inline secret is a new secret; the API creates it.
		return "", false
	}

	request := monad.RoutesV2CreateOrUpdateSecretRequest{
		Name:        &want.name,
		Description: &want.description,
		Value:       &want.value,
	}
	_, httpResp, err = c.SecretsAPI.
		UpdateSecret(ctx, c.OrganizationID, storedID).
		CreateSecretRequest(monad.RoutesV2CreateOrUpdateSecretRequestAsCreateSecretRequest(&request)).
		Execute()
	if err != nil {
		diags.AddAttributeError(slotPath, "Client Error", fmt.Sprintf(
			"Unable to update secret %q (%s) in place, got error: %s. Response: %s",
			want.name, storedID, err, getResponseBody(httpResp),
		))
		return "", false
	}

	if others := otherSecretUsers(current, selfID); len(others) > 0 {
		diags.AddAttributeWarning(slotPath, "Shared secret updated", fmt.Sprintf(
			"Secret %q is also used by %s. Its value and description were updated in place, "+
				"so those components now use the configured value too.",
			want.name, strings.Join(others, ", "),
		))
	}
	return storedID, true
}

type inlineSecretValue struct {
	name, value, description string
}

// inlineSecret reports whether v is an inline secret in the form the API
// accepts: name, value and description all non-empty strings, and no id.
func inlineSecret(v any) (inlineSecretValue, bool) {
	m, ok := v.(map[string]any)
	if !ok || isSecretRef(m) {
		return inlineSecretValue{}, false
	}
	name, _ := m["name"].(string)
	value, _ := m["value"].(string)
	description, _ := m["description"].(string)
	if name == "" || value == "" || description == "" {
		return inlineSecretValue{}, false
	}
	return inlineSecretValue{name: name, value: value, description: description}, true
}

func isSecretRef(m map[string]any) bool {
	id, _ := m["id"].(string)
	return id != ""
}

func secretRefID(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	id, _ := m["id"].(string)
	return id
}

// hasInlineSecrets reports whether any slot (or nested slot) is an inline
// secret, so updates without one skip the extra reads.
func hasInlineSecrets(secrets map[string]any) bool {
	for _, v := range secrets {
		if _, ok := inlineSecret(v); ok {
			return true
		}
		if nested, ok := v.(map[string]any); ok && !isSecretRef(nested) && hasInlineSecrets(nested) {
			return true
		}
	}
	return false
}

func otherSecretUsers(s *monad.RoutesV2SecretWithComponentsResponse, selfID string) []string {
	var users []string
	add := func(kind string, refs []monad.ModelsComponentReference) {
		for _, ref := range refs {
			if ref.GetId() == selfID {
				continue
			}
			users = append(users, fmt.Sprintf("%s %q", kind, ref.GetName()))
		}
	}
	add("input", s.Inputs)
	add("output", s.Outputs)
	add("transform", s.Transforms)
	add("enrichment", s.Enrichments)
	sort.Strings(users)
	return users
}
