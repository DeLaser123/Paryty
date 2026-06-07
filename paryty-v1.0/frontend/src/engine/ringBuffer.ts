/**
 * Ring Buffer — Fixed-Capacity Circular Buffer (AAA Game Engine Pattern)
 *
 * Zero-allocation after construction. Supports push and snapshot-to-array.
 * Ideal for time-series data where older values naturally expire.
 *
 * Memory: capacity × sizeof(T) allocated once at construction.
 *
 * Usage:
 * ```ts
 * const buf = new RingBuffer<number>(1000);
 * buf.push(42);
 * buf.push(99);
 * const snapshot = buf.toArray(); // [42, 99]
 * ```
 */

export class RingBuffer<T> {
  private buffer: (T | undefined)[];
  private head = 0;
  private _size = 0;
  readonly capacity: number;

  constructor(capacity: number) {
    this.capacity = Math.max(1, capacity);
    this.buffer = new Array<T | undefined>(this.capacity);
  }

  /** Number of elements currently in the buffer. */
  get size(): number {
    return this._size;
  }

  /**
   * Appends a value to the buffer. If the buffer is full,
   * the oldest value is silently overwritten.
   */
  push(value: T): void {
    this.buffer[this.head] = value;
    this.head = (this.head + 1) % this.capacity;
    if (this._size < this.capacity) {
      this._size++;
    }
  }

  /**
   * Appends multiple values. Equivalent to calling push() for each.
   */
  pushMany(values: T[]): void {
    for (let i = 0; i < values.length; i++) {
      this.push(values[i]);
    }
  }

  /**
   * Returns a snapshot of the buffer contents in insertion order (oldest first).
   * This IS an allocation — use sparingly (e.g., when serializing for render).
   */
  toArray(): T[] {
    if (this._size === 0) return [];

    const result: T[] = new Array(this._size);
    if (this._size < this.capacity) {
      // Buffer hasn't wrapped — straightforward 0..size
      for (let i = 0; i < this._size; i++) {
        result[i] = this.buffer[i] as T;
      }
    } else {
      // Buffer has wrapped — head is the next write position
      const start = this.head; // oldest element
      for (let i = 0; i < this._size; i++) {
        result[i] = this.buffer[(start + i) % this.capacity] as T;
      }
    }
    return result;
  }

  /** Returns the most recently pushed element, or undefined if empty. */
  peek(): T | undefined {
    if (this._size === 0) return undefined;
    const idx = this._size < this.capacity
      ? this.head - 1
      : (this.head - 1 + this.capacity) % this.capacity;
    return this.buffer[idx] as T;
  }

  /** Clears the buffer without deallocating the backing array. */
  clear(): void {
    this.head = 0;
    this._size = 0;
  }
}
