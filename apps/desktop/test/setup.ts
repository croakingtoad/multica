import "@testing-library/jest-dom/vitest";

type StorageName = "localStorage" | "sessionStorage";

// Keep this installer aligned with packages/core/test/setup.ts,
// packages/views/test/setup.ts, and apps/web/test/setup.ts; all four change together.
function createMemoryStorage(name: StorageName): Storage {
  // This setup requires jsdom to use a tuple origin. An opaque origin such as
  // about:blank makes the frame's Web Storage access throw and aborts import.
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
  installMemoryStorage("localStorage");
  installMemoryStorage("sessionStorage");

  // jsdom doesn't provide matchMedia; the sidebar's compact breakpoint and
  // auto-collapse band both read it. Nothing matches, so a shell mounted here
  // renders at its full desktop width. Mirrors packages/views/test/setup.ts.
  if (typeof window.matchMedia !== "function") {
    window.matchMedia = (query: string) =>
      ({
        matches: false,
        media: query,
        onchange: null,
        addListener: () => {},
        removeListener: () => {},
        addEventListener: () => {},
        removeEventListener: () => {},
        dispatchEvent: () => false,
      }) as MediaQueryList;
  }
}
