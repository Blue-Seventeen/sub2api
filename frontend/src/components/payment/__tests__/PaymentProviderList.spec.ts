import { describe, expect, it, vi } from "vitest";
import { mount } from "@vue/test-utils";

import PaymentProviderList from "../PaymentProviderList.vue";

vi.mock("vue-i18n", () => ({
  useI18n: () => ({ t: (key: string) => key }),
}));

describe("PaymentProviderList load failure", () => {
  it("does not present a failed provider read as an empty list or allow creation", () => {
    const wrapper = mount(PaymentProviderList, {
      props: {
        providers: [],
        loading: false,
        loadError: true,
        canCreate: true,
        enabledPaymentTypes: ["alipay"],
        allPaymentTypes: [{ value: "alipay", label: "Alipay" }],
        redirectLabel: "redirect",
      },
      global: {
        stubs: {
          Icon: true,
          VueDraggable: true,
        },
      },
    });

    expect(wrapper.text()).toContain("admin.settings.payment.providersLoadFailed");
    expect(wrapper.text()).not.toContain("admin.settings.payment.noProviders");
    expect(wrapper.findAll("button").some((button) =>
      button.text().includes("admin.settings.payment.createProvider") &&
      button.attributes("disabled") === undefined,
    )).toBe(false);
    wrapper.unmount();
  });
});
