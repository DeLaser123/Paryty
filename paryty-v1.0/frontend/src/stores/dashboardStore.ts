import { create } from 'zustand';
import { getRestClient } from '../api/rest';
import type { DigitalParyty, CreateParytyDraft, TwinDetails } from '../types/digitalParyty';
import { backendTwinToDigitalParyty } from '../types/digitalParyty';
import type { AbilityId } from '../types/ability';

// ─── localStorage helpers ──────────────────────────────────────────────────

const ACTIVE_TWIN_KEY = 'paryty_active_twin';

function loadActiveTwinId(): string | null {
  try {
    return localStorage.getItem(ACTIVE_TWIN_KEY);
  } catch {
    return null;
  }
}

function saveActiveTwinId(id: string | null): void {
  try {
    if (id) {
      localStorage.setItem(ACTIVE_TWIN_KEY, id);
    } else {
      localStorage.removeItem(ACTIVE_TWIN_KEY);
    }
  } catch {
    // localStorage unavailable — silent fail
  }
}

// ─── Store Shape ────────────────────────────────────────────────────────────

interface DashboardState {
  /** Ordered list of the client's Digital Parytys. */
  catalogue: DigitalParyty[];

  /** Whether the creation wizard modal is open. */
  wizardOpen: boolean;

  /** In-progress draft for the creation wizard. */
  draft: CreateParytyDraft;

  /** Which wizard step (0-based) the user is on. */
  wizardStep: number;

  /** Whether twin list is being fetched from backend. */
  isLoading: boolean;

  /** Error message from last fetchTwins attempt. */
  error: string | null;

  /** ID of the currently active/selected twin. */
  activeTwinId: string | null;

  // ─── Actions ──────────────────────────────────────────────────────────────

  openWizard: () => void;
  closeWizard: () => void;

  setDraftName: (name: string) => void;
  setDraftSystemLabel: (label: string) => void;
  setDraftAgentIds: (ids: string[]) => void;
  toggleDraftAbility: (id: AbilityId) => void;

  nextStep: () => void;
  prevStep: () => void;

  /** Commits the current draft as a new DigitalParyty in the catalogue. */
  commitDraft: () => Promise<void>;

  /** Adds a new ability to an existing Digital Paryty after creation. */
  addAbility: (parytyId: string, abilityId: AbilityId) => void;

  /** Fetch all twins from the backend and populate catalogue. */
  fetchTwins: () => Promise<void>;

  /** Set the active twin (persisted to localStorage). */
  setActiveTwin: (id: string | null) => void;

  /** Delete a twin by ID. */
  deleteTwin: (id: string) => Promise<void>;
}

// ─── Initial draft ─────────────────────────────────────────────────────────

function emptyDraft(): CreateParytyDraft {
  return { name: '', systemLabel: '', agentIds: [], selectedAbilities: [] };
}

// ─── Store ─────────────────────────────────────────────────────────────────

export const useDashboardStore = create<DashboardState>()((set, get) => ({
  catalogue: [],
  wizardOpen: false,
  draft: emptyDraft(),
  wizardStep: 0,
  isLoading: false,
  error: null,
  activeTwinId: loadActiveTwinId(),

  openWizard: () => set({ wizardOpen: true, draft: emptyDraft(), wizardStep: 0 }),
  closeWizard: () => set({ wizardOpen: false }),

  setDraftName: (name) => set((s) => ({ draft: { ...s.draft, name } })),
  setDraftSystemLabel: (label) => set((s) => ({ draft: { ...s.draft, systemLabel: label } })),
  setDraftAgentIds: (ids) => set((s) => ({ draft: { ...s.draft, agentIds: ids } })),

  toggleDraftAbility: (id) =>
    set((s) => {
      const has = s.draft.selectedAbilities.includes(id);
      return {
        draft: {
          ...s.draft,
          selectedAbilities: has
            ? s.draft.selectedAbilities.filter((a) => a !== id)
            : [...s.draft.selectedAbilities, id],
        },
      };
    }),

  nextStep: () => set((s) => ({ wizardStep: s.wizardStep + 1 })),
  prevStep: () => set((s) => ({ wizardStep: Math.max(0, s.wizardStep - 1) })),

  commitDraft: async () => {
    const { draft, catalogue } = get();
    const now = new Date().toISOString();
    
    // Call backend API to create twin with proper UUID
    const client = getRestClient();
    const response = await client.post<{ data: { id: string; name: string; description: string; status: string; createdAt: string; updatedAt: string } }>('/api/v1/twins', {
      name: draft.name || 'Unnamed Digital Paryty',
      description: draft.systemLabel || 'Unknown System',
    });
    
    const twin = response.data;
    const next: DigitalParyty = {
      id: twin.id,
      name: twin.name,
      systemLabel: twin.description || 'Unknown System',
      health: 'unknown',
      abilities: draft.selectedAbilities.map((id) => ({ id, enabledAt: now })),
      config: { agentIds: draft.agentIds },
      summary: {},
      createdAt: twin.createdAt || now,
      updatedAt: twin.updatedAt || now,
    };
    set({ catalogue: [...catalogue, next], wizardOpen: false, draft: emptyDraft(), wizardStep: 0 });
  },

  addAbility: (parytyId, abilityId) =>
    set((s) => ({
      catalogue: s.catalogue.map((p) => {
        if (p.id !== parytyId) return p;
        if (p.abilities.some((a) => a.id === abilityId)) return p;
        return {
          ...p,
          abilities: [...p.abilities, { id: abilityId, enabledAt: new Date().toISOString() }],
          updatedAt: new Date().toISOString(),
        };
      }),
    })),

  fetchTwins: async () => {
    set({ isLoading: true, error: null });
    try {
      const client = getRestClient();
      const response = await client.get<{ data: TwinDetails[] }>('/api/v1/twins');
      const twins = response.data ?? [];
      const catalogue = twins.map(backendTwinToDigitalParyty);
      
      // Validate activeTwinId — clear if not in results
      const { activeTwinId } = get();
      const validActiveId = catalogue.some((t) => t.id === activeTwinId)
        ? activeTwinId
        : null;
      
      set({
        catalogue,
        isLoading: false,
        activeTwinId: validActiveId,
      });
      
      if (validActiveId !== activeTwinId) {
        saveActiveTwinId(validActiveId);
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load twins';
      set({ isLoading: false, error: message });
    }
  },

  setActiveTwin: (id) => {
    set({ activeTwinId: id });
    saveActiveTwinId(id);
  },

  deleteTwin: async (id) => {
    const client = getRestClient();
    await client.delete(`/api/v1/twins/${id}`);
    const { catalogue, activeTwinId } = get();
    set({
      catalogue: catalogue.filter((t) => t.id !== id),
      activeTwinId: activeTwinId === id ? null : activeTwinId,
    });
    if (activeTwinId === id) {
      saveActiveTwinId(null);
    }
  },
}));
