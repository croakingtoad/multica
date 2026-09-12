// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type {
  RuntimeHookDiscoveryPhase,
  RuntimeHookDiscoveryProgress,
} from "@multica/core/runtimes";
import enCommon from "../../../locales/en/common.json";
import enRuntimes from "../../../locales/en/runtimes.json";
import { HookDiscoveryCard } from "./hook-discovery-card";

// Two claims this card makes about itself, both of which were false once:
// that the bound it prints is the server's (R1), and that a screen reader is
// told when the phase moves (R4).

function mount(progress: RuntimeHookDiscoveryProgress) {
  return render(
    <I18nProvider
      locale="en"
      resources={{ en: { common: enCommon, runtimes: enRuntimes } }}
    >
      <HookDiscoveryCard runtimeName="claude (daemon-1)" progress={progress} />
    </I18nProvider>,
  );
}

afterEach(() => {
  vi.useRealTimers();
});

describe("HookDiscoveryCard", () => {
  // R1. The numbers are 19 and 47 rather than 30 and 60 so a card that
  // reached for a constant — the server's or its own 100 s backstop — cannot
  // pass. The bound is per phase, so both phases are checked.
  it("prints the bound the server reported for the current phase", () => {
    const { unmount } = mount({ phase: "queued", phaseTimeoutSeconds: 19 });
    expect(
      screen.getByText(/19 s allowed for this step/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/100 s/)).not.toBeInTheDocument();
    unmount();

    mount({ phase: "reading", phaseTimeoutSeconds: 47 });
    expect(
      screen.getByText(/47 s allowed for this step/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/19 s/)).not.toBeInTheDocument();
  });

  // No bound reported — before the first answer, or from a backend without
  // the field — means no number, not a plausible-looking one.
  it("prints no bound at all when the server reported none", () => {
    mount({ phase: "initiating", phaseTimeoutSeconds: null });
    expect(screen.queryByText(/allowed for this step/)).not.toBeInTheDocument();
    expect(screen.getByText(/Every 500 ms/)).toBeInTheDocument();
  });

  // R4. The previous card put role="status" on the phase <ol>, whose text
  // never changes: the region announced the three steps once at mount and
  // said nothing as the phase advanced. The announcement must differ per
  // phase, and must not carry the ticking seconds.
  it("announces the phase it is on, and a different one per phase", () => {
    const announced: string[] = [];
    const phases: RuntimeHookDiscoveryPhase[] = [
      "initiating",
      "queued",
      "reading",
    ];
    for (const phase of phases) {
      const { unmount } = mount({ phase, phaseTimeoutSeconds: 19 });
      announced.push(screen.getByRole("status").textContent ?? "");
      unmount();
    }

    expect(new Set(announced).size).toBe(phases.length);
    expect(announced[0]).toBe("Queueing the read");
    expect(announced[1]).toBe("Waiting for the daemon");
    expect(announced[2]).toBe("Reading the host");
  });

  it("keeps the phase list a list and keeps the stopwatch out of the region", () => {
    vi.useFakeTimers();
    mount({ phase: "queued", phaseTimeoutSeconds: 19 });

    // The list is still a list: role="status" on it would have replaced that.
    expect(screen.getByRole("list")).toBeInTheDocument();

    const before = screen.getByRole("status").textContent;
    act(() => {
      vi.advanceTimersByTime(4000);
    });

    // Positive control: the counter really did move, so the region holding
    // still is a fact about the region rather than about a frozen clock.
    expect(screen.getByText(/4 s elapsed/)).toBeInTheDocument();
    expect(screen.getByRole("status").textContent).toBe(before);
    expect(screen.getByRole("status").textContent).not.toMatch(/\d/);
  });
});
