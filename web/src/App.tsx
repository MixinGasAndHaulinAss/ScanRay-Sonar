import { Navigate, Route, Routes } from "react-router-dom";
import { tokens } from "./api/client";
import Login from "./pages/Login";
import Dashboard from "./pages/Dashboard";
import Sites from "./pages/Sites";
import Agents from "./pages/Agents";
import AgentDetailPage from "./pages/AgentDetail";
import Appliances from "./pages/Appliances";
import ApplianceDetailPage from "./pages/ApplianceDetail";
import Topology from "./pages/Topology";
import Collectors from "./pages/Collectors";
import Settings from "./pages/Settings";
import Discovery from "./pages/Discovery";
import ApiKeys from "./pages/ApiKeys";
import Alarms from "./pages/Alarms";
import AuditLog from "./pages/AuditLog";
import SiteDocuments from "./pages/SiteDocuments";
import SiteNetworkMap from "./pages/SiteNetworkMap";
import World from "./pages/World";
import Users from "./pages/Users";
import Layout from "./components/Layout";
import ErrorBoundary from "./components/ErrorBoundary";

function RequireAuth({ children }: { children: React.ReactNode }) {
  if (!tokens.get()) return <Navigate to="/login" replace />;
  return <Layout>{children}</Layout>;
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        path="/"
        element={
          <RequireAuth>
            <Dashboard />
          </RequireAuth>
        }
      />
      <Route
        path="/sites"
        element={
          <RequireAuth>
            <Sites />
          </RequireAuth>
        }
      />
      <Route
        path="/agents"
        element={
          <RequireAuth>
            <Agents />
          </RequireAuth>
        }
      />
      <Route
        path="/agents/:id"
        element={
          <RequireAuth>
            <ErrorBoundary label="Agent detail crashed">
              <AgentDetailPage />
            </ErrorBoundary>
          </RequireAuth>
        }
      />
      <Route
        path="/appliances"
        element={
          <RequireAuth>
            <Appliances />
          </RequireAuth>
        }
      />
      <Route
        path="/appliances/:id"
        element={
          <RequireAuth>
            <ErrorBoundary label="Appliance detail crashed">
              <ApplianceDetailPage />
            </ErrorBoundary>
          </RequireAuth>
        }
      />
      <Route
        path="/sites/:siteId/map"
        element={
          <RequireAuth>
            <ErrorBoundary label="Site map crashed">
              <SiteNetworkMap />
            </ErrorBoundary>
          </RequireAuth>
        }
      />
      <Route
        path="/collectors"
        element={
          <RequireAuth>
            <Collectors />
          </RequireAuth>
        }
      />
      <Route
        path="/settings"
        element={
          <RequireAuth>
            <Settings />
          </RequireAuth>
        }
      />
      <Route
        path="/discovery"
        element={
          <RequireAuth>
            <Discovery />
          </RequireAuth>
        }
      />
      <Route
        path="/api-keys"
        element={
          <RequireAuth>
            <ApiKeys />
          </RequireAuth>
        }
      />
      <Route
        path="/alarms"
        element={
          <RequireAuth>
            <Alarms />
          </RequireAuth>
        }
      />
      <Route
        path="/audit-log"
        element={
          <RequireAuth>
            <AuditLog />
          </RequireAuth>
        }
      />
      <Route
        path="/documents"
        element={
          <RequireAuth>
            <SiteDocuments />
          </RequireAuth>
        }
      />
      <Route
        path="/topology"
        element={
          <RequireAuth>
            <ErrorBoundary label="Topology crashed">
              <Topology />
            </ErrorBoundary>
          </RequireAuth>
        }
      />
      <Route
        path="/world"
        element={
          <RequireAuth>
            <ErrorBoundary label="World map crashed">
              <World />
            </ErrorBoundary>
          </RequireAuth>
        }
      />
      <Route
        path="/users"
        element={
          <RequireAuth>
            <Users />
          </RequireAuth>
        }
      />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
