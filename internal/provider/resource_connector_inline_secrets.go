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
// reuseInlineSecretsInPlace), plus the in-place secret updates that reuse
// needs. The caller applies those with applyInPlaceSecretUpdates only after
// the component update succeeds. The component is only read when an inline
// secret is configured.
//
// secretsChanged reports whether the configured secrets differ from the last
// ones applied (the planned `secrets_hash` is unknown). When they don't, an
// inline secret is sent as a reference to the secret its slot already holds
// and is never written: re-sending its value on an unrelated change would undo
// a rotation made outside Terraform.
func secretsForUpdate(
	ctx context.Context,
	c *client.Client,
	kind string,
	selfID string,
	configured map[string]any,
	secretsChanged bool,
	readStored storedSecretsFunc,
) (map[string]any, []inPlaceSecretUpdate, diag.Diagnostics) {
	if !hasInlineSecrets(configured) {
		return configured, nil, nil
	}

	stored, httpResp, err := readStored(ctx)
	if err != nil {
		var diags diag.Diagnostics
		diags.AddError("Client Error", fmt.Sprintf(
			"Unable to read %s before update, got error: %s. Response: %s",
			kind, err, getResponseBody(httpResp),
		))
		return nil, nil, diags
	}
	if !secretsChanged {
		return referenceStoredSecrets(configured, stored), nil, nil
	}
	return reuseInlineSecretsInPlace(ctx, c, configured, stored, selfID)
}

// referenceStoredSecrets sends each inline secret as a reference to the secret
// its slot already holds, without reading or writing that secret. An inline
// slot with no stored reference is sent unchanged.
func referenceStoredSecrets(configured, stored map[string]any) map[string]any {
	out := make(map[string]any, len(configured))
	for key, value := range configured {
		storedValue := stored[key]
		if _, ok := inlineSecret(value); ok {
			out[key] = value
			if id := secretRefID(storedValue); id != "" {
				out[key] = map[string]any{"id": id}
			}
			continue
		}
		if nested, ok := value.(map[string]any); ok && !isSecretRef(nested) {
			storedNested, _ := storedValue.(map[string]any)
			out[key] = referenceStoredSecrets(nested, storedNested)
			continue
		}
		out[key] = value
	}
	return out
}

// The API turns every inline secret ({ name, value, description }) on a
// connector write into a new secret, and refuses a name that already exists
// with a 409. So re-sending an unchanged inline secret on update — after an
// import, or when only its value changed — always failed (ENG-11264).
//
// reuseInlineSecretsInPlace rewrites the secrets about to be sent on update:
// an inline secret whose name matches the secret that slot already references
// is sent as an { id } reference, and an update of that secret (value and
// description) is returned for the caller to apply once the component update
// has succeeded. Anything else is sent unchanged, so a new name still creates
// a new secret and a name owned by an unrelated secret still gets the API's
// 409 rather than being overwritten.
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
) (map[string]any, []inPlaceSecretUpdate, diag.Diagnostics) {
	var diags diag.Diagnostics
	var updates []inPlaceSecretUpdate
	out := reuseInlineSecretsAt(ctx, c, configured, stored, selfID, path.Root("config").AtName("secrets"), &updates, &diags)
	return out, updates, diags
}

func reuseInlineSecretsAt(
	ctx context.Context,
	c *client.Client,
	configured, stored map[string]any,
	selfID string,
	at path.Path,
	updates *[]inPlaceSecretUpdate,
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
			if update, reused := matchReferencedSecret(ctx, c, secret, storedID, selfID, slotPath, diags); reused {
				out[key] = map[string]any{"id": storedID}
				*updates = append(*updates, update)
			}
			continue
		}

		// One level of nested slots (a map of secrets), as the API allows.
		if nested, ok := value.(map[string]any); ok && !isSecretRef(nested) {
			storedNested, _ := storedValue.(map[string]any)
			out[key] = reuseInlineSecretsAt(ctx, c, nested, storedNested, selfID, slotPath, updates, diags)
			continue
		}

		out[key] = value
	}
	return out
}

// inPlaceSecretUpdate is a pending update of a secret a slot already
// references, deferred until the component update has succeeded.
type inPlaceSecretUpdate struct {
	slotPath path.Path
	id       string
	want     inlineSecretValue
	// others names the other components using the secret, for the warning.
	others []string
}

// matchReferencedSecret reads the secret the slot already references and,
// when its name matches the configured inline secret, returns the update that
// reuses it in place and reports that the slot can be sent as a reference.
func matchReferencedSecret(
	ctx context.Context,
	c *client.Client,
	want inlineSecretValue,
	storedID, selfID string,
	slotPath path.Path,
	diags *diag.Diagnostics,
) (inPlaceSecretUpdate, bool) {
	current, httpResp, err := c.SecretsAPI.GetSecret(ctx, c.OrganizationID, storedID).Execute()
	if err != nil {
		body := getResponseBody(httpResp)
		if isNotFoundResponse(httpResp, body) {
			// The referenced secret is gone; let the API create the inline one.
			return inPlaceSecretUpdate{}, false
		}
		diags.AddAttributeError(slotPath, "Client Error", fmt.Sprintf(
			"Unable to read secret %s currently referenced by this slot, got error: %s. Response: %s",
			storedID, err, body,
		))
		return inPlaceSecretUpdate{}, false
	}

	if current.GetName() != want.name {
		// A renamed inline secret is a new secret; the API creates it.
		return inPlaceSecretUpdate{}, false
	}

	return inPlaceSecretUpdate{
		slotPath: slotPath,
		id:       storedID,
		want:     want,
		others:   otherSecretUsers(current, selfID),
	}, true
}

// applyInPlaceSecretUpdates writes the updates reuseInlineSecretsInPlace
// deferred. The caller runs it only after the component update succeeds, so a
// rejected component update never changes a secret. If a secret update fails
// here, Update returns the error without saving state; the next plan shows the
// same change and the retry re-sends the same values.
func applyInPlaceSecretUpdates(
	ctx context.Context,
	c *client.Client,
	kind string,
	updates []inPlaceSecretUpdate,
) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, u := range updates {
		request := monad.RoutesV2CreateOrUpdateSecretRequest{
			Name:        &u.want.name,
			Description: &u.want.description,
			Value:       &u.want.value,
		}
		_, httpResp, err := c.SecretsAPI.
			UpdateSecret(ctx, c.OrganizationID, u.id).
			CreateSecretRequest(monad.RoutesV2CreateOrUpdateSecretRequestAsCreateSecretRequest(&request)).
			Execute()
		if err != nil {
			diags.AddAttributeError(u.slotPath, "Client Error", fmt.Sprintf(
				"The %s was updated, but updating secret %q (%s) in place failed, got error: %s. Response: %s. "+
					"Run apply again to retry; other secrets in this update may already hold their new value.",
				kind, u.want.name, u.id, err, getResponseBody(httpResp),
			))
			return diags
		}

		if len(u.others) > 0 {
			diags.AddAttributeWarning(u.slotPath, "Shared secret updated", fmt.Sprintf(
				"Secret %q is also used by %s. Its value and description were updated in place, "+
					"so those components now use the configured value too.",
				u.want.name, strings.Join(u.others, ", "),
			))
		}
	}
	return diags
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
