type StorageName = "localStorage" | "sessionStorage";

// Keep this installer aligned with packages/views/test/setup.ts,
// apps/web/test/setup.ts, and apps/desktop/test/setup.ts; all four change together.
function createMemoryStorage(name: StorageName): Storage {
  // A same-origin frame still exposes jsdom's real in-memory Storage when
  // Node's broken global shadows the top-level one. That requires jsdom to be
  // on a tuple origin: an opaque origin such as about:blank makes the frame's
  // Web Storage access throw and aborts this import.
  const frame = document.createElement("iframe");
  document.documentElement.append(frame);
  const storage = frame.contentWindow![name];
  // The frame has its own Storage.prototype, so use the top realm's prototype
  // for instanceof checks and Storage.prototype spies.
  Object.setPrototypeOf(storage, Storage.prototype);
  frame.remove();
  return storage;
}

function installMemoryStorage(name: StorageName) {
  if (globalThis[name] instanceof Storage) {
    return;
  }

  const storage = createMemoryStorage(name);
  Object.defineProperty(globalThis, name, {
    configurable: true,
    value: storage,
  });
  Object.defineProperty(window, name, {
    configurable: true,
    value: storage,
  });
}

// Pure-logic suites use Vitest's default node environment, so there is no DOM
// to patch. Suites that exercise browser persistence opt into jsdom per file.
if (typeof window !== "undefined") {
  installMemoryStorage("localStorage");
  installMemoryStorage("sessionStorage");
}
