/**
 * Ability — first-class feature modules that can be toggled per Digital Paryty.
 *
 * Each Ability maps to a backend intelligence capability and a frontend view.
 * The set is designed to be extended: add a new entry to ABILITY_CATALOGUE
 * and the creation wizard, dashboard cards, and settings screens pick it up
 * automatically.
 */

// ─── Ability IDs ────────────────────────────────────────────────────────────

export type AbilityId =
  | 'topology_observation'
  | 'forecasting'
  | 'watif_drills'
  | 'metrics_monitoring'
  | 'timeline_replay'
  | 'alerts';

// ─── Ability Metadata ───────────────────────────────────────────────────────

export interface AbilityMeta {
  id: AbilityId;
  /** Short display name shown in cards, wizard, and settings. */
  name: string;
  /** One-sentence description shown in the wizard selection step. */
  description: string;
  /** Route the ability maps to inside a Digital Paryty workspace. */
  route: string;
  /**
   * Icon name from lucide-react passed through as a string key so the UI
   * can resolve it without importing every icon at the type level.
   */
  iconName: string;
}

// ─── Catalogue (source of truth for available abilities) ───────────────────

export const ABILITY_CATALOGUE: AbilityMeta[] = [
  {
    id: 'topology_observation',
    name: 'Topology Observation',
    description:
      'Animated live graph of every service, process, and network channel. ' +
      'Trace a single user request end-to-end through your entire system.',
    route: '/topology',
    iconName: 'Globe',
  },
  {
    id: 'metrics_monitoring',
    name: 'Metrics & Monitoring',
    description:
      'Real-time CPU, memory, disk, and network metrics per agent with ' +
      'anomaly detection and configurable alert rules.',
    route: '/metrics',
    iconName: 'BarChart2',
  },
  {
    id: 'alerts',
    name: 'Alerts',
    description:
      'Rule-based alerting on any metric or topology event. ' +
      'Acknowledge, group, and silence alerts from a single panel.',
    route: '/alerts',
    iconName: 'Bell',
  },
  {
    id: 'timeline_replay',
    name: 'Timeline Replay',
    description:
      'Rewind and replay any historical window of your system state at ' +
      'configurable speed to investigate past incidents.',
    route: '/timeline',
    iconName: 'Activity',
  },
  {
    id: 'forecasting',
    name: 'Forecasting',
    description:
      'Project the future state of your infrastructure. Know days in advance ' +
      'if you need a new node, more capacity, or a config change.',
    route: '/intel',
    iconName: 'TrendingUp',
  },
  {
    id: 'watif_drills',
    name: 'Watif Drills',
    description:
      'Simulate any what-if scenario against your digital twin in compressed time. ' +
      'Run "2.5× gateway load every Friday, +0.1× per week" and see your ' +
      'architecture 3 months out.',
    route: '/intel',
    iconName: 'FlaskConical',
  },
];

// ─── Per-Paryty ability config stored after creation ──────────────────────

export interface EnabledAbility {
  id: AbilityId;
  enabledAt: string; // ISO 8601
}

// ─── Ability-to-Feature Gating Map ─────────────────────────────────────────
// Maps ability IDs to plan feature names. Used to gate abilities in the
// creation wizard based on the tenant's current plan.

export const ABILITY_FEATURE_MAP: Record<AbilityId, string> = {
  topology_observation: 'topology_monitoring',
  metrics_monitoring:   'metrics',
  alerts:               'alerts',
  timeline_replay:      'timeline_replay',
  forecasting:          'paryty_intel',
  watif_drills:         'paryty_intel',
};
