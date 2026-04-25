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
