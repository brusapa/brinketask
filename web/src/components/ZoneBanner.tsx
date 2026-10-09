// The offer to update the profile zone when the browser is in another one
// (SPEC section 9, D-47). "Keep" is remembered per browser zone, so the
// offer comes back only if the device moves again.
import { useState } from "react";
import { Button } from "react-aria-components";
import { useTranslation } from "react-i18next";

import { useProfile, useServices } from "../app/services";
import { useSaveZone } from "./SettingsView";

const storageKey = "brinketask.keptZoneFor";

/** Browser storage can be missing or refuse access; that only re-shows the offer. */
function readKept(): string | null {
  try {
    return window.localStorage.getItem(storageKey);
  } catch {
    return null;
  }
}

function writeKept(zone: string): void {
  try {
    window.localStorage.setItem(storageKey, zone);
  } catch {
    // Nothing to do: the offer will show again next time.
  }
}

export function ZoneBanner() {
  const { t } = useTranslation();
  const { browserZone } = useServices();
  const profile = useProfile();
  const saveZone = useSaveZone();
  const [kept, setKept] = useState(readKept);

  if (browserZone === profile.timezone || kept === browserZone) {
    return null;
  }
  return (
    <div className="banner" role="region" aria-label={t("zone.label")}>
      <span>{t("zone.message", { device: browserZone, profile: profile.timezone })}</span>
      <Button className="button button-primary" onPress={() => void saveZone(browserZone)}>
        {t("zone.use", { zone: browserZone })}
      </Button>
      <Button
        className="button"
        onPress={() => {
          writeKept(browserZone);
          setKept(browserZone);
        }}
      >
        {t("zone.keep", { zone: profile.timezone })}
      </Button>
    </div>
  );
}
