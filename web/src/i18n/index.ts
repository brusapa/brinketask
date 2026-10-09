// The i18n layer (SPEC section 9, D-46). Every user-facing string lives in
// a message catalog and is read through `t("key")`; components never hold
// literal text. V1 ships English only, so there is one catalog and no
// language detection.
import i18next from "i18next";
import { initReactI18next } from "react-i18next";

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

export const i18n = i18next.createInstance();

// initImmediate: false makes init synchronous, since the catalog is bundled;
// the first render then already has its strings.
void i18n.use(initReactI18next).init({
  lng: "en",
  fallbackLng: "en",
  resources: { en: { translation: en } },
  interpolation: {
    // React escapes values itself; escaping here would double it.
    escapeValue: false,
  },
  initAsync: false,
});
