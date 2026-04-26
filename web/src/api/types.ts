// Types mirror the schemas in internal/api/openapi.yaml. Keep in sync.

export interface User {
  id: string;
  email: string;
  displayName: string;
  role: "superadmin" | "siteadmin" | "tech" | "readonly";
  totpEnrolled: boolean;
  isActive: boolean;
  lastLoginAt?: string | null;
  createdAt: string;
}

export interface Site {
  id: string;
  slug: string;
  name: string;
  timezone: string;
  description?: string | null;
  createdAt: string;
}

export interface Agent {
  id: string;
  siteId: string;
  hostname: string;
  fingerprint?: string | null;
  os: string;
  osVersion: string;
  agentVersion: string;
  enrolledAt?: string | null;
  lastSeenAt?: string | null;
  isActive: boolean;
  tags: string[];
  createdAt: string;

  // Headline telemetry — populated by the latest /agent/ws metrics
  // frame. All nullable: a freshly-enrolled probe that hasn't yet
  // sent its first snapshot has only the identity fields above.
  cpuPct?: number | null;
  memUsedBytes?: number | null;
  memTotalBytes?: number | null;
  rootDiskUsedBytes?: number | null;
  rootDiskTotalBytes?: number | null;
  uptimeSeconds?: number | null;
  pendingReboot?: boolean;
  primaryIp?: string | null;
  lastMetricsAt?: string | null;

  // GeoIP enrichment from Phase 0 of the agent feature pack. Server
  // resolves the public IP via icanhazip + bundled MaxMind mmdb on
  // ingest; nullable until the first metrics frame arrives or when
  // the API has no GeoIP databases loaded.
  publicIp?: string | null;
  geoCountryIso?: string | null;
  geoCountryName?: string | null;
  geoSubdivision?: string | null;
  geoCity?: string | null;
  geoLat?: number | null;
  geoLon?: number | null;
  geoAsn?: number | null;
  geoOrg?: string | null;
}

// AgentDetail is the /agents/{id} response — same as Agent plus the
// verbatim last snapshot, suitable for rendering the system tab
// without a second fetch.
export interface AgentDetail extends Agent {
  lastMetrics?: Snapshot | null;
}

// Snapshot mirrors internal/probe/snapshot.go. Keep field names in
// sync — the API stores this verbatim as JSONB and the UI binds to
// it directly, so a rename on either side breaks the other.
export interface Snapshot {
  schemaVersion: number;
  capturedAt: string;
  captureMs: number;
  host: SnapshotHost;
  /** Schema v3+: discovered via icanhazip; cached 1h on the probe. */
  publicIp?: string;
  cpu: SnapshotCPU;
  memory: SnapshotMemory;
  loadAvg?: { load1: number; load5: number; load15: number };
  disks: SnapshotDisk[];
  nics: SnapshotNIC[];
  /** Schema v3+: collected once at probe startup; nil if the
   *  collector failed (no DMI on this host, no permission, etc.). */
  hardware?: SnapshotHardware;
  topByCpu: SnapshotProcess[];
  topByMem: SnapshotProcess[];
  listeners: SnapshotListener[];
  /** Schema v2+: aggregated active peer conversations. Optional for back-compat. */
  conversations?: SnapshotConversation[];
  loggedInUsers: SnapshotSession[];
  pendingReboot: boolean;
  pendingRebootReason?: string;
  stoppedAutoServices?: SnapshotService[];
  failedUnits?: string[];
  collectionWarnings?: string[];
}
export interface SnapshotHost {
  hostname: string;
  os: string;
  platform: string;
  platformFamily: string;
  platformVersion: string;
  kernelVersion: string;
  kernelArch: string;
  virtualization?: string;
  bootTime: string;
  uptimeSeconds: number;
  procs: number;
}
export interface SnapshotCPU {
  model: string;
  cores: number;
  logicalCpus: number;
  mhz: number;
  usagePct: number;
  perCorePct: number[];
}
export interface SnapshotMemory {
  totalBytes: number;
  usedBytes: number;
  availableBytes: number;
  usedPct: number;
  swapTotalBytes: number;
  swapUsedBytes: number;
  swapUsedPct: number;
}
export interface SnapshotDisk {
  device: string;
  mountpoint: string;
  fsType: string;
  totalBytes: number;
  usedBytes: number;
  freeBytes: number;
  usedPct: number;
}
export interface SnapshotNIC {
  name: string;
  mac?: string;
  mtu?: number;
  up: boolean;
  addresses?: string[];
  bytesSent: number;
  bytesRecv: number;
  pktsSent: number;
  pktsRecv: number;
  errIn: number;
  errOut: number;
  dropIn: number;
  dropOut: number;
}
export interface SnapshotProcess {
  pid: number;
  name: string;
  user?: string;
  cmdline?: string;
  cpuPct: number;
  rssBytes: number;
  /** Schema v3+: percent of physical memory used. */
  memPct?: number;
  /** Schema v3+: bytes/second; nil on platforms where the probe
   *  can't read per-process I/O counters (notably containerised hosts
   *  without CAP_SYS_PTRACE). */
  diskReadBps?: number;
  diskWriteBps?: number;
  netSentBps?: number;
  netRecvBps?: number;
  /** Open TCP/UDP sockets owned by this process. */
  openConns?: number;
}

// SnapshotHardware mirrors internal/probe/hardware.go. Optional —
// older probes (schema v2) don't send this section. Every sub-field
// is optional too: a hypervisor will lack DMI, a server may not
// expose GPU, etc.
export interface SnapshotHardware {
  system?: {
    manufacturer?: string;
    productName?: string;
    serialNumber?: string;
    chassisType?: string;
    chassisAssetTag?: string;
    biosVendor?: string;
    biosVersion?: string;
    biosDate?: string;
    boardManufacturer?: string;
    boardProduct?: string;
    boardSerial?: string;
  };
  cpu?: { model?: string; cores?: number; threads?: number; mhzNominal?: number };
  memoryModules?: HardwareMemoryModule[];
  storage?: HardwareDisk[];
  networkAdapters?: HardwareNIC[];
  gpus?: HardwareGPU[];
  collectionWarnings?: string[];
}
export interface HardwareMemoryModule {
  slot?: string;
  manufacturer?: string;
  partNumber?: string;
  serialNumber?: string;
  speedMhz?: number;
  type?: string;
  formFactor?: string;
  sizeBytes?: number;
}
export interface HardwareDisk {
  device?: string;
  model?: string;
  serial?: string;
  vendor?: string;
  busType?: string;
  formFactor?: string;
  sizeBytes?: number;
  rotational?: boolean;
}
export interface HardwareNIC {
  name?: string;
  vendor?: string;
  product?: string;
  driver?: string;
  busInfo?: string;
  mac?: string;
  speedMbps?: number;
}
export interface HardwareGPU {
  vendor?: string;
  product?: string;
  driver?: string;
  busInfo?: string;
}
export interface SnapshotListener {
  proto: "tcp" | "udp";
  address: string;
  port: number;
  pid?: number;
  processName?: string;
}
export interface SnapshotConversation {
  proto: "tcp" | "udp";
  direction: "inbound" | "outbound" | "local";
  remoteIp: string;
  remoteHost?: string;
  remotePort: number;
  /** Set for inbound conversations — the local listening port being hit. */
  localPort?: number;
  state?: string;
  pid?: number;
  processName?: string;
  /** Number of socket rows aggregated into this conversation. */
  count: number;
}
export interface SnapshotSession {
  user: string;
  tty?: string;
  host?: string;
  started?: string;
}
export interface SnapshotService {
  name: string;
  displayName?: string;
  startType?: string;
  status?: string;
}

export interface MetricSample {
  time: string;
  cpuPct?: number | null;
  memUsedBytes?: number | null;
  memTotalBytes?: number | null;
  rootDiskUsedBytes?: number | null;
  rootDiskTotalBytes?: number | null;
}

export interface MetricSeries {
  agentId: string;
  range: string;
  samples: MetricSample[];
  capturedAtTo: string;
}

export interface Appliance {
  id: string;
  siteId: string;
  name: string;
  vendor: "meraki" | "cisco" | "aruba" | "ubiquiti" | "mikrotik" | "generic";
  model?: string | null;
  serial?: string | null;
  mgmtIp: string;
  snmpVersion: "v1" | "v2c" | "v3";
  pollIntervalSeconds: number;
  isActive: boolean;
  tags: string[];
  lastPolledAt?: string | null;
  lastError?: string | null;
  createdAt: string;

  // Headline telemetry from the most recent SNMP poll. All nullable
  // until the poller's first cycle for a freshly-added appliance.
  sysName?: string | null;
  uptimeSeconds?: number | null;
  cpuPct?: number | null;
  memUsedBytes?: number | null;
  memTotalBytes?: number | null;
  ifUpCount?: number | null;
  ifTotalCount?: number | null;

  /**
   * Counts derived from the per-interface `kind` classification: how
   * many real (RJ45/SFP/etc.) ports the appliance has, and how many of
   * those are operationally up. These differ from `ifTotalCount` /
   * `ifUpCount`, which include SVIs, port-channels, loopbacks, etc.
   * Prefer these in any operator-facing "X ports" UI.
   */
  physTotalCount?: number | null;
  physUpCount?: number | null;
  /** Heuristic count of inter-switch uplink ports. */
  uplinkCount?: number | null;
}

export interface ApplianceDetail extends Appliance {
  sysDescr?: string | null;
  lastSnapshotAt?: string | null;
  lastSnapshot?: ApplianceSnapshot | null;
}

// ApplianceSnapshot mirrors internal/snmp/snapshot.go.
export interface ApplianceSnapshot {
  schemaVersion: number;
  capturedAt: string;
  collectMs: number;
  system: ApplianceSystem;
  chassis: ApplianceChassis;
  interfaces: ApplianceInterface[];
  entities?: ApplianceEntity[];
  lldp?: ApplianceLLDP[];
  collectionWarnings?: string[];
}
export interface ApplianceSystem {
  description: string;
  name: string;
  contact?: string;
  location?: string;
  objectId?: string;
  uptimeTicks: number;
  uptimeSeconds: number;
}
export interface ApplianceChassis {
  cpuPct?: number | null;
  memUsedBytes?: number | null;
  memTotalBytes?: number | null;
  tempC?: number | null;
}
export interface ApplianceInterface {
  ifIndex: number;
  name: string;
  descr: string;
  alias?: string;
  type: number;
  /**
   * Server-side classification: "physical" is a real RJ45/SFP-cage port,
   * everything else is logical (VLAN SVIs, loopbacks, tunnels, LAGs, etc.)
   * and is hidden by default in operator views.
   */
  kind?: "physical" | "vlan" | "loopback" | "tunnel" | "lag" | "mgmt" | "stack" | "other";
  /** Heuristic flag for inter-switch uplink ports — pinned to the top of tables and badged. */
  isUplink?: boolean;
  mtu?: number;
  speedBps?: number;
  mac?: string;
  adminUp: boolean;
  operUp: boolean;
  lastChangeSeconds?: number;
  inOctets: number;
  outOctets: number;
  inUcast?: number;
  outUcast?: number;
  inErrors?: number;
  outErrors?: number;
  inDiscards?: number;
  outDiscards?: number;
  inBps?: number | null;
  outBps?: number | null;
}
export interface ApplianceEntity {
  index: number;
  class: number;
  description: string;
  name?: string;
  hardwareRev?: string;
  firmwareRev?: string;
  softwareRev?: string;
  serial?: string;
  modelName?: string;
}
export interface ApplianceLLDP {
  localIfIndex: number;
  localPort?: string;
  remoteSysName?: string;
  remoteSysDescr?: string;
  remotePortId?: string;
  remotePortDescr?: string;
  remoteChassisId?: string;
}

export interface ApplianceMetricSample {
  time: string;
  cpuPct?: number | null;
  memUsedBytes?: number | null;
  memTotalBytes?: number | null;
}
export interface ApplianceMetricSeries {
  applianceId: string;
  range: string;
  capturedAtTo: string;
  samples: ApplianceMetricSample[];
}

export interface ApplianceIfaceSample {
  time: string;
  inBps?: number | null;
  outBps?: number | null;
  inErrors?: number | null;
  outErrors?: number | null;
  inDiscards?: number | null;
  outDiscards?: number | null;
}
export interface ApplianceIfaceSeries {
  applianceId: string;
  ifIndex: number;
  range: string;
  capturedAtTo: string;
  samples: ApplianceIfaceSample[];
}

export interface EnrollmentToken {
  id: string;
  siteId: string;
  label: string;
  maxUses: number;
  usedCount: number;
  expiresAt: string;
  revokedAt?: string | null;
  createdAt: string;
  isValid: boolean;
}

export interface NewEnrollmentToken {
  id: string;
  siteId: string;
  label: string;
  token: string;
  expiresAt: string;
  maxUses: number;
  /** Linux install one-liner (kept for back-compat). */
  installCmd: string;
  /** Per-OS install one-liners — pick the one that matches the target host. */
  installCmds: {
    linux: string;
    windows: string;
  };
}

// AgentNetworkGraph is the /agents/{id}/network-graph response. It's
// the conversations + listeners pulled out of the latest snapshot,
// aggregated by remote IP and enriched with GeoIP/ASN data.
export interface AgentNetworkGraph {
  agent: {
    id: string;
    hostname: string;
    publicIp?: string | null;
    primaryIp?: string | null;
    countryIso?: string | null;
    city?: string | null;
    lat?: number | null;
    lon?: number | null;
    asn?: number | null;
    org?: string | null;
  };
  capturedAt?: string | null;
  peers: AgentNetworkPeer[];
  processes: string[];
}

export interface AgentNetworkPeer {
  ip: string;
  host?: string;
  asn?: number;
  org?: string;
  countryIso?: string;
  countryName?: string;
  city?: string;
  lat?: number;
  lon?: number;
  direction: "inbound" | "outbound" | "local";
  isPrivate?: boolean;
  totalConns: number;
  ports?: number[];
  processes: { name: string; pid?: number; count: number }[];
}

export interface VersionInfo {
  version: string;
  commit: string;
  buildTime: string;
  goVersion: string;
}

export interface TopologyNode {
  id: string;
  /** "appliance" = managed by us; "foreign" = only learned via a neighbor's CDP/LLDP. */
  kind: "appliance" | "foreign";
  name: string;
  label: string;
  vendor?: string;
  model?: string;
  platform?: string;
  mgmtIp?: string;
  siteId?: string;
  status: "up" | "degraded" | "down" | "unknown";
  lastSeenAt?: string;
  portsTotal?: number;
  portsUp?: number;
  uplinkCount?: number;
  /** User-defined tags carried over from the appliance row (managed nodes only). */
  tags?: string[];
  /** True when MgmtIP is in an RFC1918 / link-local / loopback block. */
  isPrivate?: boolean;
  /** MaxMind enrichment of MgmtIP (public addresses only). */
  countryIso?: string;
  countryName?: string;
  city?: string;
  lat?: number;
  lon?: number;
  asn?: number;
  org?: string;
}

export interface TopologyEdge {
  from: string;
  to: string;
  fromPort?: string;
  toPort?: string;
  /** "both" means the same link was reported by both LLDP and CDP (or by both ends). */
  protocol: "lldp" | "cdp" | "both";
  operUp: boolean;
}

export interface Topology {
  generatedAt: string;
  nodes: TopologyNode[];
  edges: TopologyEdge[];
}

export interface LoginResponse {
  accessToken: string;
  refreshToken: string;
  expiresAt: string;
  mfaRequired?: boolean;
  user: User;
}
