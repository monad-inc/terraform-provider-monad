package provider

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	monad "github.com/monad-inc/sdk/go"
	"github.com/monad-inc/terraform-provider-monad/internal/provider/client"
)

// permissionsPageSize is the page size used to read the permission catalog.
// The catalog is small (51 entries on 2026-10-22), so this is one request.
const permissionsPageSize = 200

// listAllPermissions reads the organization's full permission catalog.
func listAllPermissions(ctx context.Context, c *client.Client) ([]monad.ModelsPermission, error) {
	var out []monad.ModelsPermission
	for offset := int32(0); ; offset += permissionsPageSize {
		page, httpResp, err := c.PermissionsAPI.ListPermissions(ctx, c.OrganizationID).
			Limit(permissionsPageSize).Offset(offset).Execute()
		if err != nil {
			return nil, fmt.Errorf("listing permissions: %s. Response: %s", err, getResponseBody(httpResp))
		}
		out = append(out, page.Permissions...)
		total, ok := page.Pagination.GetTotalOk()
		if len(page.Permissions) == 0 || (ok && int32(len(out)) >= *total) {
			return out, nil
		}
	}
}

// resolvePermissionSlugs maps permission slugs (`pipeline:read`) to the IDs the
// role endpoints take. Every unknown slug is reported at once, with the valid
// ones, so a typo is fixed in one pass.
func resolvePermissionSlugs(catalog []monad.ModelsPermission, slugs []string) ([]string, error) {
	bySlug := make(map[string]string, len(catalog))
	for _, p := range catalog {
		bySlug[p.GetSlug()] = p.GetId()
	}

	ids := make([]string, 0, len(slugs))
	var unknown []string
	for _, s := range slugs {
		id, ok := bySlug[s]
		if !ok {
			unknown = append(unknown, s)
			continue
		}
		ids = append(ids, id)
	}
	if len(unknown) > 0 {
		valid := make([]string, 0, len(bySlug))
		for s := range bySlug {
			valid = append(valid, s)
		}
		sort.Strings(valid)
		sort.Strings(unknown)
		return nil, fmt.Errorf(
			"unknown permission %s: %s. Valid permissions: %s",
			pluralize("slug", len(unknown)), strings.Join(unknown, ", "), strings.Join(valid, ", "),
		)
	}
	return ids, nil
}

// permissionSlugs returns the slugs of a role's permissions.
func permissionSlugs(perms []monad.ModelsPermission) []string {
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		out = append(out, p.GetSlug())
	}
	return out
}

func pluralize(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// listRolesForAdopt is the adoptLister for roles.
func listRolesForAdopt(c *client.Client) adoptLister {
	return func(ctx context.Context, limit, offset int32) ([]adoptCandidate, *int32, *http.Response, error) {
		page, httpResp, err := c.RolesAPI.ListRoles(ctx, c.OrganizationID).
			Limit(limit).Offset(offset).Execute()
		if err != nil || page == nil {
			return nil, nil, httpResp, err
		}
		out := make([]adoptCandidate, 0, len(page.Roles))
		for _, r := range page.Roles {
			out = append(out, adoptCandidate{ID: r.Id, Name: r.Name, CreatedAt: r.CreatedAt})
		}
		total, _ := page.Pagination.GetTotalOk()
		return out, total, httpResp, nil
	}
}
