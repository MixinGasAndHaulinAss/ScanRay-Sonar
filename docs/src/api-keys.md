# API keys

API keys let scripts and integrations call Sonar’s HTTP API without an interactive login.

Keys are shown once at creation—copy them immediately. Prefix is typically `scr_…`.

## Create a key

1. Open **API keys**.
2. Create a key with a descriptive name.
3. Optionally restrict the key to specific **sites**.
4. Store the secret in your secret manager; Sonar will not show it again.

## Use a key

Send `Authorization: Bearer scr_…` on `/api/v1/…` requests. Site-scoped keys cannot see other sites’ data.

## Rotate or revoke

Delete (revoke) a compromised or unused key, then create a replacement. Superadmins can list keys across the platform from the admin API keys view when exposed.

## OpenAPI

Machine-readable API docs live at `/api/v1/openapi.yaml` on your Sonar host. Prefer that for endpoint details; this guide covers operator tasks in the UI.
