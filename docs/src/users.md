# Users

**Users** is a **superadmin** page for creating and managing console accounts.

## Create a user

1. Open **Users**.
2. Enter email, display name, temporary password, and [role](rbac.md).
3. Save and communicate credentials out of band (or require password change per your process).
4. Enable MFA with the user when your policy requires it.

## Edit or disable

Update role or display name as responsibilities change. Delete only when the account should lose all access; prefer role downgrade for temporary reductions.

## Roles reminder

- **superadmin** — Full control. Manage users, sites, and the platform.
- **siteadmin** — Manage agents, appliances, and alerts within sites.
- **tech** — Read everything; ack alerts; run on-demand checks.
- **readonly** — Read-only dashboards.
