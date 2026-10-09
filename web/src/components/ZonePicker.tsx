// A searchable list of IANA time zones, from the browser's own list
// (Intl.supportedValuesOf), for the profile zone and fixed-time tasks.
import { useMemo } from "react";
import {
  Button,
  ComboBox,
  Input,
  Label,
  ListBox,
  ListBoxItem,
  Popover,
} from "react-aria-components";
import { ChevronDown } from "lucide-react";

export function zoneNames(): string[] {
  // Every browser the app supports has supportedValuesOf; UTC is not always
  // in its list, so it is added.
  const names = Intl.supportedValuesOf("timeZone");
  return names.includes("UTC") ? names : ["UTC", ...names];
}

export function ZonePicker({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (zone: string) => void;
}) {
  const items = useMemo(() => zoneNames().map((name) => ({ id: name, name })), []);
  return (
    <ComboBox
      className="field"
      items={items}
      value={value}
      onChange={(key) => {
        if (key !== null) onChange(String(key));
      }}
      // Typing filters by substring, so "madrid" finds Europe/Madrid.
      defaultFilter={(text, input) => text.toLowerCase().includes(input.toLowerCase())}
      menuTrigger="focus"
    >
      <Label>{label}</Label>
      <div className="combo">
        <Input className="input" />
        <Button className="icon-button combo-button">
          <ChevronDown size={16} strokeWidth={1.5} aria-hidden="true" />
        </Button>
      </div>
      <Popover className="popover combo-popover">
        <ListBox className="listbox">
          {(item: { id: string; name: string }) => (
            <ListBoxItem id={item.id} className="menu-item">
              {item.name.replaceAll("_", " ")}
            </ListBoxItem>
          )}
        </ListBox>
      </Popover>
    </ComboBox>
  );
}
