// The message catalog and its i18next instance, without React, so the
// service worker (src/sw) can share the app's texts (D-65): one i18n
// layer for the whole client (SPEC section 9).
import i18next, { type i18n as I18n } from "i18next";

import en from "./en.json";

export const defaultNS = "translation";

// Declaration merging: this tells the TypeScript types of i18next which
// keys exist, so `t("no.such.key")` is a compile error.
declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: typeof defaultNS;
    resources: { translation: typeof en };
  }
}

/**
 * A new i18next instance with the catalog, set up synchronously (the
 * catalog is bundled). plugins are applied before init, e.g. React's.
 */
export function createI18n(plugins: Parameters<I18n["use"]>[0][] = []): I18n {
  const instance = i18next.createInstance();
  for (const plugin of plugins) instance.use(plugin);
  void instance.init({
    lng: "en",
    fallbackLng: "en",
    resources: { en: { translation: en } },
    interpolation: {
      // React escapes values itself, and notifications are plain text;
      // escaping here would show "&amp;" to the user.
      escapeValue: false,
    },
    initAsync: false,
  });
  return instance;
}
