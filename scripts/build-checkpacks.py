#!/usr/bin/env python3
"""Build Sonar-native check packs from a local full-sensor catalog JSON.

Reads prtg_full_sensor_catalog/prtg_full_sensor_catalog.json (gitignored)
and emits phase-1 packs under internal/checks/catalog/. Runtime code never
references third-party product names — only Sonar type IDs.

Usage (from repo root):
  python scripts/build-checkpacks.py
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SRC = ROOT / "prtg_full_sensor_catalog" / "prtg_full_sensor_catalog.json"
OUT = ROOT / "internal" / "checks" / "catalog"

# Catalog sensor id → Sonar check type (phase 1 only).
PHASE1 = {
    "ping": "icmp",
    "ping_v2": "icmp",
    "http": "http",
    "http_advanced": "http",
    "http_v2": "http",
    "ssl_certificate": "tls",
    "ssl_certificate_v2": "tls",
    "dns": "dns",
    "dns_v2": "dns",
    "port": "tcp",
    "port_range": "tcp",
}

# Phase 4 (central-only, vault credentials). Source ids are informational only.
PHASE4 = {
    "postgresql": "sql_query",
    "mysql_v2": "sql_query",
    "microsoft_sql_v2": "sql_query",
    "ado_sql_v2": "sql_query",
    "smtp": "smtp",
    "imap": "imap",
    "ldap": "ldap_bind",
}

LIMIT_RE = re.compile(
    r"(Upper|Lower)\s+(error|warning)\s+limit:\s*([0-9.]+)",
    re.I,
)


def slug(name: str) -> str:
    s = name.lower().strip()
    s = re.sub(r"[^a-z0-9]+", "_", s)
    return s.strip("_") or "channel"


def parse_alarm(desc: str) -> dict | None:
    if not desc:
        return None
    # Prefer error limits; fall back to warning.
    err = None
    warn = None
    for m in LIMIT_RE.finditer(desc):
        side, sev, val = m.group(1).lower(), m.group(2).lower(), float(m.group(3))
        op = ">" if side == "upper" else "<"
        entry = {"op": op, "value": val, "severity": "critical" if sev == "error" else "warning"}
        if sev == "error":
            err = entry
        else:
            warn = entry
    return err or warn


def params_for(type_id: str) -> list[dict]:
    common = [
        {"name": "host", "type": "string", "required": True, "label": "Target host"},
        {"name": "timeoutSec", "type": "number", "required": False, "default": 5, "label": "Timeout (s)"},
    ]
    if type_id == "icmp":
        return common + [
            {"name": "count", "type": "number", "required": False, "default": 4, "label": "Echo count"},
        ]
    if type_id == "tcp":
        return common + [
            {"name": "port", "type": "number", "required": True, "label": "TCP port"},
        ]
    if type_id == "http":
        return [
            {"name": "url", "type": "string", "required": True, "label": "URL"},
            {"name": "expectCode", "type": "number", "required": False, "default": 200, "label": "Expected status"},
            {"name": "timeoutSec", "type": "number", "required": False, "default": 10, "label": "Timeout (s)"},
        ]
    if type_id == "dns":
        return common + [
            {"name": "recordType", "type": "string", "required": False, "default": "A", "label": "Record type"},
            {"name": "resolver", "type": "string", "required": False, "label": "Resolver IP (optional)"},
        ]
    if type_id == "tls":
        return common + [
            {"name": "port", "type": "number", "required": False, "default": 443, "label": "TLS port"},
            {"name": "sni", "type": "string", "required": False, "label": "SNI / server name"},
        ]
    if type_id == "sql_query":
        return [
            {"name": "host", "type": "string", "required": True, "label": "Database host"},
            {"name": "port", "type": "number", "required": False, "default": 5432, "label": "Port"},
            {"name": "driver", "type": "string", "required": False, "default": "postgres", "label": "Driver (postgres|sqlserver|mysql)"},
            {"name": "query", "type": "string", "required": False, "default": "SELECT 1", "label": "Read-only SELECT"},
            {"name": "expectMinRows", "type": "number", "required": False, "default": 1, "label": "Minimum rows"},
            {"name": "timeoutSec", "type": "number", "required": False, "default": 15, "label": "Timeout (s)"},
            {"name": "credentialId", "type": "string", "required": True, "label": "Vault credential (sql)"},
        ]
    if type_id == "smtp":
        return [
            {"name": "host", "type": "string", "required": True, "label": "SMTP host"},
            {"name": "port", "type": "number", "required": False, "default": 587, "label": "Port"},
            {"name": "tlsMode", "type": "string", "required": False, "default": "starttls", "label": "TLS mode (starttls|tls|plain)"},
            {"name": "from", "type": "string", "required": False, "label": "MAIL FROM (optional)"},
            {"name": "to", "type": "string", "required": False, "label": "RCPT TO probe (optional)"},
            {"name": "timeoutSec", "type": "number", "required": False, "default": 15, "label": "Timeout (s)"},
            {"name": "credentialId", "type": "string", "required": True, "label": "Vault credential (smtp)"},
        ]
    if type_id == "imap":
        return [
            {"name": "host", "type": "string", "required": True, "label": "IMAP host"},
            {"name": "port", "type": "number", "required": False, "default": 993, "label": "Port"},
            {"name": "tlsMode", "type": "string", "required": False, "default": "tls", "label": "TLS mode (tls|starttls|plain)"},
            {"name": "timeoutSec", "type": "number", "required": False, "default": 15, "label": "Timeout (s)"},
            {"name": "credentialId", "type": "string", "required": True, "label": "Vault credential (imap)"},
        ]
    if type_id == "ldap_bind":
        return [
            {"name": "host", "type": "string", "required": True, "label": "LDAP host"},
            {"name": "port", "type": "number", "required": False, "default": 389, "label": "Port"},
            {"name": "useTLS", "type": "boolean", "required": False, "default": False, "label": "Use LDAPS"},
            {"name": "bindDN", "type": "string", "required": False, "label": "Bind DN override (optional)"},
            {"name": "timeoutSec", "type": "number", "required": False, "default": 15, "label": "Timeout (s)"},
            {"name": "credentialId", "type": "string", "required": True, "label": "Vault credential (ldap)"},
        ]
    return common


def channels_for(type_id: str, catalog_channels: list[dict]) -> list[dict]:
    """Sonar runner channel keys (stable). Catalog text only seeds defaultAlarm when labels match."""
    defaults = {
        "icmp": [
            {"key": "response_time_ms", "label": "Response Time", "valueKind": "gauge",
             "defaultAlarm": {"op": ">", "value": 500, "severity": "warning", "name": "ICMP latency high", "flatField": "icmp_response_time_ms"}},
            {"key": "packet_loss_pct", "label": "Packet Loss %", "valueKind": "gauge",
             "defaultAlarm": {"op": ">", "value": 0, "severity": "warning", "name": "ICMP packet loss", "flatField": "icmp_packet_loss_pct"}},
            {"key": "up", "label": "Up", "valueKind": "gauge"},
        ],
        "tcp": [
            {"key": "response_time_ms", "label": "Available", "valueKind": "gauge"},
            {"key": "up", "label": "Up", "valueKind": "gauge",
             "defaultAlarm": {"op": "!=", "value": 1, "severity": "critical", "name": "TCP port down", "flatField": "tcp_up"}},
        ],
        "http": [
            {"key": "response_time_ms", "label": "Response Time", "valueKind": "gauge",
             "defaultAlarm": {"op": ">", "value": 5000, "severity": "warning", "name": "HTTP latency high", "flatField": "http_response_time_ms"}},
            {"key": "status_code", "label": "HTTP Status", "valueKind": "gauge"},
            {"key": "up", "label": "Up", "valueKind": "gauge",
             "defaultAlarm": {"op": "!=", "value": 1, "severity": "critical", "name": "HTTP check failed", "flatField": "http_up"}},
        ],
        "dns": [
            {"key": "response_time_ms", "label": "Response Time", "valueKind": "gauge"},
            {"key": "record_count", "label": "Record Count", "valueKind": "gauge"},
            {"key": "up", "label": "Records Resolved", "valueKind": "gauge",
             "defaultAlarm": {"op": "!=", "value": 1, "severity": "critical", "name": "DNS resolve failed", "flatField": "dns_up"}},
        ],
        "tls": [
            {"key": "days_to_expiration", "label": "Days to Expiration", "valueKind": "gauge",
             "defaultAlarm": {"op": "<", "value": 28, "severity": "warning", "name": "TLS cert expiring soon", "flatField": "tls_days_to_expiration"}},
            {"key": "cn_match", "label": "Common Name Check", "valueKind": "gauge"},
            {"key": "up", "label": "Up", "valueKind": "gauge",
             "defaultAlarm": {"op": "!=", "value": 1, "severity": "critical", "name": "TLS handshake failed", "flatField": "tls_up"}},
        ],
        "sql_query": [
            {"key": "response_time_ms", "label": "Response Time", "valueKind": "gauge",
             "defaultAlarm": {"op": ">", "value": 5000, "severity": "warning", "name": "SQL query latency high", "flatField": "sql_response_time_ms"}},
            {"key": "row_count", "label": "Row Count", "valueKind": "gauge"},
            {"key": "up", "label": "Up", "valueKind": "gauge",
             "defaultAlarm": {"op": "!=", "value": 1, "severity": "critical", "name": "SQL check failed", "flatField": "sql_up"}},
        ],
        "smtp": [
            {"key": "response_time_ms", "label": "Response Time", "valueKind": "gauge"},
            {"key": "up", "label": "Up", "valueKind": "gauge",
             "defaultAlarm": {"op": "!=", "value": 1, "severity": "critical", "name": "SMTP check failed", "flatField": "smtp_up"}},
        ],
        "imap": [
            {"key": "response_time_ms", "label": "Response Time", "valueKind": "gauge"},
            {"key": "mailbox_count", "label": "Mailbox Count", "valueKind": "gauge"},
            {"key": "up", "label": "Up", "valueKind": "gauge",
             "defaultAlarm": {"op": "!=", "value": 1, "severity": "critical", "name": "IMAP check failed", "flatField": "imap_up"}},
        ],
        "ldap_bind": [
            {"key": "response_time_ms", "label": "Response Time", "valueKind": "gauge"},
            {"key": "up", "label": "Up", "valueKind": "gauge",
             "defaultAlarm": {"op": "!=", "value": 1, "severity": "critical", "name": "LDAP bind failed", "flatField": "ldap_up"}},
        ],
    }
    # Prefer Sonar runner schema; optionally overlay limits from catalog descriptions.
    out = [dict(c) for c in defaults.get(type_id, [])]
    for c in catalog_channels:
        name = (c.get("name") or "").lower()
        alarm = parse_alarm(c.get("description") or "")
        if not alarm:
            continue
        for ch in out:
            label = ch["label"].lower()
            if ("ping time" in name or "response time" in name) and ch["key"] == "response_time_ms":
                ch["defaultAlarm"] = {**alarm, "name": ch.get("defaultAlarm", {}).get("name", type_id), "flatField": ch.get("defaultAlarm", {}).get("flatField", f"{type_id}_{ch['key']}")}
            if "packet loss" in name and ch["key"] == "packet_loss_pct":
                ch["defaultAlarm"] = {**alarm, "name": "ICMP packet loss", "flatField": "icmp_packet_loss_pct"}
            if "days to expiration" in name and ch["key"] == "days_to_expiration":
                ch["defaultAlarm"] = {**alarm, "name": "TLS cert expiring soon", "flatField": "tls_days_to_expiration"}
            if label in name or name in label:
                pass
    return out


def titles() -> dict[str, str]:
    return {
        "icmp": "ICMP ping",
        "tcp": "TCP port",
        "http": "HTTP / HTTPS",
        "dns": "DNS query",
        "tls": "TLS certificate",
        "sql_query": "SQL query",
        "smtp": "SMTP",
        "imap": "IMAP",
        "ldap_bind": "LDAP bind",
    }


def mechanism_for(type_id: str) -> str:
    return {
        "sql_query": "sql",
        "smtp": "mail",
        "imap": "mail",
        "ldap_bind": "ldap",
    }.get(type_id, type_id)


def runner_for(type_id: str) -> str:
    if type_id in ("sql_query", "smtp", "imap", "ldap_bind"):
        return "central"
    return "either"


def main() -> int:
    packs: dict[str, dict] = {}
    titles_map = titles()
    source_ids: dict[str, list[str]] = {t: [] for t in titles_map}

    if SRC.is_file():
        data = json.loads(SRC.read_text(encoding="utf-8"))
        sensors = data.get("sensors") or []
        for s in sensors:
            sid = s.get("id") or ""
            type_id = PHASE1.get(sid) or PHASE4.get(sid)
            if not type_id:
                continue
            source_ids.setdefault(type_id, []).append(sid)
            if type_id in packs:
                continue
            packs[type_id] = {
                "id": type_id,
                "title": titles_map.get(type_id, type_id),
                "mechanism": mechanism_for(type_id),
                "runner": runner_for(type_id),
                "params": params_for(type_id),
                "channels": channels_for(type_id, s.get("channels") or []),
                "sourceIds": [sid],
            }
        print(f"loaded {len(sensors)} catalog sensors → {len(packs)} packs", file=sys.stderr)
    else:
        print(f"catalog missing at {SRC}; writing built-in phase-1+4 packs", file=sys.stderr)

    for type_id, title in titles_map.items():
        if type_id in packs:
            packs[type_id]["sourceIds"] = source_ids.get(type_id) or packs[type_id].get("sourceIds") or []
            continue
        packs[type_id] = {
            "id": type_id,
            "title": title,
            "mechanism": mechanism_for(type_id),
            "runner": runner_for(type_id),
            "params": params_for(type_id),
            "channels": channels_for(type_id, []),
            "sourceIds": source_ids.get(type_id) or [],
        }

    OUT.mkdir(parents=True, exist_ok=True)
    index = {"packs": []}
    for type_id in sorted(packs):
        p = packs[type_id]
        path = OUT / f"{type_id}.json"
        path.write_text(json.dumps(p, indent=2) + "\n", encoding="utf-8")
        index["packs"].append({"id": type_id, "title": p["title"], "file": path.name, "channelCount": len(p["channels"])})
        print(f"  wrote {path.relative_to(ROOT)} ({len(p['channels'])} channels)")

    (OUT / "index.json").write_text(json.dumps(index, indent=2) + "\n", encoding="utf-8")
    print(f"wrote {OUT / 'index.json'}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
