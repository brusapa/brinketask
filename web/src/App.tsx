// The screens and their URLs. Every screen has its own path, so reloading
// or sharing a URL shows the same thing; the open task is ?task=<id>.
import { BrowserRouter, Navigate, Route, Routes, useParams, useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";

import { Layout } from "./app/Layout";
import { useNow, useProfile, useSnapshot } from "./app/services";
import { TaskDetail } from "./components/detail/TaskDetail";
import { SettingsView } from "./components/SettingsView";
import { TaskListView } from "./components/TaskListView";
import { TrashView } from "./components/TrashView";
import { dateIn } from "./lib/dates";
import { formatLongDay } from "./lib/format";

export function App() {
  return (
    <BrowserRouter>
      <AppRoutes />
    </BrowserRouter>
  );
}

export function AppRoutes() {
  return (
    <Routes>
      <Route
        element={
          <Layout detail={(taskId, close) => <TaskDetail taskId={taskId} onClose={close} />} />
        }
      >
        <Route index element={<InboxScreen />} />
        <Route path="today" element={<TodayScreen />} />
        <Route path="next7" element={<Next7Screen />} />
        <Route path="lists/:listId" element={<ListScreen />} />
        <Route path="tags/:tagId" element={<TagScreen />} />
        <Route path="search" element={<SearchScreen />} />
        <Route path="trash" element={<TrashView />} />
        <Route path="settings" element={<SettingsView />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}

function InboxScreen() {
  const { t } = useTranslation();
  const profile = useProfile();
  return (
    <TaskListView view={{ kind: "list", listId: profile.inbox_list_id }} title={t("nav.inbox")} />
  );
}

function TodayScreen() {
  const { t, i18n } = useTranslation();
  const zone = useProfile().timezone;
  const now = useNow();
  return (
    <TaskListView
      view={{ kind: "today" }}
      title={t("nav.today")}
      info={formatLongDay(dateIn(now, zone), zone, i18n.language)}
    />
  );
}

function Next7Screen() {
  const { t } = useTranslation();
  return <TaskListView view={{ kind: "next7" }} title={t("nav.next7")} />;
}

function ListScreen() {
  const { listId = "" } = useParams();
  const snapshot = useSnapshot();
  const list = snapshot.lists.get(listId);
  // A deleted or unknown list: back to the inbox.
  if (list === undefined) return <Navigate to="/" replace />;
  // `key` gives each list its own component state (quick-add text).
  return <TaskListView key={listId} view={{ kind: "list", listId }} title={list.name} />;
}

function TagScreen() {
  const { tagId = "" } = useParams();
  const snapshot = useSnapshot();
  const tag = snapshot.tags.get(tagId);
  if (tag === undefined) return <Navigate to="/" replace />;
  return <TaskListView key={tagId} view={{ kind: "tag", tagId }} title={`#${tag.name}`} />;
}

function SearchScreen() {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const query = params.get("q") ?? "";
  return <TaskListView view={{ kind: "search", query }} title={t("search.title")} />;
}
