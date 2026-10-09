// The injected clock (D-17). Code that needs "now" asks the clock, so tests
// can fix the time; nothing in the client calls Date.now() directly except
// the system clock below.
export interface Clock {
  now(): Date;
}

export const systemClock: Clock = {
  now: () => new Date(),
};

/** A clock that only moves when told to; for tests. */
export class FixedClock implements Clock {
  private current: Date;

  constructor(start: Date | string) {
    this.current = new Date(start);
  }

  now(): Date {
    return new Date(this.current);
  }

  set(at: Date | string): void {
    this.current = new Date(at);
  }

  advance(milliseconds: number): void {
    this.current = new Date(this.current.getTime() + milliseconds);
  }
}
