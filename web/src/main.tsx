// Entry point of the web client. The font is bundled and served by our own
// server (DESIGN.md section 3), Latin subset only: English, and Spanish in V2.
import "@fontsource/ibm-plex-sans/latin-400.css";
import "@fontsource/ibm-plex-sans/latin-500.css";
import "@fontsource/ibm-plex-sans/latin-600.css";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/app.css";
import "./i18n";

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./App";
import { Root } from "./app/Root";
import { systemClock } from "./lib/clock";

const root = document.getElementById("root");
if (root === null) {
  throw new Error("index.html has no #root element");
}
createRoot(root).render(
  <StrictMode>
    <Root
      env={{
        origin: window.location.origin,
        search: window.location.search,
        clock: systemClock,
        browserZone: Intl.DateTimeFormat().resolvedOptions().timeZone,
        leave: (path) => {
          window.location.assign(path);
        },
      }}
    >
      <App />
    </Root>
  </StrictMode>,
);
