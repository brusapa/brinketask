// Transient messages at the bottom of the screen, such as "Task deleted —
// Undo" (SPEC section 9). A tiny store: components show toasts through it
// and <Toasts> renders the current ones.

export interface Toast {
  id: number;
  message: string;
  action?: { label: string; run: () => void } | undefined;
  /** Errors stay until dismissed; others disappear on their own. */
  tone: "info" | "error";
}

/** How long a toast stays, in milliseconds. */
export const toastDuration = 6000;

export class ToastStore {
  private toasts: readonly Toast[] = [];
  private listeners = new Set<() => void>();
  private nextId = 1;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  getSnapshot = (): readonly Toast[] => this.toasts;

  show(toast: Omit<Toast, "id">): number {
    const id = this.nextId++;
    // One toast at a time keeps the undo action unambiguous.
    this.toasts = [{ ...toast, id }];
    this.emit();
    if (toast.tone === "info") {
      setTimeout(() => {
        this.dismiss(id);
      }, toastDuration);
    }
    return id;
  }

  dismiss(id: number): void {
    if (this.toasts.some((t) => t.id === id)) {
      this.toasts = this.toasts.filter((t) => t.id !== id);
      this.emit();
    }
  }

  private emit(): void {
    for (const listener of this.listeners) listener();
  }
}
