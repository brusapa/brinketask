// Settings (SPEC section 9): the profile time zone, the time of all-day
// reminders, notifications on this device and the list of devices, and
// signing out.
import { parseTime, type Time } from "@internationalized/date";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { LogOut } from "lucide-react";
import { useState } from "react";
import { Button, DateInput, DateSegment, Label, TimeField } from "react-aria-components";
import { useTranslation } from "react-i18next";

import { mergePatch, unwrap } from "../api/client";
import type { PushSubscription, UserPatch } from "../api/types";
import { useProfile, useServices } from "../app/services";
import { dateIn } from "../lib/dates";
import { formatDay } from "../lib/format";
import {
  deviceLabel,
  disableNotifications,
  enableNotifications,
  thisDeviceId,
} from "../push/devices";
import { ViewHeader } from "./ViewHeader";
import { ZonePicker } from "./ZonePicker";

/**
 * Saves profile settings. The server recomputes pending reminders when the
 * zone or the default time changes (SPEC section 6), so the replica pulls
 * those changes afterwards.
 */
function useSaveProfile(): (patch: UserPatch, saved: string) => Promise<void> {
  const { api, profile, toasts, syncer } = useServices();
  const { t } = useTranslation();
  return async (patch, saved) => {
    try {
      const user = await unwrap(() => api.PATCH("/me", { body: patch, headers: mergePatch }));
      profile.set(user);
      toasts.show({ message: saved, tone: "info" });
      syncer.pull().catch(() => undefined);
    } catch {
      toasts.show({ message: t("errors.generic"), tone: "error" });
    }
  };
}

/** Saves the profile zone. */
export function useSaveZone(): (zone: string) => Promise<void> {
  const save = useSaveProfile();
  const { t } = useTranslation();
  return (zone) => save({ timezone: zone }, t("settings.zoneSaved", { zone }));
}

export function SettingsView() {
  const { t } = useTranslation();
  const user = useProfile();
  const { logout } = useServices();
  const saveZone = useSaveZone();

  return (
    <section className="view" aria-label={t("nav.settings")}>
      <ViewHeader title={t("nav.settings")} />
      <div className="settings">
        <h2 className="section-heading">{t("settings.account")}</h2>
        <p className="settings-account">{user.display_name ?? user.email ?? ""}</p>
        <div className="settings-zone">
          {/* `key` restarts the picker when the saved zone changes. */}
          <ZonePicker
            key={user.timezone}
            label={t("settings.zone")}
            value={user.timezone}
            onChange={(zone) => {
              if (zone !== user.timezone) void saveZone(zone);
            }}
          />
          <p className="view-note">{t("settings.zoneHelp")}</p>
        </div>
        <ReminderTime />
        <Notifications />
        <Button
          className="button"
          onPress={() => {
            void logout();
          }}
        >
          <LogOut size={16} strokeWidth={1.5} aria-hidden="true" />
          <span>{t("settings.logout")}</span>
        </Button>
      </div>
    </section>
  );
}

/** all_day_reminder_time (SPEC section 4), saved when the field loses focus. */
function ReminderTime() {
  const { t } = useTranslation();
  const user = useProfile();
  const save = useSaveProfile();
  const [value, setValue] = useState<Time | null>(parseTime(user.all_day_reminder_time));
  const commit = () => {
    if (value === null) return;
    const text = `${String(value.hour).padStart(2, "0")}:${String(value.minute).padStart(2, "0")}`;
    if (text !== user.all_day_reminder_time) {
      void save({ all_day_reminder_time: text }, t("settings.reminderTimeSaved", { time: text }));
    }
  };
  return (
    <div className="settings-zone">
      <TimeField className="field" value={value} onChange={setValue} onBlur={commit} hourCycle={24}>
        <Label>{t("settings.reminderTime")}</Label>
        <DateInput className="input date-input">
          {(segment) => <DateSegment segment={segment} className="date-segment" />}
        </DateInput>
      </TimeField>
      <p className="view-note">{t("settings.reminderTimeHelp")}</p>
    </div>
  );
}

/** Notifications on this device, and the user's devices (SPEC section 9). */
function Notifications() {
  const { t, i18n } = useTranslation();
  const { api, push, clock, toasts } = useServices();
  const zone = useProfile().timezone;
  const queryClient = useQueryClient();
  const devices = useQuery({
    queryKey: ["devices"],
    queryFn: () => unwrap(() => api.GET("/push/subscriptions")),
  });
  const [permission, setPermission] = useState(() => push.permission());
  const [busy, setBusy] = useState(false);
  const thisId = thisDeviceId();
  const items: PushSubscription[] = devices.data?.items ?? [];
  const registeredHere = thisId !== null && items.some((d) => d.id === thisId && !d.disabled_at);
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["devices"] });

  const turnOn = async () => {
    setBusy(true);
    try {
      setPermission(await enableNotifications(api, push, clock, deviceLabel(push, t)));
    } catch {
      toasts.show({ message: t("devices.failed"), tone: "error" });
    } finally {
      setBusy(false);
      await refresh();
    }
  };
  const turnOff = async () => {
    setBusy(true);
    await disableNotifications(api, push).catch(() => undefined);
    setBusy(false);
    await refresh();
  };

  let status: string;
  if (!push.supported) status = t("devices.unsupported");
  else if (permission === "denied") status = t("devices.blocked");
  else status = registeredHere ? t("devices.on") : t("devices.off");

  return (
    <section className="settings-zone" aria-labelledby="settings-notifications">
      <h2 className="section-heading" id="settings-notifications">
        {t("devices.heading")}
      </h2>
      <p>{status}</p>
      {push.supported && permission !== "denied" && (
        <Button
          className="button"
          isDisabled={busy}
          onPress={() => void (registeredHere ? turnOff() : turnOn())}
        >
          {registeredHere ? t("devices.turnOff") : t("devices.turnOn")}
        </Button>
      )}
      <h3 className="field-label">{t("devices.list")}</h3>
      {items.length === 0 && devices.isSuccess && <p className="view-note">{t("devices.none")}</p>}
      <ul className="device-list">
        {items.map((device) => {
          const label = device.label ?? t("devices.unknown");
          return (
            <li key={device.id} className="device">
              <span className="device-label">
                {label}
                {device.id === thisId && <span className="chip">{t("devices.thisDevice")}</span>}
              </span>
              <span className="view-note">
                {device.disabled_at
                  ? t("devices.unreachable")
                  : device.last_success_at
                    ? t("devices.lastSent", {
                        date: formatDay(
                          dateIn(new Date(device.last_success_at), zone),
                          zone,
                          i18n.language,
                        ),
                      })
                    : ""}
              </span>
              <Button
                className="header-button"
                aria-label={t("devices.testDevice", { label })}
                onPress={() => {
                  void unwrap(() =>
                    api.POST("/push/subscriptions/{id}/test", {
                      params: { path: { id: device.id } },
                    }),
                  ).then(
                    () => toasts.show({ message: t("devices.testSent"), tone: "info" }),
                    () => toasts.show({ message: t("errors.generic"), tone: "error" }),
                  );
                }}
              >
                {t("devices.test")}
              </Button>
              <Button
                className="header-button"
                aria-label={t("devices.removeDevice", { label })}
                onPress={() => {
                  void unwrap(() =>
                    api.DELETE("/push/subscriptions/{id}", { params: { path: { id: device.id } } }),
                  ).then(refresh, () =>
                    toasts.show({ message: t("errors.generic"), tone: "error" }),
                  );
                }}
              >
                {t("devices.remove")}
              </Button>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
