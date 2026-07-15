# ScanRay Sonar operator guide

Welcome. This guide explains how to use ScanRay Sonar day to day: what each area of the product does, how to complete common tasks, and what the important settings mean.

## What Sonar is

ScanRay Sonar is the central console for:

- **Devices (agents)** — endpoints running the Sonar probe that report health, network path, and experience metrics
- **Appliances** — network gear polled by SNMP or enriched from Meraki Dashboard
- **Collectors** — site-side daemons that reach gear Sonar cannot poll directly
- **Checks** — synthetic ICMP/TCP/HTTP/DNS/TLS monitors (agent-first or central)
- **Alarms, reports, topology, and traffic** — operational views over that inventory

You must be signed in to open this documentation. The **Documentation** item in the sidebar issues a short session and opens this guide.

## How to use this guide

| If you want to… | Start here |
|-----------------|------------|
| Sign in, understand roles, find your way around | [Getting started](getting-started.md) |
| Know what your role can change | [Roles and permissions](rbac.md) |
| Enroll a probe or collector | [Devices](devices.md), [Collectors](collectors.md) |
| Add or tune network gear | [Appliances](appliances.md), [Site discovery](site-discovery.md) |
| Add ICMP/HTTP/DNS/TLS checks | [Checks](checks.md) |
| Configure SMTP, webhooks | [Settings](settings.md) |
| Understand hot/trend storage and roll-off | [Data storage and retention](data-retention.md) |

!!! tip "Documents vs Documentation"
    **Documents** in the main nav is for uploading site files (runbooks, diagrams). **Documentation** (this guide) is the product manual.
