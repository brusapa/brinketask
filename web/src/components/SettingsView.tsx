// Settings (SPEC section 9): the profile time zone and signing out. The
// default reminder time and the devices with notifications arrive with
// reminders in phase 5 (D-50).
import { LogOut } from "lucide-react";
import { Button } from "react-aria-components";
import { useTranslation } from "react-i18next";

import { mergePatch, unwrap } from "../api/client";
import { useProfile, useServices } from "../app/services";
import { ViewHeader } from "./ViewHeader";
import { ZonePicker } from "./ZonePicker";

/** Saves the profile zone and puts the answer in the profile store. */
export function useSaveZone(): (zone: string) => Promise<void> {
  const { api, profile, toasts } = useServices();
  const { t } = useTranslation();
  return async (zone) => {
    try {
      const user = await unwrap(() =>
        api.PATCH("/me", { body: { timezone: zone }, headers: mergePatch }),
      );
      profile.set(user);
      toasts.show({ message: t("settings.zoneSaved", { zone }), tone: "info" });
    } catch {
      toasts.show({ message: t("errors.generic"), tone: "error" });
    }
  };
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
