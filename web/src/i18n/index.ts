// The i18n layer (SPEC section 9, D-46). Every user-facing string lives in
// a message catalog and is read through `t("key")`; components never hold
// literal text. V1 ships English only, so there is one catalog and no
// language detection. The catalog and its types are in core.ts.
import { initReactI18next } from "react-i18next";

import { createI18n } from "./core";

export { defaultNS } from "./core";

export const i18n = createI18n([initReactI18next]);
