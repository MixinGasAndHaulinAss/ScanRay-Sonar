# World map

**World** plots agents on a geographic map using GeoIP enrichment when MaxMind (or equivalent) databases are loaded on the API host.

## How to use it

1. Open **World**.
2. Locate clusters of devices by country/city.
3. Click through to a device for details.

## If locations look wrong or missing

- GeoIP databases may not be installed yet on the server (operators run the refresh script / volume mount).
- VPN or CGNAT egress can place a device in the wrong city.
- Agents that have never reported an egress IP cannot be placed.
