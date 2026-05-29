export interface ModelsListConfig {
  enabled: boolean
  models: string[]
}

export interface ModelsListItem {
  uid: string
  id: string
  selected: boolean
  editing?: boolean
  draft?: string
}

export interface ModelsListState {
  enabled: boolean
  savedEnabled: boolean
  savedModels: string[]
  items: ModelsListItem[]
  itemsInitialized: boolean
}

let modelsListItemSeq = 0

export const createModelsListState = (
  config?: Partial<ModelsListConfig> | null,
): ModelsListState => ({
  enabled: config?.enabled ?? false,
  savedEnabled: config?.enabled ?? false,
  savedModels: normalizeModels(config?.models ?? []),
  items: [],
  itemsInitialized: false,
})

export const hydrateModelsListState = (
  config: Partial<ModelsListConfig> | null | undefined,
  candidates: string[],
): ModelsListState => {
  const state = createModelsListState(config)
  setModelsListCandidates(state, candidates)
  return state
}

export const setModelsListCandidates = (
  state: ModelsListState,
  candidates: string[],
) => {
  const normalizedCandidates = normalizeModels(candidates)
  const currentSelected = new Set(
    state.items.filter(item => item.selected).map(item => modelKey(item.id)),
  )
  const currentKnown = new Set(state.items.map(item => modelKey(item.id)))
  const savedSelected = new Set(state.savedModels.map(modelKey))
  const candidateKeys = new Set(normalizedCandidates.map(modelKey))
  const existingByKey = new Map(state.items.map(item => [modelKey(item.id), item]))
  const hasExistingItems = state.items.length > 0
  const selectionOrder = normalizeModels([
    ...state.items.map(item => item.id),
    ...state.savedModels,
    ...normalizedCandidates,
  ])

  state.items = selectionOrder.map(id => {
    const selected = hasExistingItems
      ? currentSelected.has(modelKey(id))
      : state.savedModels.length > 0
        ? savedSelected.has(modelKey(id))
        : !state.savedEnabled && candidateKeys.has(modelKey(id))
    const existing = existingByKey.get(modelKey(id))

    return {
      uid: existing?.uid ?? nextModelsListItemUID(),
      id,
      draft: existing?.draft ?? id,
      editing: existing?.editing ?? false,
      selected: selected && (currentKnown.has(modelKey(id)) || savedSelected.has(modelKey(id)) || state.savedModels.length === 0),
    }
  })
  state.itemsInitialized = true
}

export const toggleModelsListItem = (state: ModelsListState, modelID: string) => {
  const item = state.items.find(item => item.id === modelID)
  if (item) {
    item.selected = !item.selected
  }
}

export const moveModelsListItem = (
  state: ModelsListState,
  fromIndex: number,
  toIndex: number,
) => {
  if (
    fromIndex === toIndex ||
    fromIndex < 0 ||
    toIndex < 0 ||
    fromIndex >= state.items.length ||
    toIndex >= state.items.length
  ) {
    return
  }
  const [item] = state.items.splice(fromIndex, 1)
  state.items.splice(toIndex, 0, item)
}

export const addModelsListItem = (state: ModelsListState) => {
  state.itemsInitialized = true
  state.items.unshift({
    uid: nextModelsListItemUID(),
    id: '',
    selected: true,
    editing: true,
    draft: '',
  })
}

export const startEditModelsListItem = (item: ModelsListItem) => {
  item.draft = item.id
  item.editing = true
}

export const commitModelsListItemEdit = (state: ModelsListState, item: ModelsListItem) => {
  const nextID = (item.draft ?? item.id).trim()
  if (!nextID) {
    removeModelsListItem(state, item)
    return
  }
  const nextKey = modelKey(nextID)
  const duplicate = state.items.find(other => other !== item && modelKey(other.id) === nextKey)
  if (duplicate) {
    if (item.id.trim()) {
      item.draft = item.id
      item.editing = false
      return
    }
    removeModelsListItem(state, item)
    return
  }
  item.id = nextID
  item.draft = nextID
  item.editing = false
  item.selected = true
}

export const cancelModelsListItemEdit = (state: ModelsListState, item: ModelsListItem) => {
  if (!item.id.trim()) {
    removeModelsListItem(state, item)
    return
  }
  item.draft = item.id
  item.editing = false
}

export const removeModelsListItem = (state: ModelsListState, item: ModelsListItem) => {
  state.itemsInitialized = true
  const index = state.items.indexOf(item)
  if (index >= 0) {
    state.items.splice(index, 1)
  }
}

export const removeSelectedModelsListItems = (state: ModelsListState) => {
  state.itemsInitialized = true
  state.items = state.items.filter(item => !item.selected)
}

export const buildModelsListConfig = (state: ModelsListState): ModelsListConfig => ({
  enabled: state.enabled,
  models: state.itemsInitialized
    ? normalizeModels(
        state.items
          .filter(item => item.selected)
          .map(item => (item.editing ? item.draft ?? item.id : item.id)),
      )
    : [...state.savedModels],
})

const normalizeModels = (models: string[]): string[] => {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of models) {
    const model = raw.trim()
    const key = modelKey(model)
    if (!model || seen.has(key)) {
      continue
    }
    seen.add(key)
    out.push(model)
  }
  return out
}

const modelKey = (model: string): string => model.trim().toLowerCase()

const nextModelsListItemUID = () => `models-list-item-${++modelsListItemSeq}`
