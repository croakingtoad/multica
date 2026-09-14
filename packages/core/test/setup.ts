type StorageName = "localStorage" | "sessionStorage";

function createMemoryStorage(name: StorageName): Storage {
  // A same-origin frame still exposes jsdom's real in-memory Storage when
  // Node's broken global shadows the top-level one.
  const frame = document.createElement("iframe");
  document.documentElement.append(frame);
  const storage = frame.contentWindow![name];
  Object.setPrototypeOf(storage, Storage.prototype);
  frame.remove();
  return storage;
}

function installMemoryStorage(name: StorageName) {
  if (window[name] instanceof Storage) {
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
