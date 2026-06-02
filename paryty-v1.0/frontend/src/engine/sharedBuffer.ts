// SharedArrayBuffer integration for zero-copy data transfer
// Uses Atomics for cross-thread synchronization

export interface SharedRingBufferConfig {
  capacity: number; // Number of slots
  slotSize: number; // Bytes per slot
}

export interface SharedRingBuffer {
  buffer: SharedArrayBuffer;
  data: Int32Array; // Using Int32Array for Atomics compatibility (values scaled to int)
  metadata: Int32Array; // [writeIndex, readIndex, count, lock]
}

// Layout constants for metadata
const META_WRITE_IDX = 0;
const META_READ_IDX = 1;
const META_COUNT = 2;
const META_LOCK = 3;
const META_SIZE = 4; // 4 int32 values

/**
 * Creates a shared ring buffer for cross-thread metric data transfer.
 */
export function createSharedRingBuffer(config: SharedRingBufferConfig): SharedRingBuffer {
  const { capacity, slotSize } = config;
  const totalSlots = capacity;
  const dataBytes = totalSlots * slotSize * Float64Array.BYTES_PER_ELEMENT;
  const metaBytes = META_SIZE * Int32Array.BYTES_PER_ELEMENT;

  // Align to 8 bytes
  const alignedMetaBytes = Math.ceil(metaBytes / 8) * 8;
  const totalBytes = dataBytes + alignedMetaBytes;

  const buffer = new SharedArrayBuffer(totalBytes);
  const data = new Int32Array(buffer, 0, totalSlots * slotSize);
  const metadata = new Int32Array(buffer, dataBytes, META_SIZE);

  // Initialize
  Atomics.store(metadata, META_WRITE_IDX, 0);
  Atomics.store(metadata, META_READ_IDX, 0);
  Atomics.store(metadata, META_COUNT, 0);
  Atomics.store(metadata, META_LOCK, 0);

  return { buffer, data, metadata };
}

/**
 * Writes data to the ring buffer (producer side).
 * Returns true if successful, false if buffer is full.
 */
export function writeToRingBuffer(
  rb: SharedRingBuffer,
  values: number[],
  slotSize: number,
): boolean {
  const { data, metadata } = rb;
  const capacity = data.length / slotSize;

  acquireLock(metadata);

  try {
    const count = Atomics.load(metadata, META_COUNT);
    if (count >= capacity) {
      return false;
    }

    const writeIdx = Atomics.load(metadata, META_WRITE_IDX);
    const offset = writeIdx * slotSize;

    // Write data (scale float to int: multiply by 1000 for 3 decimal precision)
    for (let i = 0; i < Math.min(values.length, slotSize); i++) {
      Atomics.store(data, offset + i, Math.round(values[i] * 1000));
    }

    // Update metadata
    Atomics.store(metadata, META_WRITE_IDX, (writeIdx + 1) % capacity);
    Atomics.add(metadata, META_COUNT, 1);

    return true;
  } finally {
    releaseLock(metadata);
  }
}

/**
 * Reads data from the ring buffer (consumer side).
 * Returns the data or null if buffer is empty.
 */
export function readFromRingBuffer(
  rb: SharedRingBuffer,
  slotSize: number,
): number[] | null {
  const { data, metadata } = rb;

  acquireLock(metadata);

  try {
    const count = Atomics.load(metadata, META_COUNT);
    if (count <= 0) {
      return null;
    }

    const readIdx = Atomics.load(metadata, META_READ_IDX);
    const offset = readIdx * slotSize;

    // Read data (scale back to float: divide by 1000)
    const result: number[] = new Array(slotSize);
    for (let i = 0; i < slotSize; i++) {
      result[i] = Atomics.load(data, offset + i) / 1000;
    }

    // Update metadata
    Atomics.store(metadata, META_READ_IDX, (readIdx + 1) % (data.length / slotSize));
    Atomics.sub(metadata, META_COUNT, 1);

    return result;
  } finally {
    releaseLock(metadata);
  }
}

/**
 * Gets the current count of items in the ring buffer.
 */
export function ringBufferCount(metadata: Int32Array): number {
  return Atomics.load(metadata, META_COUNT);
}

/**
 * Spinlock acquisition using Atomics.wait.
 */
function acquireLock(metadata: Int32Array): void {
  while (true) {
    const prev = Atomics.compareExchange(metadata, META_LOCK, 0, 1);
    if (prev === 0) return; // Lock acquired
    // Wait for lock to be released
    Atomics.wait(metadata, META_LOCK, 1, 10);
  }
}

/**
 * Releases the spinlock.
 */
function releaseLock(metadata: Int32Array): void {
  Atomics.store(metadata, META_LOCK, 0);
  Atomics.notify(metadata, META_LOCK, 1);
}
