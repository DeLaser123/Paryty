import { create } from 'zustand';
import type { DigitalParyty, CreateParytyDraft } from '../types/digitalParyty';
import type { AbilityId } from '../types/ability';

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
  commitDraft: () => void;

  /** Adds a new ability to an existing Digital Paryty after creation. */
  addAbility: (parytyId: string, abilityId: AbilityId) => void;
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

  commitDraft: () => {
    const { draft, catalogue } = get();
    const now = new Date().toISOString();
    const next: DigitalParyty = {
      id: `dp_${Date.now()}`,
      name: draft.name || 'Unnamed Digital Paryty',
      systemLabel: draft.systemLabel || 'Unknown System',
      health: 'unknown',
      abilities: draft.selectedAbilities.map((id) => ({ id, enabledAt: now })),
      config: { agentIds: draft.agentIds },
      summary: {},
      createdAt: now,
      updatedAt: now,
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
}));
