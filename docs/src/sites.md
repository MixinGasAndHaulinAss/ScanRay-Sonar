# Sites

A **site** groups devices, appliances, collectors, credentials, and discovery settings for one location or tenant boundary.

## Create or edit a site

Requires **superadmin**.

1. Open **Sites**.
2. Create a site with a clear name (building or campus).
3. Edit later if the name or metadata changes.

Other roles see the sites they are allowed to work in and use them when enrolling agents/collectors or opening site discovery.

## Site network map

From a site, open the **map** view (`/sites/:siteId/map`) for a topology-style picture of agents and appliances at that site. Use it to confirm what is enrolled and how pieces relate, not as a CAD floor plan.

## Site discovery

Each site has its own discovery and credential settings. Open **Discovery** from the site (or navigate to `/sites/:siteId/discovery`) as a **siteadmin**. See [Site discovery](site-discovery.md).

## When to add another site

Add a site when:

- The location has its own collector or SNMP reachability domain
- You need separate Meraki org filters or credentials
- You want alarms and documents scoped cleanly

Avoid one mega-site if collectors or credentials would conflict.
