// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import {
  DEFAULT_SUB_ISSUE_ROW_PROPERTIES,
  useSubIssueDisplayStore,
} from "./sub-issue-display-store";

describe("sub-issue display store", () => {
  beforeEach(() => {
    useSubIssueDisplayStore.setState({
      rowProperties: { ...DEFAULT_SUB_ISSUE_ROW_PROPERTIES },
      rowPropertyIds: [],
    });
  });

  it("toggleRowProperty flips a single field without touching the rest", () => {
    useSubIssueDisplayStore.getState().toggleRowProperty("dueDate");
    expect(useSubIssueDisplayStore.getState().rowProperties).toEqual({
      ...DEFAULT_SUB_ISSUE_ROW_PROPERTIES,
      dueDate: false,
    });

    useSubIssueDisplayStore.getState().toggleRowProperty("dueDate");
    expect(useSubIssueDisplayStore.getState().rowProperties).toEqual(
      DEFAULT_SUB_ISSUE_ROW_PROPERTIES,
    );
  });

  it("toggleRowPropertyId adds then removes a custom property id", () => {
    useSubIssueDisplayStore.getState().toggleRowPropertyId("prop-1");
    useSubIssueDisplayStore.getState().toggleRowPropertyId("prop-2");
    expect(useSubIssueDisplayStore.getState().rowPropertyIds).toEqual([
      "prop-1",
      "prop-2",
    ]);

    useSubIssueDisplayStore.getState().toggleRowPropertyId("prop-1");
    expect(useSubIssueDisplayStore.getState().rowPropertyIds).toEqual(["prop-2"]);
  });
});
