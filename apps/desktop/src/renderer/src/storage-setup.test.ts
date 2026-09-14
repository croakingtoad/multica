import { describe, expect, it } from "vitest";

describe("test storage setup", () => {
  it.each(["localStorage", "sessionStorage"] as const)(
    "provides a genuine %s",
    (name) => {
      const storage = globalThis[name];
      const key = `storage-setup-${name}`;

      expect(storage).toBeInstanceOf(Storage);
      expect(Object.hasOwn(storage, "setItem")).toBe(false);

      storage.setItem(key, "stored-value");
      expect(storage.getItem(key)).toBe("stored-value");
      expect(storage.length).toBeGreaterThan(0);
      expect(storage.key(0)).toEqual(expect.any(String));
      storage.removeItem(key);
      expect(storage.getItem(key)).toBeNull();
      storage.clear();
      expect(storage.length).toBe(0);
    },
  );
});
