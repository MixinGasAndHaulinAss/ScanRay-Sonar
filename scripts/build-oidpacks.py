#!/usr/bin/env python3
"""Build Sonar-native SNMP OID packs from a local oid_bundle analysis CSV.

Reads oid_bundle/analysis/all_oid_entries.csv (and optional lookups/) and
emits internal/snmp/oidpacks/*.json + enums/ + alarms.json.

The source extract is reference-only and must not be committed. Runtime
pack IDs and metric keys are Sonar-native (no third-party product names).
"""

from __future__ import annotations

import csv
import json
import re
import xml.etree.ElementTree as ET
from collections import defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CSV_PATH = ROOT / "oid_bundle" / "analysis" / "all_oid_entries.csv"
LOOKUPS = ROOT / "oid_bundle" / "lookups"
OUT = ROOT / "internal" / "snmp" / "oidpack" / "data"
ENUMS = OUT / "enums"

# OIDs already covered by CollectAll IF-MIB / typed CollectUPS / CollectCiscoChassis.
SKIP_OID_PREFIXES = (
    "1.3.6.1.2.1.2.",  # ifTable
    "1.3.6.1.2.1.31.1.1.",  # ifXTable
)
SKIP_OIDS = {
    # Typed UPS (APC / RFC1628 overlap used by vendor_ups.go)
    "1.3.6.1.4.1.318.1.1.1.1.1.1.0",
    "1.3.6.1.4.1.318.1.1.1.1.2.2.0",
    "1.3.6.1.4.1.318.1.1.1.1.2.3.0",
    "1.3.6.1.4.1.318.1.1.1.2.1.3.0",
    "1.3.6.1.4.1.318.1.1.1.2.2.4.0",
    "1.3.6.1.4.1.318.1.1.1.3.2.5.0",
    "1.3.6.1.4.1.318.1.1.1.4.1.1.0",
    "1.3.6.1.4.1.318.1.1.1.4.2.1.0",
    "1.3.6.1.4.1.318.1.1.1.4.2.3.0",
    "1.3.6.1.2.1.33.1.2.1.0",
    "1.3.6.1.2.1.33.1.2.3.0",
    "1.3.6.1.2.1.33.1.2.4.0",
    "1.3.6.1.2.1.33.1.2.7.0",
    "1.3.6.1.2.1.33.1.3.3.1.3",
    "1.3.6.1.2.1.33.1.4.4.1.5",
    # Cisco chassis CPU already in vendor_cisco
    "1.3.6.1.4.1.9.2.1.56",
    "1.3.6.1.4.1.9.2.1.57",
    "1.3.6.1.4.1.9.2.1.58",
    "1.3.6.1.4.1.9.9.109.1.1.1.1.6",
    "1.3.6.1.4.1.9.9.109.1.1.1.1.7",
    "1.3.6.1.4.1.9.9.109.1.1.1.1.8",
}

# library filename -> pack metadata
LIBRARY_PACKS: dict[str, dict] = {
    "APC UPS.oidlib": {
        "id": "apc_ups",
        "title": "APC UPS PowerNet supplements",
        "enterprises": ["318"],
        "vendorAliases": ["ups", "ups-apc", "apc", "ups-generic"],
        "sysObjectPrefixes": ["1.3.6.1.4.1.318.1.1.1"],
    },
    "APCSensorstationlib.oidlib": {
        "id": "apc_env",
        "title": "APC environmental probes",
        "enterprises": ["318"],
        "vendorAliases": ["apc-env", "apc"],
        "sysObjectPrefixes": ["1.3.6.1.4.1.318.1.1.10"],
    },
    "Basic Linux Library (UCD-SNMP-MIB).oidlib": {
        "id": "linux_host",
        "title": "Linux/Unix host (UCD-SNMP)",
        "enterprises": ["2021"],
        "vendorAliases": ["linux", "unix"],
        "sysObjectPrefixes": ["1.3.6.1.4.1.8072", "1.3.6.1.4.1.2021"],
    },
    "Dell Storage Management.oidlib": {
        "id": "dell_storage",
        "title": "Dell storage management",
        "enterprises": ["674"],
        "vendorAliases": ["dell-storage", "dell"],
        "sysObjectPrefixes": ["1.3.6.1.4.1.674.10893"],
    },
    "Dell Systems Management Instrumentation.oidlib": {
        "id": "dell_server",
        "title": "Dell server management",
        "enterprises": ["674"],
        "vendorAliases": ["dell", "dell-server"],
        "sysObjectPrefixes": ["1.3.6.1.4.1.674.10892"],
    },
    "HP Laserjet Status.oidlib": {
        "id": "printer_hp",
        "title": "HP LaserJet status",
        "enterprises": ["11"],
        "vendorAliases": ["printer", "printer-hp", "hp"],
        "sysObjectPrefixes": ["1.3.6.1.4.1.11.2.3.9"],
    },
    "SNMP Informant Std.oidlib": {
        "id": "windows_informant",
        "title": "Windows host (SNMP Informant)",
        "enterprises": ["9600"],
        "vendorAliases": ["windows", "windows-informant"],
        "sysObjectPrefixes": ["1.3.6.1.4.1.9600"],
    },
    "cisco-queue.oidlib": {
        "id": "cisco_queue",
        "title": "Cisco interface queues",
        "enterprises": ["9"],
        "vendorAliases": ["cisco"],
        "sysObjectPrefixes": ["1.3.6.1.4.1.9.9.37"],
    },
    "cisco-interfaces.oidlib": {
        "id": "cisco_if_extras",
        "title": "Cisco interface extras",
        "enterprises": ["9"],
        "vendorAliases": ["cisco"],
        "sysObjectPrefixes": ["1.3.6.1.4.1.9"],
    },
}

# Multi-library → pack by OID enterprise / name heuristics
LINUX_NET_META = {
    "id": "linux_net",
    "title": "Linux/Unix network MIB supplements",
    "enterprises": ["2021", "8072"],
    "vendorAliases": ["linux", "unix"],
    "sysObjectPrefixes": ["1.3.6.1.4.1.8072", "1.3.6.1.4.1.2021"],
}
HOST_RESOURCES_META = {
    "id": "host_resources",
    "title": "HOST-RESOURCES-MIB",
    "enterprises": [],
    "vendorAliases": ["linux", "unix", "windows", "generic", "printer"],
    "sysObjectPrefixes": [],
}
CISCO_ENV_META = {
    "id": "cisco_env",
    "title": "Cisco environment and memory extras",
    "enterprises": ["9"],
    "vendorAliases": ["cisco"],
    "sysObjectPrefixes": ["1.3.6.1.4.1.9"],
}
WINDOWS_IIS_META = {
    "id": "windows_iis",
    "title": "Windows IIS / SMTP via SNMP",
    "enterprises": ["311"],
    "vendorAliases": ["windows", "windows-iis"],
    "sysObjectPrefixes": ["1.3.6.1.4.1.311"],
}
HP_PROCURVE_META = {
    "id": "hp_procurve",
    "title": "HP Procurve switch CPU/memory",
    "enterprises": ["11"],
    "vendorAliases": ["hp-procurve", "hp", "aruba"],
    "sysObjectPrefixes": ["1.3.6.1.4.1.11.2.14"],
}
PRINTER_GENERIC_META = {
    "id": "printer_generic",
    "title": "Generic network printer (Printer-MIB)",
    "enterprises": [],
    "vendorAliases": ["printer", "printer-hp", "hp"],
    "sysObjectPrefixes": ["1.3.6.1.2.1.43"],
}


def sanitize_key(parts: list[str]) -> str:
    out = []
    for p in parts:
        p = p.strip().lower()
        p = re.sub(r"[^a-z0-9]+", "_", p)
        p = p.strip("_")
        if p:
            out.append(p)
    return ".".join(out)[:180] if out else "metric"


def metric_key(pack_id: str, name: str, indicator: str, oid: str) -> str:
    # Prefer indicator; fall back to last pipe segment of name.
    label = indicator or name.split("|")[-1]
    label = re.sub(r"#\[[^\]]+\]", "", label)
    label = re.sub(r":\s*$", "", label)
    key = sanitize_key([pack_id.replace("_", "."), label])
    # Ensure uniqueness with short oid suffix if needed later by caller.
    return key


def should_skip(oid: str) -> bool:
    oid = oid.lstrip(".")
    if oid in SKIP_OIDS:
        return True
    return any(oid.startswith(p) for p in SKIP_OID_PREFIXES)


def mode_for(kind: str) -> str:
    if kind == "mkTable":
        return "walk"
    return "get"


def value_kind(typ: str) -> str:
    if typ == "vmDiff":
        return "counter"
    return "gauge"


def scale_of(row: dict) -> float:
    try:
        s = float(row.get("scale") or 1)
    except ValueError:
        s = 1.0
    if (row.get("multiply") or "").lower() == "yes":
        return s if s else 1.0
    # Many libraries store "scale" as divisor (e.g. 10 → value/10).
    if s and s != 1:
        return 1.0 / s
    return 1.0


def route_row(library: str, oid: str, name: str) -> tuple[str, dict] | None:
    oid = oid.lstrip(".")
    if library in LIBRARY_PACKS:
        # Sensorstation rows under APC UPS library → apc_env
        if library == "APC UPS.oidlib" and oid.startswith("1.3.6.1.4.1.318.1.1.10"):
            return "apc_env", LIBRARY_PACKS["APCSensorstationlib.oidlib"]
        return LIBRARY_PACKS[library]["id"], LIBRARY_PACKS[library]

    # Linux SNMP * libraries
    if library.startswith("Linux SNMP"):
        if oid.startswith("1.3.6.1.4.1.2021"):
            return "linux_host", LIBRARY_PACKS["Basic Linux Library (UCD-SNMP-MIB).oidlib"]
        if should_skip(oid):
            return None
        return "linux_net", LINUX_NET_META

    if library.startswith("Paessler Common") or "Common OID" in library:
        if oid.startswith("1.3.6.1.2.1.25"):
            return "host_resources", HOST_RESOURCES_META
        if oid.startswith("1.3.6.1.4.1.9"):
            return "cisco_env", CISCO_ENV_META
        if oid.startswith("1.3.6.1.4.1.311"):
            return "windows_iis", WINDOWS_IIS_META
        if oid.startswith("1.3.6.1.4.1.11.2.14"):
            return "hp_procurve", HP_PROCURVE_META
        if oid.startswith("1.3.6.1.2.1.43"):
            return "printer_generic", PRINTER_GENERIC_META
        # Sensorbox / NetWare — fold into host_resources-ish generic pack
        return "host_resources", HOST_RESOURCES_META

    return None


def suggest_alarm(pack_id: str, key: str, unit: str, name: str) -> dict | None:
    low = (key + " " + name).lower()
    # Printer states
    if "toner" in low and "status" in low:
        return {"op": ">", "value": 0, "severity": "warning", "name": "Printer toner not OK", "flatField": "printer_toner_status"}
    if "paper" in low and "status" in low:
        return {"op": ">", "value": 0, "severity": "warning", "name": "Printer paper not OK", "flatField": "printer_paper_status"}
    if "jam" in low and "status" in low:
        return {"op": ">", "value": 0, "severity": "critical", "name": "Printer jam", "flatField": "printer_jam_status"}
    if pack_id == "apc_env" and "alarm_status" in low:
        return {"op": "!=", "value": 1, "severity": "warning", "name": "APC env probe alarm", "flatField": "apc_env_alarm_status"}
    if pack_id == "dell_server" and ("overall" in low or "global_system_status" in low or "systemstatus" in low.replace("_", "")):
        return {"op": "!=", "value": 3, "severity": "warning", "name": "Dell server health not OK", "flatField": "dell_server_overall_status"}
    if "load" in low and ("1min" in low or "load_1" in low or "loadaverage" in low.replace("_", "")):
        return {"op": ">", "value": 5, "severity": "warning", "name": "Host load average high", "flatField": "linux_load_1min"}
    if "disk" in low and ("free" in low or "percent" in low) and unit in ("%",):
        return {"op": "<", "value": 10, "severity": "warning", "name": "Disk free space low", "flatField": "disk_free_pct"}
    return None


def parse_ovl(path: Path) -> dict[str, str]:
    tree = ET.parse(path)
    root = tree.getroot()
    mapping: dict[str, str] = {}
    for node in root.findall(".//SingleInt"):
        val = node.get("value")
        text = (node.text or "").strip()
        if val is not None:
            mapping[val] = text
    return mapping


def enum_for(name: str, indicator: str) -> str | None:
    low = (name + " " + indicator).lower()
    if "toner" in low and "status" in low:
        return "printer_toner_status"
    if "paper" in low and "status" in low:
        return "printer_paper_status"
    if "jam" in low and "status" in low:
        return "printer_jam_status"
    if "cover" in low and "state" in low:
        return "printer_cover_state"
    if "cartridge" in low and "level" in low:
        return "printer_cartridge_level"
    return None


def main() -> None:
    if not CSV_PATH.exists():
        raise SystemExit(f"missing {CSV_PATH}; place oid_bundle analysis locally first")

    packs: dict[str, dict] = {}
    seen_keys: dict[str, set[str]] = defaultdict(set)
    alarms: list[dict] = []

    with CSV_PATH.open(newline="", encoding="utf-8") as f:
        reader = csv.DictReader(f)
        for row in reader:
            library = row["library"]
            oid = (row["oid"] or "").lstrip(".")
            if not oid or should_skip(oid):
                continue
            routed = route_row(library, oid, row.get("name") or "")
            if not routed:
                continue
            pack_id, meta = routed
            if pack_id not in packs:
                packs[pack_id] = {
                    "id": meta["id"],
                    "title": meta["title"],
                    "enterprises": list(meta.get("enterprises") or []),
                    "vendorAliases": list(meta.get("vendorAliases") or []),
                    "sysObjectPrefixes": list(meta.get("sysObjectPrefixes") or []),
                    "metrics": [],
                }

            key = metric_key(pack_id, row.get("name") or "", row.get("indicator") or "", oid)
            if key in seen_keys[pack_id]:
                # Disambiguate with last OID arc
                key = f"{key}.{oid.split('.')[-1]}"
            seen_keys[pack_id].add(key)

            metric = {
                "key": key,
                "oid": oid,
                "mode": mode_for(row.get("kind") or "mkDirect"),
                "valueKind": value_kind(row.get("type") or "vmAbsolute"),
                "scale": scale_of(row),
                "unit": (row.get("units") or "").strip(),
                "label": (row.get("indicator") or row.get("name") or key).strip()[:200],
            }
            em = enum_for(row.get("name") or "", row.get("indicator") or "")
            if em:
                metric["enumMap"] = em
            alarm = suggest_alarm(pack_id, key, metric["unit"], row.get("name") or "")
            if alarm:
                metric["alarm"] = alarm
                flat = alarm["flatField"]
                alarms.append(
                    {
                        "name": alarm["name"],
                        "severity": alarm["severity"],
                        "expression": f"device.{flat} {alarm['op']} {alarm['value']}",
                        "flatField": flat,
                        "metricKey": key,
                    }
                )
            packs[pack_id]["metrics"].append(metric)

    OUT.mkdir(parents=True, exist_ok=True)
    ENUMS.mkdir(parents=True, exist_ok=True)

    # Enums from lookups (Sonar IDs only)
    enum_files = {
        "printer_toner_status": "oid.paessler.hplaserjet.tonerstatus.ovl",
        "printer_paper_status": "oid.paessler.hplaserjet.paperstatus.ovl",
        "printer_jam_status": "oid.paessler.hplaserjet.jamstatus.ovl",
        "printer_cover_state": "prtg.standardlookups.snmpprinter.coverstate.ovl",
        "printer_cartridge_level": "prtg.standardlookups.snmpprinter.cartridgelevel.ovl",
    }
    for enum_id, fname in enum_files.items():
        path = LOOKUPS / fname
        if path.exists():
            mapping = parse_ovl(path)
            (ENUMS / f"{enum_id}.json").write_text(
                json.dumps({"id": enum_id, "values": mapping}, indent=2) + "\n",
                encoding="utf-8",
            )

    index = {"packs": [], "generatedFrom": "oid_bundle/analysis/all_oid_entries.csv"}
    for pack_id in sorted(packs):
        pack = packs[pack_id]
        # Cap extremely large walk packs for linux_net noise (still include all metrics;
        # runtime caps row count). Deduplicate enterprises.
        pack["enterprises"] = sorted(set(pack["enterprises"]))
        path = OUT / f"{pack_id}.json"
        path.write_text(json.dumps(pack, indent=2) + "\n", encoding="utf-8")
        index["packs"].append(
            {
                "id": pack_id,
                "title": pack["title"],
                "file": f"{pack_id}.json",
                "metricCount": len(pack["metrics"]),
            }
        )
        print(f"wrote {path.name} ({len(pack['metrics'])} metrics)")

    (OUT / "index.json").write_text(json.dumps(index, indent=2) + "\n", encoding="utf-8")

    # Deduplicate alarms by flatField
    unique = {}
    for a in alarms:
        unique[a["flatField"]] = a
    (OUT / "alarms.json").write_text(
        json.dumps({"alarms": list(unique.values())}, indent=2) + "\n",
        encoding="utf-8",
    )
    print(f"index: {len(index['packs'])} packs, {sum(p['metricCount'] for p in index['packs'])} metrics")
    print(f"alarms: {len(unique)}")


if __name__ == "__main__":
    main()
