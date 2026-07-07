import type { AdminGroup } from "@/types";

export interface PeakRateWindowForm {
  start: string;
  end: string;
  multiplier: number | string;
}

export interface PeakRateWindowPayload {
  start: string;
  end: string;
  multiplier: number;
}

export interface PeakRateFormState {
  peak_rate_enabled: boolean;
  peak_start: string;
  peak_end: string;
  peak_rate_multiplier: number | string;
  peak_rate_windows: PeakRateWindowForm[];
}

export type PeakRateErrorKey =
  | "required"
  | "maxWindows"
  | "multiplier"
  | "timeFormat"
  | "range"
  | "overlap";

type PeakRateErrorHandler = (key: PeakRateErrorKey) => void;

export const createEmptyPeakRateWindow = (): PeakRateWindowForm => ({
  start: "",
  end: "",
  multiplier: 1,
});

export const appendPeakRateWindow = (windows: PeakRateWindowForm[]): boolean => {
  if (windows.length >= 24) return false;
  windows.push(createEmptyPeakRateWindow());
  return true;
};

export const removePeakRateWindow = (
  windows: PeakRateWindowForm[],
  index: number,
) => {
  windows.splice(index, 1);
};

export const normalizeRateMultiplier = (value: number | string | null | undefined) => {
  if (value === null || value === undefined || value === "") {
    return 1;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : 1;
};

const peakWindowMinutes = (value: string): number | null => {
  const match = /^([01]\d|2[0-3]):([0-5]\d)$/.exec(value);
  if (!match) return null;
  const hour = Number(match[1]);
  const minute = Number(match[2]);
  if (!Number.isInteger(hour) || hour < 0 || hour > 23) return null;
  return hour * 60 + minute;
};

const parsePeakRateMultiplier = (
  value: number | string | null | undefined,
): number | null => {
  if (value === null || value === undefined || value === "") {
    return 1;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
};

export const normalizePeakRateWindowsForSubmit = (
  form: PeakRateFormState,
  onError?: PeakRateErrorHandler,
): PeakRateWindowPayload[] | null => {
  if (!form.peak_rate_enabled) {
    return [];
  }
  const windows = form.peak_rate_windows
    .map((window) => {
      const multiplier = parsePeakRateMultiplier(window.multiplier);
      return {
        start: String(window.start || "").trim(),
        end: String(window.end || "").trim(),
        multiplier,
      };
    })
    .filter((window) => window.start || window.end);

  if (windows.length === 0) {
    onError?.("required");
    return null;
  }
  if (windows.length > 24) {
    onError?.("maxWindows");
    return null;
  }

  const withMinutes = windows.map((window) => {
    const startMinutes = peakWindowMinutes(window.start);
    const endMinutes = peakWindowMinutes(window.end);
    return { ...window, startMinutes, endMinutes };
  });

  for (const window of withMinutes) {
    if (window.multiplier === null) {
      onError?.("multiplier");
      return null;
    }
    if (window.startMinutes === null || window.endMinutes === null) {
      onError?.("timeFormat");
      return null;
    }
    if (window.startMinutes >= window.endMinutes) {
      onError?.("range");
      return null;
    }
  }

  withMinutes.sort((a, b) => (a.startMinutes ?? 0) - (b.startMinutes ?? 0));
  for (let i = 1; i < withMinutes.length; i++) {
    if ((withMinutes[i].startMinutes ?? 0) < (withMinutes[i - 1].endMinutes ?? 0)) {
      onError?.("overlap");
      return null;
    }
  }

  return withMinutes.map(({ start, end, multiplier }) => ({
    start,
    end,
    multiplier: multiplier ?? 1,
  }));
};

export const syncPeakRateLegacyFields = (
  form: PeakRateFormState,
  windows: PeakRateWindowPayload[],
) => {
  if (!form.peak_rate_enabled || windows.length === 0) {
    form.peak_rate_enabled = false;
    form.peak_start = "";
    form.peak_end = "";
    form.peak_rate_multiplier = 1;
    form.peak_rate_windows = [];
    return;
  }
  form.peak_rate_windows = windows;
  form.peak_start = windows[0].start;
  form.peak_end = windows[0].end;
  form.peak_rate_multiplier = normalizeRateMultiplier(windows[0].multiplier);
};

export const buildPeakRatePayload = (
  form: PeakRateFormState,
  onError?: PeakRateErrorHandler,
) => {
  const windows = normalizePeakRateWindowsForSubmit(form, onError);
  if (windows === null) return null;
  syncPeakRateLegacyFields(form, windows);
  return {
    peak_rate_enabled: form.peak_rate_enabled,
    peak_start: form.peak_start,
    peak_end: form.peak_end,
    peak_rate_multiplier: normalizeRateMultiplier(form.peak_rate_multiplier),
    peak_rate_windows: windows,
  };
};

export const peakRateWindowsFromGroup = (group: AdminGroup): PeakRateWindowForm[] => {
  if (Array.isArray(group.peak_rate_windows) && group.peak_rate_windows.length > 0) {
    return group.peak_rate_windows.map((window) => ({
      start: window.start,
      end: window.end,
      multiplier: window.multiplier ?? 1,
    }));
  }
  if (group.peak_start && group.peak_end) {
    return [{
      start: group.peak_start,
      end: group.peak_end,
      multiplier: group.peak_rate_multiplier ?? 1,
    }];
  }
  return [];
};
