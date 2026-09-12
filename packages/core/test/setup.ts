function createMemoryStorage(): Storage {
  // A same-origin frame still exposes jsdom's real in-memory Storage when
  // Node's broken global shadows the top-level one.
  const frame = document.createElement("iframe");
  document.documentElement.append(frame);
  const storage = frame.contentWindow!.localStorage;
  Object.setPrototypeOf(storage, Storage.prototype);
  frame.remove();
  return storage;
}

// Pure-logic suites use Vitest's default node environment, so there is no DOM
// to patch. Suites that exercise browser persistence opt into jsdom per file.
if (
  typeof window !== "undefined" &&
  typeof globalThis.localStorage?.clear !== "function"
) {
  const storage = createMemoryStorage();
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: storage,
  });
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: storage,
  });
}
