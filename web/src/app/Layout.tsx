// The frame around every screen (DESIGN.md section 2): sidebar, the screen
// itself, and the task detail panel while a task is selected. Below 768 px
// the sidebar is a drawer and the detail takes the whole screen; the CSS
// does the arrangement, driven by two data attributes.
import { useState, type ReactNode } from "react";
import { RouterProvider } from "react-aria-components";
import { Outlet, useHref, useLocation, useNavigate, useSearchParams } from "react-router";

import { Sidebar } from "../components/Sidebar";
import { Toasts } from "../components/Toasts";
import { ZoneBanner } from "../components/ZoneBanner";
import { LayoutContext } from "./layoutContext";

export function Layout({ detail }: { detail: (taskId: string, close: () => void) => ReactNode }) {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const location = useLocation();
  const taskId = params.get("task");

  // Choosing a destination closes the drawer. React allows setting state
  // during render when it derives from a value seen in the previous render.
  const [shownPath, setShownPath] = useState(location.pathname);
  if (shownPath !== location.pathname) {
    setShownPath(location.pathname);
    setDrawerOpen(false);
  }

  const closeDetail = () => {
    const next = new URLSearchParams(params);
    next.delete("task");
    setParams(next);
  };

  return (
    // RouterProvider lets React Aria links (href on rows) navigate through
    // the app's router instead of reloading the page.
    <RouterProvider navigate={(path) => void navigate(path)} useHref={useHref}>
      <LayoutContext.Provider value={{ openDrawer: () => setDrawerOpen(true) }}>
        <div
          className="layout"
          data-drawer={drawerOpen ? "open" : "closed"}
          data-detail={taskId !== null ? "open" : "closed"}
        >
          <Sidebar />
          {/* Clicking outside the open drawer closes it; keyboard users
              close it by choosing a destination. */}
          <div
            className="drawer-backdrop"
            aria-hidden="true"
            onClick={() => setDrawerOpen(false)}
          />
          <main className="main">
            <ZoneBanner />
            <Outlet />
          </main>
          {taskId !== null && detail(taskId, closeDetail)}
          <Toasts />
        </div>
      </LayoutContext.Provider>
    </RouterProvider>
  );
}
