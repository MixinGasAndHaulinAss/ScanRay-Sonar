// Agents — overview shell.
//
// Replaces the old monolithic Agents page with a dropdown switcher
// that routes to one of seven sub-pages under pages/agents/. The
// dropdown selection persists across reloads in localStorage so an
// operator who lives in "Network · Latency" doesn't have to re-pick
// it every refresh. Filters/search remain inside the Devices view
// since they only matter there.

import { useEffect, useState } from "react";
import OverviewSelect, {
  loadOverviewView,
  saveOverviewView,
  type OverviewView,
} from "../components/OverviewSelect";
import ApplicationsPerformance from "./agents/ApplicationsPerformance";
import Devices from "./agents/Devices";
import DevicesAverages from "./agents/DevicesAverages";
import DevicesPerformance from "./agents/DevicesPerformance";
import Enrollment from "./agents/Enrollment";
import NetworkLatency from "./agents/NetworkLatency";
import NetworkPerformance from "./agents/NetworkPerformance";
import Overview from "./agents/Overview";
import UserExperience from "./agents/UserExperience";

export default function Agents() {
  const [view, setView] = useState<OverviewView>(loadOverviewView);
  useEffect(() => saveOverviewView(view), [view]);

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Agents</h2>
          <p className="text-sm text-slate-400">
            Hosts running the Sonar Probe. Switch the view dropdown to drill into
            device, network, or user-experience aggregations.
          </p>
        </div>
        <OverviewSelect value={view} onChange={setView} />
      </div>

      <ViewBody view={view} />
    </div>
  );
}

function ViewBody({ view }: { view: OverviewView }) {
  switch (view) {
    case "overview":
      return <Overview />;
    case "devices":
      return <Devices />;
    case "devices-averages":
      return <DevicesAverages />;
    case "devices-performance":
      return <DevicesPerformance />;
    case "network-latency":
      return <NetworkLatency />;
    case "network-performance":
      return <NetworkPerformance />;
    case "applications-performance":
      return <ApplicationsPerformance />;
    case "user-experience":
      return <UserExperience />;
    case "enrollment":
      return <Enrollment />;
    default: {
      // exhaustiveness check — adding a new OverviewView without
      // wiring a case here is a TS error.
      const _exhaustive: never = view;
      void _exhaustive;
      return null;
    }
  }
}
