// The header of a screen (DESIGN.md section 2): menu button on narrow
// screens, title, secondary info and the search field.
import { Menu as MenuIcon, Search } from "lucide-react";
import { useState } from "react";
import { Button, Input, SearchField } from "react-aria-components";
import { useTranslation } from "react-i18next";
import { useLocation, useNavigate, useSearchParams } from "react-router";

import { useLayout } from "../app/layoutContext";

export function ViewHeader({ title, info }: { title: string; info?: string | undefined }) {
  const { t } = useTranslation();
  const { openDrawer } = useLayout();
  return (
    <header className="view-header">
      <Button
        className="icon-button drawer-button"
        aria-label={t("nav.openMenu")}
        onPress={openDrawer}
      >
        <MenuIcon size={16} strokeWidth={1.5} aria-hidden="true" />
      </Button>
      <h1 className="view-title">{title}</h1>
      {info !== undefined && <span className="view-info">{info}</span>}
      <SearchBox />
    </header>
  );
}

/** Searching navigates to /search?q=…, so the results have their own URL. */
function SearchBox() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const location = useLocation();
  const [value, setValue] = useState(params.get("q") ?? "");
  return (
    <SearchField
      className="search-field"
      aria-label={t("search.label")}
      value={value}
      onChange={(text) => {
        setValue(text);
        // Results follow the typing; replace keeps one history entry.
        void navigate(text.trim() === "" ? "/search" : `/search?q=${encodeURIComponent(text)}`, {
          replace: true,
        });
      }}
      onClear={() => {
        setValue("");
      }}
      // The first key typed in another screen opens the search screen,
      // whose header is a new element: focus moves to its field so typing
      // goes on there.
      autoFocus={location.pathname === "/search"}
    >
      <Search size={16} strokeWidth={1.5} aria-hidden="true" className="search-icon" />
      <Input className="search-input" placeholder={t("search.placeholder")} />
    </SearchField>
  );
}
