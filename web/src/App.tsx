import { Navigate, Route, Routes } from "react-router-dom";
import { tokens } from "./api/client";
import Login from "./pages/Login";
import Dashboard from "./pages/Dashboard";
import Sites from "./pages/Sites";
import Agents from "./pages/Agents";
import AgentDetailPage from "./pages/AgentDetail";
import Appliances from "./pages/Appliances";
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
