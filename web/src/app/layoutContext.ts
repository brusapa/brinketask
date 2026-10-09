// What the layout offers to the screens inside it: opening the sidebar
// drawer on narrow screens.
import { createContext, useContext } from "react";

export interface LayoutControls {
  openDrawer: () => void;
}

export const LayoutContext = createContext<LayoutControls>({ openDrawer: () => undefined });

export function useLayout(): LayoutControls {
  return useContext(LayoutContext);
}
