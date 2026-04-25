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
  cpu: SnapshotCPU;
  memory: SnapshotMemory;
  loadAvg?: { load1: number; load5: number; load15: number };
  disks: SnapshotDisk[];
  nics: SnapshotNIC[];
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

export interface VersionInfo {
  version: string;
  commit: string;
  buildTime: string;
  goVersion: string;
}

export interface LoginResponse {
  accessToken: string;
  refreshToken: string;
  expiresAt: string;
  mfaRequired?: boolean;
  user: User;
}
