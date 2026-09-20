import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

import ModelListScopeDialog from "../ModelListScopeDialog.vue";

vi.mock("vue-i18n", () => ({
  useI18n: () => ({ t: (key: string) => key }),
}));

describe("ModelListScopeDialog", () => {
  it("emits group and global choices for an add operation", async () => {
    const wrapper = mount(ModelListScopeDialog, {
      props: { show: true, operation: "add" },
      global: { stubs: { BaseDialog: { template: "<div><slot /><slot name='footer' /></div>" } } },
    });

    const choices = wrapper.findAll("button");
    await choices[0].trigger("click");
    await choices[1].trigger("click");

    expect(wrapper.emitted("select")).toEqual([["group"], ["global"]]);
  });

  it("emits both scopes for delete operations too", async () => {
    const wrapper = mount(ModelListScopeDialog, {
      props: { show: true, operation: "remove", selectedCount: 2 },
      global: { stubs: { BaseDialog: { template: "<div><slot /><slot name='footer' /></div>" } } },
    });

    const choices = wrapper.findAll("button");
    await choices[0].trigger("click");
    await choices[1].trigger("click");

    expect(wrapper.emitted("select")).toEqual([["group"], ["global"]]);
  });

  it("describes the selected count for delete operations", () => {
    const wrapper = mount(ModelListScopeDialog, {
      props: { show: true, operation: "remove", selectedCount: 3 },
      global: { stubs: { BaseDialog: { template: "<div><slot /><slot name='footer' /></div>" } } },
    });

    expect(wrapper.text()).toContain("admin.groups.modelsList.deleteScopeDescription");
  });
});
