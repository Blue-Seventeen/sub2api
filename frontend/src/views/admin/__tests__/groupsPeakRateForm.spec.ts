import { describe, expect, it, vi } from "vitest";

import {
  appendPeakRateWindow,
  buildPeakRatePayload,
  peakRateWindowsFromGroup,
  type PeakRateFormState,
} from "../groupsPeakRateForm";
import type { AdminGroup } from "@/types";

const createForm = (overrides: Partial<PeakRateFormState> = {}): PeakRateFormState => ({
  peak_rate_enabled: true,
  peak_start: "",
  peak_end: "",
  peak_rate_multiplier: 1,
  peak_rate_windows: [
    { start: "18:00", end: "22:00", multiplier: 2 },
    { start: "09:00", end: "12:00", multiplier: 1.5 },
  ],
  ...overrides,
});

describe("groupsPeakRateForm", () => {
  it("builds create/update payload with sorted windows and synced legacy first-window fields", () => {
    const form = createForm();

    const payload = buildPeakRatePayload(form);

    expect(payload).toEqual({
      peak_rate_enabled: true,
      peak_start: "09:00",
      peak_end: "12:00",
      peak_rate_multiplier: 1.5,
      peak_rate_windows: [
        { start: "09:00", end: "12:00", multiplier: 1.5 },
        { start: "18:00", end: "22:00", multiplier: 2 },
      ],
    });
    expect(form.peak_start).toBe("09:00");
    expect(form.peak_end).toBe("12:00");
    expect(form.peak_rate_multiplier).toBe(1.5);
  });

  it("preserves peak windows for standard balance groups instead of clearing them", () => {
    const form = {
      ...createForm(),
      subscription_type: "standard",
    };

    const payload = buildPeakRatePayload(form);

    expect(payload?.peak_rate_enabled).toBe(true);
    expect(payload?.peak_rate_windows).toHaveLength(2);
  });

  it("allows a zero multiplier and keeps it in the payload", () => {
    const form = createForm({
      peak_rate_windows: [{ start: "00:00", end: "01:00", multiplier: 0 }],
    });

    const payload = buildPeakRatePayload(form);

    expect(payload?.peak_rate_multiplier).toBe(0);
    expect(payload?.peak_rate_windows).toEqual([
      { start: "00:00", end: "01:00", multiplier: 0 },
    ]);
  });

  it("rejects overlapping windows", () => {
    const onError = vi.fn();
    const form = createForm({
      peak_rate_windows: [
        { start: "09:00", end: "12:00", multiplier: 1.5 },
        { start: "11:00", end: "13:00", multiplier: 2 },
      ],
    });

    expect(buildPeakRatePayload(form, onError)).toBeNull();
    expect(onError).toHaveBeenCalledWith("overlap");
  });

  it("rejects cross-day or equal-boundary windows", () => {
    const onError = vi.fn();
    const form = createForm({
      peak_rate_windows: [{ start: "23:00", end: "01:00", multiplier: 2 }],
    });

    expect(buildPeakRatePayload(form, onError)).toBeNull();
    expect(onError).toHaveBeenCalledWith("range");
  });

  it("rejects negative multipliers", () => {
    const onError = vi.fn();
    const form = createForm({
      peak_rate_windows: [{ start: "09:00", end: "12:00", multiplier: -1 }],
    });

    expect(buildPeakRatePayload(form, onError)).toBeNull();
    expect(onError).toHaveBeenCalledWith("multiplier");
  });

  it("clears windows and legacy fields when disabled", () => {
    const form = createForm({ peak_rate_enabled: false });

    expect(buildPeakRatePayload(form)).toEqual({
      peak_rate_enabled: false,
      peak_start: "",
      peak_end: "",
      peak_rate_multiplier: 1,
      peak_rate_windows: [],
    });
  });

  it("hydrates display windows from new multi-window fields before legacy fields", () => {
    const group = {
      peak_start: "08:00",
      peak_end: "09:00",
      peak_rate_multiplier: 9,
      peak_rate_windows: [{ start: "10:00", end: "11:00", multiplier: 1.2 }],
    } as AdminGroup;

    expect(peakRateWindowsFromGroup(group)).toEqual([
      { start: "10:00", end: "11:00", multiplier: 1.2 },
    ]);
  });

  it("uses legacy single-window fields when multi-window fields are empty", () => {
    const group = {
      peak_start: "08:00",
      peak_end: "09:00",
      peak_rate_multiplier: 1.8,
      peak_rate_windows: [],
    } as AdminGroup;

    expect(peakRateWindowsFromGroup(group)).toEqual([
      { start: "08:00", end: "09:00", multiplier: 1.8 },
    ]);
  });

  it("does not append more than twenty-four windows", () => {
    const windows = Array.from({ length: 24 }, () => ({
      start: "00:00",
      end: "01:00",
      multiplier: 1,
    }));

    expect(appendPeakRateWindow(windows)).toBe(false);
    expect(windows).toHaveLength(24);
  });
});
