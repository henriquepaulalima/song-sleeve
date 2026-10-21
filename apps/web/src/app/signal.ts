type Listener = () => void;

/** A tiny observable value for app-level flags. */
export class Signal<T> {
  private listeners = new Set<Listener>();
  constructor(private value: T) {}
  get = (): T => this.value;
  set(v: T) {
    if (Object.is(v, this.value)) return;
    this.value = v;
    for (const l of this.listeners) l();
  }
  subscribe = (fn: Listener): (() => void) => {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  };
}
