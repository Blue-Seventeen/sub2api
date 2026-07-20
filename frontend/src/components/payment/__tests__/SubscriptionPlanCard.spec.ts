import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import { createPinia } from "pinia";
import type { SubscriptionPlan } from "@/types/payment";
import SubscriptionPlanCard from "../SubscriptionPlanCard.vue";

vi.mock("vue-i18n", async () => {
  const actual = await vi.importActual<typeof import("vue-i18n")>("vue-i18n");
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === "payment.days") return "days";
        if (key === "payment.planCard.peakRate") return "Peak rate";
        if (key === "payment.planCard.quota") return "Quota";
        if (key === "payment.planCard.rate") return "Rate";
        if (key === "payment.planCard.unlimited") return "Unlimited";
        if (key === "payment.subscribeNow") return "Subscribe now";
        if (key === "common.peakRateCompactSingle") return `Peak x${params?.multiplier}`;
        if (key === "common.peakRateCompactMultiple") return `Peak ${params?.count} windows`;
        return key;
      },
    }),
  };
});

const mountPlanCard = (groupPlatform: string, overrides: Partial<SubscriptionPlan> = {}) =>
  mount(SubscriptionPlanCard, {
    props: {
      plan: {
        id: 1,
        group_id: 10,
        group_platform: groupPlatform,
        name: "Pro",
        price: 10,
        amount: 1000,
        features: [],
        rate_multiplier: 1,
        validity_days: 30,
        validity_unit: "day",
        supported_model_scopes: ["claude", "gemini_text", "gemini_image"],
        is_active: true,
        ...overrides,
      },
    },
    global: { plugins: [createPinia()] },
  });

describe("SubscriptionPlanCard", () => {
  it("does not show Antigravity model scopes for OpenAI plans", () => {
    const text = mountPlanCard("openai").text();

    expect(text).not.toContain("Claude");
    expect(text).not.toContain("Gemini");
    expect(text).not.toContain("Imagen");
  });

  it("shows model scopes for Antigravity plans", () => {
    const text = mountPlanCard("antigravity").text();

    expect(text).toContain("Claude");
    expect(text).toContain("Gemini");
    expect(text).toContain("Imagen");
  });

  it("shows a compact peak-rate summary for multiple windows", () => {
    const wrapper = mountPlanCard("openai", {
      peak_rate_enabled: true,
      peak_rate_windows: [
        { start: "09:00", end: "12:00", multiplier: 1.5 },
        { start: "18:00", end: "22:00", multiplier: 2 },
      ],
    });

    expect(wrapper.text()).toContain("Peak 2 windows");
    expect(wrapper.text()).not.toContain("09:00-12:00");
    expect(wrapper.find("[title*='09:00-12:00']").exists()).toBe(true);
    expect(wrapper.find("[title*='18:00-22:00']").exists()).toBe(true);
  });

  it("uses the configured currency symbol while preserving USD for legacy plans", () => {
    const cnyPlan = mountPlanCard("openai", { currency: "CNY", original_price: 20 }).text();

    expect(cnyPlan).toContain("\u00a510.00CNY");
    expect(cnyPlan).toContain("\u00a520.00CNY");
    expect(mountPlanCard("openai", { currency: "USD" }).text()).toContain("$10.00USD");
    expect(mountPlanCard("openai", { currency: "" }).text()).toContain("$10.00");
  });
});
