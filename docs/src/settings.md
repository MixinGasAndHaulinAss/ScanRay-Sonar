# Settings

**Settings** (sidebar) covers outbound email, webhooks, and platform retention. SMTP and webhooks require **siteadmin**; retention requires **superadmin**.

## SMTP

Configure the outbound mail relay Sonar uses for alarm and test mail.

Typical fields: host, port, TLS mode, username/password, from-address.

Use **Test** after saving to send a probe message. If tests fail, check firewall egress, credentials, and that the from-address is allowed by your relay.

## Webhooks

Signed outbound webhooks notify external systems when events fire.

1. Add a webhook with URL and secret as prompted.
2. Enable it.
3. Use **Test** to verify delivery.
4. Patch or delete when rotating endpoints.

Keep webhook URLs on HTTPS and treat secrets like passwords.

## Data retention (superadmin)

Retention controls how long hot samples, rollups, flows, vendor samples, cleared alarms, and audit rows are kept. Typical knobs include:

| Setting | Meaning |
|---------|---------|
| Hot window (days) | High-resolution samples kept before rollup/compress |
| Compress after (days) | When older samples are compressed |
| Rollup retention (days) | How long rolled-up trends remain |
| Flow hot window (days) | Flow detail retention |
| Vendor samples (days) | Meraki/vendor sample retention |
| Cleared alarms (days) | How long cleared alarm rows remain |
| Audit log (days) | Security audit retention |

Saving retention updates the platform policy the retention worker applies. Shortening windows frees storage but removes history—coordinate before aggressive cuts.
