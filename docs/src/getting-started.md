# Getting started

If Sonar is not running yet, start with [Installation](installation.md) (central stack, then probe/collector enroll). This page assumes you already have a URL and an account.

## Sign in

1. Open your Sonar URL (for example `https://sonar.example.com`).
2. Enter your email and password.
3. If your account has MFA enabled, enter the one-time code from your authenticator app when prompted.
4. After a successful login you land on the **Dashboard**.

Sessions use short-lived access tokens plus a longer-lived refresh token stored in the browser. If you sit idle and then get an authentication error, sign in again.

## UI tour

| Area | Purpose |
|------|---------|
| Left sidebar | Jump between product areas. Some admin links only appear for higher roles. |
| Top of sidebar | Product name and live version from the API. |
| User footer | Your display name, role, theme toggle, and **Sign out**. |
| Main pane | The page for the selected area. |
| **Checks** | Synthetic ICMP/TCP/HTTP/DNS/TLS monitors — see [Checks](checks.md). |

On a phone or narrow window, use the menu button to open the sidebar.

## Sites first

Almost everything is scoped to a **site** (a building, campus, or logical location). Create sites before enrolling devices or collectors. Superadmins create and edit sites; other roles work inside sites they can see.

## Recommended first-week path

1. Finish [Installation](installation.md) if this is a new deployment.
2. Create at least one [site](sites.md).
3. Enroll a [collector](collectors.md) at that site if Sonar cannot SNMP-poll gear itself.
4. Configure [site discovery](site-discovery.md) credentials (SNMP and/or Meraki).
5. Enroll [devices](devices.md) (probes) where you need endpoint visibility.
6. Confirm inventory on **Appliances** and **Devices**, then set [alarms](alarms.md).
7. Add [checks](checks.md) for services you care about (website up, DNS, TLS expiry, LAN ping from a site probe).
