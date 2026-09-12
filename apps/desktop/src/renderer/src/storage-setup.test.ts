import { describe, expect, it } from "vitest";

describe("test storage setup", () => {
  it.each(["localStorage", "sessionStorage"] as const)(
    "provides a genuine %s",
    (name) => {
      const storage = globalThis[name];

      expect(storage).toBeInstanceOf(Storage);
      expect(Object.hasOwn(storage, "setItem")).toBe(false);
    },
  );
});
