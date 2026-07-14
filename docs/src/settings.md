# Settings

**Settings** (sidebar) covers outbound email, webhooks, and platform data retention.

| Area | Who can change it |
|------|-------------------|
| SMTP | **siteadmin** (and above) |
| Webhooks | **siteadmin** (and above) |
| Data retention / roll-off | **superadmin** only |

## Data storage and retention (superadmin)

Telemetry lives in TimescaleDB and ages from **hot** (raw) → **compressed** → **hourly trends** → **gone**. Cleared alarms and audit rows roll off on separate day counts.

The full operator guide—including every field, defaults, ranges, what data maps to which knob, capacity sizing, and how charts switch to rollups—is on **[Data storage and retention](data-retention.md)**.

Open **Settings → Data retention**, edit the day counts, then **Save retention** to apply policies.

## SMTP

Configure the outbound mail relay Sonar uses for alarm and test mail.

Typical fields: host, port, TLS mode, username/password, from-address.

Use **Test** after saving to send a probe message. If tests fail, check firewall egress, credentials, and that the from-address is allowed by your relay.

Leave password blank when saving if a password is already stored and you do not want to rotate it.

## Webhooks

Signed outbound webhooks notify external systems when events fire.

1. Add a webhook with URL and secret as prompted.
2. Enable it.
3. Use **Test** to verify delivery.
4. Patch or delete when rotating endpoints.

Keep webhook URLs on HTTPS and treat secrets like passwords.
