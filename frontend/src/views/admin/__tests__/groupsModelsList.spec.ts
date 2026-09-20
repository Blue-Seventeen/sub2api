import { describe, expect, it } from "vitest";

import {
  addModelsListItem,
  buildModelsListConfig,
  buildModelsListPayload,
  commitModelsListItemEdit,
  createModelsListState,
  hydrateModelsListState,
  moveModelsListItem,
  removeModelsListItem,
  removeModelsListItemWithScope,
  removeSelectedModelsListItemsWithScope,
  removeSelectedModelsListItems,
  queueGlobalModelOperation,
  clearGlobalModelOperations,
  getGlobalModelOperations,
  setModelsListCandidates,
  startEditModelsListItem,
  toggleModelsListItem,
  type ModelsListItem,
} from "../groupsModelsList";

const simpleItems = (items: ModelsListItem[]) => items.map(item => ({
  id: item.id,
  selected: item.selected,
}))

describe("groupsModelsList", () => {
  it("selects all default candidates for a new disabled config", () => {
    const state = createModelsListState();

    setModelsListCandidates(state, ["gpt-5.5", "gpt-5.4"]);

    expect(state.enabled).toBe(false);
    expect(simpleItems(state.items)).toEqual([
      { id: "gpt-5.5", selected: true },
      { id: "gpt-5.4", selected: true },
    ]);
  });

  it("shows only saved whitelist entries when editing an enabled config", () => {
    const state = createModelsListState({
      enabled: true,
      models: ["gpt-5.5", "gpt-5.4"],
    });

    setModelsListCandidates(state, ["gpt-5.4", "legacy-gpt", "gpt-5.5"]);

    expect(state.enabled).toBe(true);
    expect(simpleItems(state.items)).toEqual([
      { id: "gpt-5.5", selected: true },
      { id: "gpt-5.4", selected: true },
    ]);
  });

  it("keeps an enabled empty saved list empty instead of adding candidates back", () => {
    const state = createModelsListState({
      enabled: true,
      models: [],
    });

    setModelsListCandidates(state, ["gpt-5.5", "gpt-5.4"]);

    expect(simpleItems(state.items)).toEqual([]);
    expect(buildModelsListConfig(state)).toEqual({
      enabled: true,
      models: [],
    });
  });

  it("does not re-add deleted default candidates when reopening an enabled config", () => {
    const state = createModelsListState({
      enabled: true,
      models: ["kimi-k2.6", "kimi-k2.5"],
    });

    setModelsListCandidates(state, [
      "kimi-k2.6",
      "kimi-k2.5",
      "kimi-k2-thinking",
      "kimi-k2-thinking-turbo",
    ]);

    expect(simpleItems(state.items)).toEqual([
      { id: "kimi-k2.6", selected: true },
      { id: "kimi-k2.5", selected: true },
    ]);
  });

  it("builds config with selected models in current display order", () => {
    const state = hydrateModelsListState({
      enabled: true,
      models: ["gpt-5.5", "gpt-5.4", "legacy-gpt"],
    }, ["gpt-5.5", "gpt-5.4", "legacy-gpt"]);

    toggleModelsListItem(state, "legacy-gpt");
    moveModelsListItem(state, 1, 0);

    expect(buildModelsListConfig(state)).toEqual({
      enabled: true,
      models: ["gpt-5.4", "gpt-5.5"],
    });
  });

  it("keeps selected models in payload even when disabled so reopening can restore choices", () => {
    const state = hydrateModelsListState({
      enabled: false,
      models: ["gpt-5.5"],
    }, ["gpt-5.5", "gpt-5.4"]);

    expect(buildModelsListConfig(state)).toEqual({
      enabled: false,
      models: ["gpt-5.5"],
    });
  });

  it("preserves saved models when candidates have not loaded yet", () => {
    const state = createModelsListState({
      enabled: true,
      models: ["gpt-5.5", "gpt-5.4"],
    });

    expect(buildModelsListConfig(state)).toEqual({
      enabled: true,
      models: ["gpt-5.5", "gpt-5.4"],
    });
  });

  it("can add and edit a custom model", () => {
    const state = hydrateModelsListState({
      enabled: true,
      models: ["gpt-5.5"],
    }, ["gpt-5.5"]);

    addModelsListItem(state);
    state.items[0].draft = " KIMI-* ";
    commitModelsListItemEdit(state, state.items[0]);

    expect(buildModelsListConfig(state)).toEqual({
      enabled: true,
      models: ["KIMI-*", "gpt-5.5"],
    });
  });

  it("dedupes edited models case-insensitively and keeps the first entry", () => {
    const state = hydrateModelsListState({
      enabled: true,
      models: ["kimi"],
    }, ["kimi"]);

    addModelsListItem(state);
    state.items[0].draft = "KIMI";
    commitModelsListItemEdit(state, state.items[0]);

    expect(buildModelsListConfig(state)).toEqual({
      enabled: true,
      models: ["kimi"],
    });
  });

  it("rolls back an existing item when editing it to a duplicate model", () => {
    const state = hydrateModelsListState({
      enabled: true,
      models: ["kimi", "gpt-5.5"],
    }, ["kimi", "gpt-5.5"]);

    startEditModelsListItem(state.items[1]);
    state.items[1].draft = "KIMI";
    commitModelsListItemEdit(state, state.items[1]);

    expect(buildModelsListConfig(state)).toEqual({
      enabled: true,
      models: ["kimi", "gpt-5.5"],
    });
  });

  it("can delete a single model and batch delete selected models", () => {
    const state = hydrateModelsListState({
      enabled: true,
      models: ["gpt-5.5", "gpt-5.4"],
    }, ["gpt-5.5", "gpt-5.4", "gpt-5.4-mini"]);

    removeModelsListItem(state, state.items[1]);
    removeSelectedModelsListItems(state);

    expect(buildModelsListConfig(state)).toEqual({
      enabled: true,
      models: [],
    });
  });

  it("commits an active edit when building config", () => {
    const state = hydrateModelsListState({
      enabled: true,
      models: ["gpt-5.5"],
    }, ["gpt-5.5"]);

    startEditModelsListItem(state.items[0]);
    state.items[0].draft = "Kimi";

    expect(buildModelsListConfig(state)).toEqual({
      enabled: true,
      models: ["Kimi"],
    });
  });

  it("queues global model operations with case-insensitive last-operation-wins semantics", () => {
    const state = createModelsListState({ enabled: true, models: ["gpt-5.4"] });

    queueGlobalModelOperation(state, "add", " GPT-5.5 ");
    queueGlobalModelOperation(state, "remove", "gpt-5.5");
    queueGlobalModelOperation(state, "add", "GPT-5.5");

    expect(getGlobalModelOperations(state)).toEqual([
      { operation: "add", model: "GPT-5.5" },
    ]);
  });

  it("does not queue blank global model operations", () => {
    const state = createModelsListState();

    queueGlobalModelOperation(state, "add", "   ");

    expect(getGlobalModelOperations(state)).toEqual([]);
  });

  it("clears pending global operations without changing local models", () => {
    const state = hydrateModelsListState(
      { enabled: true, models: ["gpt-5.4"] },
      ["gpt-5.4"],
    );
    queueGlobalModelOperation(state, "remove", "gpt-5.4");

    clearGlobalModelOperations(state);

    expect(getGlobalModelOperations(state)).toEqual([]);
    expect(buildModelsListConfig(state)).toEqual({
      enabled: true,
      models: ["gpt-5.4"],
    });
  });

  it("defaults newly added entries to group scope", () => {
    const state = createModelsListState();

    addModelsListItem(state);
    state.items[0].draft = "gpt-5.5";
    commitModelsListItemEdit(state, state.items[0]);

    expect(getGlobalModelOperations(state)).toEqual([]);
    expect(buildModelsListConfig(state).models).toEqual(["gpt-5.5"]);
  });

  it("queues a global add when a global draft is committed", () => {
    const state = createModelsListState({ enabled: false });

    addModelsListItem(state, "global");
    state.items[0].draft = " gpt-5.5 ";
    commitModelsListItemEdit(state, state.items[0]);

    expect(getGlobalModelOperations(state)).toEqual([
      { operation: "add", model: "gpt-5.5" },
    ]);
  });

  it("queues a global add even when the model already exists locally", () => {
    const state = hydrateModelsListState(
      { enabled: false, models: ["gpt-5.5"] },
      ["gpt-5.5"],
    );

    addModelsListItem(state, "global");
    state.items[0].draft = "GPT-5.5";
    commitModelsListItemEdit(state, state.items[0]);

    expect(getGlobalModelOperations(state)).toEqual([
      { operation: "add", model: "GPT-5.5" },
    ]);
    expect(buildModelsListConfig(state).models).toEqual(["gpt-5.5"]);
  });

  it("queues only selected exact entries for a global delete", () => {
    const state = hydrateModelsListState(
      { enabled: false, models: ["gpt-*", "gpt-5.5", "gpt-5.4"] },
      ["gpt-*", "gpt-5.5", "gpt-5.4"],
    );
    state.items.forEach((item) => {
      item.selected = item.id !== "gpt-5.4";
    });

    removeSelectedModelsListItemsWithScope(state, "global");

    expect(getGlobalModelOperations(state)).toEqual([
      { operation: "remove", model: "gpt-*" },
      { operation: "remove", model: "gpt-5.5" },
    ]);
    expect(buildModelsListConfig(state)).toEqual({ enabled: false, models: [] });
  });

  it("does not queue a local row deletion as a global operation", () => {
    const state = hydrateModelsListState(
      { enabled: true, models: ["gpt-5.5"] },
      ["gpt-5.5"],
    );

    removeModelsListItemWithScope(state, state.items[0]);

    expect(getGlobalModelOperations(state)).toEqual([]);
  });

  it("retains pending operations while building a failed-save payload", () => {
    const state = createModelsListState({ enabled: false });
    queueGlobalModelOperation(state, "add", "gpt-5.5");

    expect(buildModelsListConfig(state)).toEqual({ enabled: false, models: [] });
    expect(getGlobalModelOperations(state)).toEqual([
      { operation: "add", model: "gpt-5.5" },
    ]);
  });

  it("builds a create/update payload without changing the hidden-list flag", () => {
    const state = createModelsListState({ enabled: false, models: ["gpt-5.4"] });
    queueGlobalModelOperation(state, "remove", "gpt-5.5");

    expect(buildModelsListPayload(state)).toEqual({
      models_list_config: { enabled: false, models: ["gpt-5.4"] },
      global_model_operations: [{ operation: "remove", model: "gpt-5.5" }],
    });
  });
});
