// ErrorBoundary — last-line-of-defense for React render failures.
//
// Without this, an unhandled exception during render unmounts the
// entire tree and the user sees a blank dark page (the body bg-color
// shows through). That's much worse than a useful error card: an
// operator can't tell "the page crashed" from "I clicked the wrong
// link". This component catches render errors anywhere below it and
// shows the message + stack so we can actually see what went wrong
// without having to ask the user to open DevTools.
//
// Scope: we mount one at the root in main.tsx so any page-level
// crash stays visible. Local error boundaries can also be used to
// keep a panel-level failure from blanking the whole screen.

import { Component, type ErrorInfo, type ReactNode } from "react";

interface Props {
  children: ReactNode;
  /** Optional label shown in the heading; defaults to "Application error". */
  label?: string;
}

interface State {
  error: Error | null;
  info: ErrorInfo | null;
}

export default class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null, info: null };

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    this.setState({ info });
    console.error("ErrorBoundary caught:", error, info);
  }

  reset = () => this.setState({ error: null, info: null });

  render() {
    if (!this.state.error) return this.props.children;

    const e = this.state.error;
    return (
      <div className="min-h-full bg-ink-950 px-6 py-10 text-slate-100">
        <div className="mx-auto max-w-2xl rounded-xl border border-red-800/60 bg-red-950/30 p-6">
          <h1 className="text-lg font-semibold text-red-200">
            {this.props.label ?? "Application error"}
          </h1>
          <p className="mt-1 text-sm text-red-300/80">
            Something on this page threw and React stopped rendering. The
            error and component stack are below — share them with engineering
            (or just hit reload after fixing).
          </p>
          <div className="mt-4 rounded-md border border-red-900/50 bg-ink-950/70 p-3 font-mono text-xs">
            <div className="text-red-200">{e.name}: {e.message}</div>
            {e.stack && (
              <pre className="mt-2 max-h-60 overflow-auto whitespace-pre-wrap text-[11px] text-slate-300">
                {e.stack}
              </pre>
            )}
            {this.state.info?.componentStack && (
              <details className="mt-2">
                <summary className="cursor-pointer text-[11px] text-slate-400">
                  component stack
                </summary>
                <pre className="mt-1 max-h-60 overflow-auto whitespace-pre-wrap text-[11px] text-slate-400">
                  {this.state.info.componentStack}
                </pre>
              </details>
            )}
          </div>
          <div className="mt-4 flex gap-2">
            <button
              type="button"
              onClick={this.reset}
              className="rounded-md border border-red-700 bg-red-900/40 px-3 py-1.5 text-sm text-red-100 hover:bg-red-900/60"
            >
              Try again
            </button>
            <button
              type="button"
              onClick={() => window.location.reload()}
              className="rounded-md border border-ink-700 bg-ink-800 px-3 py-1.5 text-sm text-slate-200 hover:bg-ink-700"
            >
              Reload page
            </button>
          </div>
        </div>
      </div>
    );
  }
}
