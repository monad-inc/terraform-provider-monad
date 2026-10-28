package provider

import (
	"context"
	"encoding/json"
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
//
// owned is the set of slots the last apply sent inline (see inlineSlots); nil
// when there is no record.
func secretsForUpdate(
	ctx context.Context,
	c *client.Client,
	kind string,
	selfID string,
	configured map[string]any,
	secretsChanged bool,
	owned inlineSlots,
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
	return reuseInlineSecretsInPlace(ctx, c, kind, configured, stored, selfID, owned)
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
// A matching name alone does not prove the secret belongs to this component:
// a slot that referenced a separately managed secret by { id } and is switched
// to an inline secret of the same name would overwrite it. So the update is
// refused unless owned (the slots the last apply sent inline) lists the slot.
// With no record — after an import, or state written by an older provider —
// the update is allowed only when no other component uses the secret.
//
// configured is never modified; the caller hashes it for rotation detection.
// stored is the component's current config.secrets as the API returns it
// (slot → { id }, or one level of nested slots). selfID is the component's
// own id, excluded when warning that a secret is shared.
func reuseInlineSecretsInPlace(
	ctx context.Context,
	c *client.Client,
	kind string,
	configured, stored map[string]any,
	selfID string,
	owned inlineSlots,
) (map[string]any, []inPlaceSecretUpdate, diag.Diagnostics) {
	r := secretReuse{c: c, kind: kind, selfID: selfID, owned: owned}
	out := r.at(ctx, configured, stored, path.Root("config").AtName("secrets"), nil)
	return out, r.updates, r.diags
}

// secretReuse carries one reuseInlineSecretsInPlace walk.
type secretReuse struct {
	c       *client.Client
	kind    string
	selfID  string
	owned   inlineSlots
	updates []inPlaceSecretUpdate
	diags   diag.Diagnostics
}

func (r *secretReuse) at(
	ctx context.Context,
	configured, stored map[string]any,
	at path.Path,
	keys []string,
) map[string]any {
	out := make(map[string]any, len(configured))
	for key, value := range configured {
		slotPath := at.AtMapKey(key)
		slot := append(append([]string(nil), keys...), key)
		storedValue, _ := stored[key]

		if secret, ok := inlineSecret(value); ok {
			out[key] = value
			storedID := secretRefID(storedValue)
			if storedID == "" {
				continue
			}
			if update, reused := r.match(ctx, secret, storedID, slotPath, slot); reused {
				out[key] = map[string]any{"id": storedID}
				r.updates = append(r.updates, update)
			}
			continue
		}

		// One level of nested slots (a map of secrets), as the API allows.
		if nested, ok := value.(map[string]any); ok && !isSecretRef(nested) {
			storedNested, _ := storedValue.(map[string]any)
			out[key] = r.at(ctx, nested, storedNested, slotPath, slot)
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

// match reads the secret the slot already references and, when its name
// matches the configured inline secret and this component may update it,
// returns the update that reuses it in place and reports that the slot can be
// sent as a reference.
func (r *secretReuse) match(
	ctx context.Context,
	want inlineSecretValue,
	storedID string,
	slotPath path.Path,
	slot []string,
) (inPlaceSecretUpdate, bool) {
	c := r.c
	current, httpResp, err := c.SecretsAPI.GetSecret(ctx, c.OrganizationID, storedID).Execute()
	if err != nil {
		body := getResponseBody(httpResp)
		if isNotFoundResponse(httpResp, body) {
			// The referenced secret is gone; let the API create the inline one.
			return inPlaceSecretUpdate{}, false
		}
		r.diags.AddAttributeError(slotPath, "Client Error", fmt.Sprintf(
			"Unable to read secret %s currently referenced by this slot, got error: %s. Response: %s",
			storedID, err, body,
		))
		return inPlaceSecretUpdate{}, false
	}

	if current.GetName() != want.name {
		// A renamed inline secret is a new secret; the API creates it.
		return inPlaceSecretUpdate{}, false
	}

	others := otherSecretUsers(current, r.selfID)
	switch {
	case r.owned != nil && !r.owned.has(slot):
		r.diags.AddAttributeError(slotPath, "Secret not owned by this "+r.kind, fmt.Sprintf(
			"This slot referenced secret %q (%s) by id when last applied, so it is not an inline secret "+
				"this %s created, and the provider will not overwrite it. Keep the { id } reference and change "+
				"the secret where it is managed, or give the inline secret a new name.",
			want.name, storedID, r.kind,
		))
		return inPlaceSecretUpdate{}, false
	case r.owned == nil && len(others) > 0:
		r.diags.AddAttributeError(slotPath, "Shared secret not updated", fmt.Sprintf(
			"Secret %q (%s) is also used by %s, and the provider has no record of whether this %s created it "+
				"(it was imported, or last applied by an older provider version), so it will not update it in "+
				"place. Reference it as { id = %q } and change it where it is managed, or give the inline "+
				"secret a new name.",
			want.name, storedID, strings.Join(others, ", "), r.kind, storedID,
		))
		return inPlaceSecretUpdate{}, false
	}

	return inPlaceSecretUpdate{
		slotPath: slotPath,
		id:       storedID,
		want:     want,
		others:   others,
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

// inlineSecretSlotsKey is the private-state key under which a connector
// records the slots its last create or update sent inline.
const inlineSecretSlotsKey = "inline_secret_slots"

// inlineSlots is the set of secret slots (each a path of map keys under
// config.secrets) a connector last sent inline. It is kept in private state
// because `secrets` is write-only, so state cannot say whether a slot held an
// inline secret or an { id } reference. nil means there is no record.
type inlineSlots map[string]struct{}

func slotKey(slot []string) string {
	b, _ := json.Marshal(slot)
	return string(b)
}

func (s inlineSlots) has(slot []string) bool {
	_, ok := s[slotKey(slot)]
	return ok
}

// collectInlineSlots returns the slots of secrets that hold an inline secret.
// It is never nil, so an empty record still counts as a record.
func collectInlineSlots(secrets map[string]any) inlineSlots {
	slots := inlineSlots{}
	var walk func(m map[string]any, keys []string)
	walk = func(m map[string]any, keys []string) {
		for key, value := range m {
			slot := append(append([]string(nil), keys...), key)
			if _, ok := inlineSecret(value); ok {
				slots[slotKey(slot)] = struct{}{}
				continue
			}
			if nested, ok := value.(map[string]any); ok && !isSecretRef(nested) {
				walk(nested, slot)
			}
		}
	}
	walk(secrets, nil)
	return slots
}

// privateGetter and privateSetter are the parts of the framework's private
// state data the connectors use.
type privateGetter interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
}

type privateSetter interface {
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

// readInlineSlots returns the recorded inline slots, or nil when there is no
// record (after an import, or state written by an older provider version).
func readInlineSlots(ctx context.Context, p privateGetter) (inlineSlots, diag.Diagnostics) {
	b, diags := p.GetKey(ctx, inlineSecretSlotsKey)
	if diags.HasError() || b == nil {
		return nil, diags
	}
	var list [][]string
	if err := json.Unmarshal(b, &list); err != nil {
		// Treat an unreadable record as absent (shared secrets are then
		// refused) and warn rather than fail the apply.
		diags.AddWarning("Inline secret record unreadable", fmt.Sprintf(
			"Could not read the provider's record of inline secret slots: %s. "+
				"Treating it as absent; the next successful apply rewrites it.", err,
		))
		return nil, diags
	}
	slots := inlineSlots{}
	for _, slot := range list {
		slots[slotKey(slot)] = struct{}{}
	}
	return slots, diags
}

// writeInlineSlots records which slots of secrets were sent inline.
func writeInlineSlots(ctx context.Context, p privateSetter, secrets map[string]any) diag.Diagnostics {
	slots := collectInlineSlots(secrets)
	list := make([][]string, 0, len(slots))
	for key := range slots {
		var slot []string
		_ = json.Unmarshal([]byte(key), &slot)
		list = append(list, slot)
	}
	sort.Slice(list, func(i, j int) bool { return slotKey(list[i]) < slotKey(list[j]) })
	b, err := json.Marshal(list)
	if err != nil {
		var diags diag.Diagnostics
		diags.AddError("Failed to record inline secret slots", err.Error())
		return diags
	}
	return p.SetKey(ctx, inlineSecretSlotsKey, b)
}
