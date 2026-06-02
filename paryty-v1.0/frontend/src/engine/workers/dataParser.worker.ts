// Data parsing Web Worker
// Handles JSON and protocol buffer parsing off the main thread

interface ParseRequest {
  type: 'json' | 'protobuf';
  id: string;
  data: string | ArrayBuffer;
}

self.onmessage = (event: MessageEvent<ParseRequest>) => {
  const { type, id, data } = event.data;

  try {
    switch (type) {
      case 'json': {
        const parsed = JSON.parse(data as string);
        self.postMessage({ type: 'result', id, data: parsed });
        break;
      }
      case 'protobuf': {
        // Placeholder: would use protobuf-es decode here
        // For now, return the raw buffer info
        self.postMessage({
          type: 'result',
          id,
          data: { raw: data, length: (data as ArrayBuffer).byteLength },
        });
        break;
      }
      default:
        self.postMessage({ type: 'error', id, data: `Unknown parse type: ${type}` });
    }
  } catch (err) {
    self.postMessage({ type: 'error', id, data: (err as Error).message });
  }
};
