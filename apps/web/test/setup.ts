import "@testing-library/jest-dom/vitest";

type StorageName = "localStorage" | "sessionStorage";

// Keep this installer aligned with packages/core/test/setup.ts,
// packages/views/test/setup.ts, and apps/desktop/test/setup.ts; all four change together.
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

// Everything below patches gaps in jsdom. Pure-logic suites opt out of jsdom
// with `// @vitest-environment node` and share this file, so there is no DOM to
// patch there — bail out rather than guard each stub.
if (typeof window !== "undefined") {
  // jsdom doesn't provide ResizeObserver; stub it so components that rely on it
  // (e.g. input-otp) can render in tests.
  if (typeof globalThis.ResizeObserver === "undefined") {
    globalThis.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as unknown as typeof ResizeObserver;
  }

  // jsdom doesn't implement elementFromPoint; input-otp uses it internally.
  if (typeof document.elementFromPoint !== "function") {
    document.elementFromPoint = () => null;
  }

  installMemoryStorage("localStorage");
  installMemoryStorage("sessionStorage");
}
