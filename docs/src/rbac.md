# Roles and permissions

Sonar uses four roles. Higher roles include the abilities of lower ones for coarse checks.

| Role | What it means |
|------|----------------|
| **readonly** | View dashboards and inventory. No configuration changes. |
| **tech** | Read everything; acknowledge operational work that techs are allowed to do (for example on-demand style actions where exposed). |
| **siteadmin** | Manage agents, appliances, collectors, alarms, checks, credentials, discovery, SMTP/webhooks for sites. |
| **superadmin** | Full platform control: users, sites, retention, global discovery, audit log. |

Exact wording in the Users UI:

- **superadmin** — Full control. Manage users, sites, and the platform.
- **siteadmin** — Manage agents, appliances, and alerts within sites.
- **tech** — Read everything; ack alerts; run on-demand checks.
- **readonly** — Read-only dashboards.

## What you typically need

| Task | Minimum role |
|------|----------------|
| View dashboard, devices, appliances, checks | readonly |
| Create / edit / delete [checks](checks.md) | siteadmin |
| Enroll collectors / agents, edit discovery | siteadmin |
| Create sites, manage users, retention | superadmin |
| Create API keys for yourself | any signed-in user (see [API keys](api-keys.md)) |

Retention fields (hot window, rollups, flow/vendor samples, cleared alarms, audit roll-off) are documented in [Data storage and retention](data-retention.md).

API keys can also call the API; keys may be limited to specific sites. Prefer the least privilege that still does the job.

## Sidebar visibility

Admin links (**Settings**, **Discovery**, **Audit**, **Users**) only appear when your role is allowed to use them. Missing a link usually means your role cannot open that page—not that the feature is broken.
