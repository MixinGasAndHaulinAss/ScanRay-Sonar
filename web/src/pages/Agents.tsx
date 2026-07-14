// Devices shell — ControlUp-style Overview | Details | Data | Events | Compliance | Groups | Reports IA.

import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import Devices from "./agents/Devices";
import DevicesCompliance from "./agents/DevicesCompliance";
import DevicesData from "./agents/DevicesData";
import DevicesEvents from "./agents/DevicesEvents";
import DevicesGroups from "./agents/DevicesGroups";
import DevicesReports from "./agents/DevicesReports";
import Enrollment from "./agents/Enrollment";
import Overview, { type OverviewPanel } from "./agents/Overview";

type DevicesTab =
  | "overview"
  | "details"
  | "data"
  | "events"
  | "compliance"
  | "groups"
  | "reports"
  | "enrollment";

const TAB_KEY = "sonar.devices.tab";
const PANEL_KEY = "sonar.devices.overview.panel";

function loadTab(): DevicesTab {
  try {
    const v = localStorage.getItem(TAB_KEY);
    const ok: DevicesTab[] = [
      "overview",
      "details",
      "data",
      "events",
      "compliance",
      "groups",
      "reports",
      "enrollment",
    ];
    if (ok.includes(v as DevicesTab)) return v as DevicesTab;
  } catch {
    /* ignore */
  }
  return "overview";
}

function loadPanel(): OverviewPanel {
  try {
    const v = localStorage.getItem(PANEL_KEY);
    const ok: OverviewPanel[] = [
      "home",
      "user-experience",
      "applications",
      "network-latency",
      "network-performance",
      "devices-performance",
      "devices-averages",
    ];
    if (ok.includes(v as OverviewPanel)) return v as OverviewPanel;
  } catch {
    /* ignore */
  }
  return "home";
}

const TABS: { id: DevicesTab; label: string }[] = [
  { id: "overview", label: "Overview" },
  { id: "details", label: "Details" },
  { id: "data", label: "Data" },
  { id: "events", label: "Events" },
  { id: "compliance", label: "Compliance" },
  { id: "groups", label: "Groups" },
  { id: "reports", label: "Reports" },
];

export default function Agents() {
  const [tab, setTab] = useState<DevicesTab>(loadTab);
  const [panel, setPanel] = useState<OverviewPanel>(loadPanel);
  useEffect(() => {
    try {
      localStorage.setItem(TAB_KEY, tab);
    } catch {
      /* ignore */
    }
  }, [tab]);
  useEffect(() => {
    try {
      localStorage.setItem(PANEL_KEY, panel);
    } catch {
      /* ignore */
    }
  }, [panel]);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Devices</h2>
          <p className="text-sm text-slate-400">
            Endpoint fleet visibility — live grid, DEX history, events, compliance, and reports.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setTab("enrollment")}
          className="text-xs text-sonar-400 hover:underline"
        >
          Enrollment tokens
        </button>
      </div>

      <div className="flex flex-wrap items-center gap-1 border-b border-ink-800">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => setTab(t.id)}
            className={
              "relative px-4 py-2 text-sm font-medium transition " +
              (tab === t.id
                ? "text-sonar-200 after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded after:bg-sonar-400"
                : "text-slate-400 hover:text-slate-200")
            }
          >
            {t.label}
          </button>
        ))}
        {tab === "enrollment" && (
          <span className="relative px-4 py-2 text-sm font-medium text-sonar-200 after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded after:bg-sonar-400">
            Enrollment
          </span>
        )}
      </div>

      {tab === "overview" && <Overview panel={panel} onPanel={setPanel} />}
      {tab === "details" && <Devices />}
      {tab === "data" && <DevicesData />}
      {tab === "events" && <DevicesEvents />}
      {tab === "compliance" && <DevicesCompliance />}
      {tab === "groups" && <DevicesGroups />}
      {tab === "reports" && <DevicesReports />}
      {tab === "enrollment" && (
        <div className="space-y-3">
          <p className="text-sm text-slate-400">
            Issue install tokens for new probes.{" "}
            <Link
              to="#"
              className="text-sonar-400 hover:underline"
              onClick={(e) => {
                e.preventDefault();
                setTab("details");
              }}
            >
              Back to Details
            </Link>
          </p>
          <Enrollment />
        </div>
      )}
    </div>
  );
}
