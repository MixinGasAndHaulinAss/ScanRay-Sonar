# Topology

**Topology** shows how agents and appliances relate across the sites you can access—neighbors from LLDP/CDP (and Meraki topology where available), plus inventory linkage.

## How to use it

1. Open **Topology**.
2. Orient on the site or region you care about.
3. Click nodes to jump into appliance or device detail.

Use topology to answer “what is connected to what?” and to spot missing neighbor data (empty edges often mean SNMP/LLDP not collected yet, or Meraki discovery soft-failed).

It is not a replacement for a network management NMS design tool; it reflects what Sonar has learned from polls and Dashboard APIs.
