// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { useCommentComposerStore } from "./comment-composer-store";

describe("comment composer store", () => {
  beforeEach(() => {
    useCommentComposerStore.setState({ sticky: true });
  });

  it("toggleSticky flips the preference", () => {
    useCommentComposerStore.getState().toggleSticky();
    expect(useCommentComposerStore.getState().sticky).toBe(false);

    useCommentComposerStore.getState().toggleSticky();
    expect(useCommentComposerStore.getState().sticky).toBe(true);
  });
});
