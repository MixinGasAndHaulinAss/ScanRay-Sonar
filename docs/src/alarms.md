# Alarms

Alarms turn inventory and metric conditions into actionable events, with optional notification channels.

## Building blocks

| Piece | Purpose |
|-------|---------|
| **Alarm rules** | Conditions that open an alarm (what metric/device, thresholds, severity). |
| **Notification channels** | Where to send (for example email/webhook-style destinations managed in Sonar). |
| **Alarms list** | Live and recent open/acked/cleared events. |

Siteadmins create and edit rules and channels. Techs and above can work alarms according to role (ack/clear typically siteadmin+ in the API).

## Typical workflow

1. Define notification channels you actually monitor.
2. Create rules scoped to the right sites / device classes.
3. Watch **Alarms** for new opens.
4. **Ack** while investigating; **Clear** when resolved (or let auto-clear behavior apply if configured).

## Tips

- Start with a few high-signal rules (device offline, critical interface down) before fine thresholds.
- Synthetic [checks](checks.md) ship with default rules (HTTP/TCP/DNS down, TLS expiry, ICMP loss). Tune thresholds under alarm rules (`target_kind` = check).
- Pair with [Settings](settings.md) SMTP/webhooks so notifications leave Sonar.
- Use the audit log (superadmin) if you need to see who changed rules.
