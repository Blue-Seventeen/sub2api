import { describe, expect, it } from "vitest";

import {
  addModelsListItem,
  buildModelsListConfig,
  commitModelsListItemEdit,
  createModelsListState,
  hydrateModelsListState,
  moveModelsListItem,
  removeModelsListItem,
  removeSelectedModelsListItems,
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
});
