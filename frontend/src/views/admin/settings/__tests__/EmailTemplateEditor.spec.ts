import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";

import EmailTemplateEditor from "../EmailTemplateEditor.vue";

const { getEmailTemplates, getEmailTemplate, updateEmailTemplate, restoreOfficialEmailTemplate, previewEmailTemplate, showError, showSuccess } = vi.hoisted(() => ({
  getEmailTemplates: vi.fn(),
  getEmailTemplate: vi.fn(),
  updateEmailTemplate: vi.fn(),
  restoreOfficialEmailTemplate: vi.fn(),
  previewEmailTemplate: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}));

vi.mock("@/api", () => ({
  adminAPI: {
    settings: {
      getEmailTemplates,
      getEmailTemplate,
      updateEmailTemplate,
      previewEmailTemplate,
      restoreOfficialEmailTemplate,
    },
  },
}));

vi.mock("@/stores", () => ({
  useAppStore: () => ({ showError, showSuccess }),
}));

vi.mock("vue-i18n", () => ({
  useI18n: () => ({
    t: (key: string) => key,
    locale: { value: "en-US" },
  }),
}));

describe("admin EmailTemplateEditor save state", () => {
  beforeEach(() => {
    getEmailTemplates.mockReset().mockResolvedValue({
      events: [
        { value: "auth.verify_code", label: "Verification", category: "auth", optional: false },
        { value: "auth.password_reset", label: "Password reset", category: "auth", optional: false },
      ],
      locales: ["en"],
      templates: [],
      placeholders: [],
    });
    getEmailTemplate.mockReset().mockResolvedValue({
      subject: "Verification subject",
      html: "<p>Verification body</p>",
      is_custom: true,
      placeholders: [],
    });
    updateEmailTemplate.mockReset();
    restoreOfficialEmailTemplate.mockReset().mockResolvedValue({
      subject: "Official subject",
      html: "<p>Official body</p>",
      is_custom: false,
      placeholders: [],
    });
    previewEmailTemplate.mockReset().mockResolvedValue({
      subject: "Preview subject",
      html: "<p>Preview</p>",
    });
    showError.mockReset();
    showSuccess.mockReset();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  it("clears the previous template and blocks save when the newly selected template cannot load", async () => {
    getEmailTemplate
      .mockResolvedValueOnce({
        subject: "Verification subject",
        html: "<p>Verification body</p>",
        is_custom: true,
        placeholders: [],
      })
      .mockRejectedValueOnce(new Error("template unavailable"));

    const wrapper = mount(EmailTemplateEditor);
    await flushPromises();
    expect((wrapper.get("#email-template-subject").element as HTMLInputElement).value).toBe("Verification subject");

    await wrapper.get("#email-template-event").setValue("auth.password_reset");
    await flushPromises();

    expect((wrapper.get("#email-template-subject").element as HTMLInputElement).value).toBe("");
    expect((wrapper.get("#email-template-html").element as HTMLTextAreaElement).value).toBe("");
    expect(wrapper.get("button.btn-primary").attributes("disabled")).toBeDefined();
    expect(updateEmailTemplate).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("enables the restored current template after a template read failed", async () => {
    getEmailTemplate
      .mockResolvedValueOnce({
        subject: "Verification subject",
        html: "<p>Verification body</p>",
        is_custom: true,
        placeholders: [],
      })
      .mockRejectedValueOnce(new Error("template unavailable"));
    vi.spyOn(window, "confirm").mockReturnValue(true);

    const wrapper = mount(EmailTemplateEditor);
    await flushPromises();
    await wrapper.get("#email-template-event").setValue("auth.password_reset");
    await flushPromises();
    expect(wrapper.get("button.btn-primary").attributes("disabled")).toBeDefined();

    await wrapper.findAll("button").find((button) => button.text() === "admin.settings.emailTemplates.restoreOfficial")!.trigger("click");
    await flushPromises();

    expect(restoreOfficialEmailTemplate).toHaveBeenCalledWith("auth.password_reset", "en");
    expect((wrapper.get("#email-template-subject").element as HTMLInputElement).value).toBe("Official subject");
    expect(wrapper.get("#email-template-subject").attributes("disabled")).toBeUndefined();
    expect(wrapper.get("button.btn-primary").attributes("disabled")).toBeUndefined();
    wrapper.unmount();
  });
});
