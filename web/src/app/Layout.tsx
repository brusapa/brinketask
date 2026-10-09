// The frame around every screen (DESIGN.md section 2): sidebar, the screen
// itself, and the task detail panel while a task is selected. Below 768 px
// the sidebar is a drawer and the detail takes the whole screen; the CSS
// does the arrangement, driven by two data attributes.
import { useState, type ReactNode } from "react";
import { Outlet, useSearchParams } from "react-router";

import { Sidebar } from "../components/Sidebar";
import { Toasts } from "../components/Toasts";
import { LayoutContext } from "./layoutContext";

export function Layout({ detail }: { detail: (taskId: string, close: () => void) => ReactNode }) {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [params, setParams] = useSearchParams();
  const taskId = params.get("task");

  const closeDetail = () => {
    const next = new URLSearchParams(params);
    next.delete("task");
    setParams(next);
  };

  return (
    <LayoutContext.Provider value={{ openDrawer: () => setDrawerOpen(true) }}>
      <div
        className="layout"
        data-drawer={drawerOpen ? "open" : "closed"}
        data-detail={taskId !== null ? "open" : "closed"}
      >
        <Sidebar onNavigate={() => setDrawerOpen(false)} />
        {/* Clicking outside the open drawer closes it; keyboard users
            close it by choosing a destination. */}
        <div className="drawer-backdrop" aria-hidden="true" onClick={() => setDrawerOpen(false)} />
        <main className="main">
          <Outlet />
        </main>
        {taskId !== null && detail(taskId, closeDetail)}
        <Toasts />
      </div>
    </LayoutContext.Provider>
  );
}
