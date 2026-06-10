/**
 * DashboardPage — Home page of the Paryty client workspace.
 *
 * Renders the Digital Paryty catalogue: a card grid of every Digital Paryty
 * the client owns, a counter row with workspace-level stats, an empty state
 * when no parytys exist, and the multi-step creation wizard modal.
 *
 * All styling uses --aef-* tokens from the Paryty Design System.
 * No hard-coded hex values.
 */

import { useCallback, useRef, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Plus, Globe, BarChart2, Bell, Activity, TrendingUp, FlaskConical,
  ArrowUpRight, CheckCircle2, X, Box, AlertTriangle,
} from 'lucide-react';
import clsx from 'clsx';

import { useDashboardStore } from '../../stores/dashboardStore';
import { usePlanStore } from '../../stores/planStore';
import { ABILITY_CATALOGUE } from '../../types/ability';
import type { DigitalParyty } from '../../types/digitalParyty';
import type { AbilityMeta } from '../../types/ability';
import type { HealthStatus } from '../../types/agent';
import './dashboard.css';

// ─── Icon resolver ──────────────────────────────────────────────────────────

function abilityIcon(iconName: string, size = 14): React.ReactNode {
  const icons: Record<string, React.ReactNode> = {
    Globe:        <Globe size={size} />,
    BarChart2:    <BarChart2 size={size} />,
    Bell:         <Bell size={size} />,
    Activity:     <Activity size={size} />,
    TrendingUp:   <TrendingUp size={size} />,
    FlaskConical: <FlaskConical size={size} />,
  };
  return icons[iconName] ?? <Box size={size} />;
}

// ─── Health dot ──────────────────────────────────────────────────────────────

function HealthDot({ status }: { status: HealthStatus }) {
  return (
    <span
      className={clsx('dp-card__health-dot', `dp-card__health-dot--${status}`)}
      aria-label={`Health: ${status}`}
    />
  );
}

// ─── Ability badge (used on cards) ───────────────────────────────────────────

function AbilityBadge({ ability }: { ability: AbilityMeta }) {
  return (
    <span className="dp-ability-badge">
      {abilityIcon(ability.iconName)}
      {ability.name}
    </span>
  );
}

// ─── Digital Paryty card ─────────────────────────────────────────────────────

function DigitalParytyCard({ paryty }: { paryty: DigitalParyty }) {
  const navigate = useNavigate();
  const enabledMetas = paryty.abilities
    .map((a) => ABILITY_CATALOGUE.find((m) => m.id === a.id))
    .filter((m): m is AbilityMeta => m !== undefined);

  const open = useCallback(() => {
    navigate(`/twins/${paryty.id}`);
  }, [navigate, paryty.id]);

  const created = new Date(paryty.createdAt).toLocaleDateString('en-US', {
    month: 'short', day: 'numeric', year: 'numeric',
  });

  return (
    <article
      className="aef-container-card dp-card"
      tabIndex={0}
      role="button"
      aria-label={`Open ${paryty.name}`}
      onClick={open}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); open(); } }}
    >
      <div className="aef-container-card__body dp-card__inner">
        {/* Header row */}
        <div className="dp-card__head">
          <HealthDot status={paryty.health} />
          <div className="dp-card__title-block">
            <div className="dp-card__name">{paryty.name}</div>
            <div className="dp-card__system">{paryty.systemLabel}</div>
          </div>
        </div>

        {/* Summary stats */}
        {(paryty.summary.nodeCount !== undefined ||
          paryty.summary.activeAlerts !== undefined ||
          paryty.summary.activeTraces !== undefined) && (
          <div className="dp-card__stats">
            {paryty.summary.nodeCount !== undefined && (
              <div className="dp-stat">
                <span className="dp-stat__label">Nodes</span>
                <span className="dp-stat__value">{paryty.summary.nodeCount}</span>
              </div>
            )}
            {paryty.summary.activeTraces !== undefined && (
              <div className="dp-stat">
                <span className="dp-stat__label">Traces</span>
                <span className="dp-stat__value">{paryty.summary.activeTraces}</span>
              </div>
            )}
            {paryty.summary.activeAlerts !== undefined && (
              <div className="dp-stat">
                <span className="dp-stat__label">Alerts</span>
                <span className="dp-stat__value">{paryty.summary.activeAlerts}</span>
              </div>
            )}
            {paryty.summary.forecastHorizonDays !== undefined && (
              <div className="dp-stat">
                <span className="dp-stat__label">Forecast</span>
                <span className="dp-stat__value">{paryty.summary.forecastHorizonDays}d</span>
              </div>
            )}
          </div>
        )}

        {/* Ability badges */}
        {enabledMetas.length > 0 && (
          <div className="dp-card__abilities" aria-label="Enabled abilities">
            {enabledMetas.map((m) => (
              <AbilityBadge key={m.id} ability={m} />
            ))}
          </div>
        )}

        {/* Footer */}
        <div className="dp-card__footer">
          <span className="dp-card__date">Created {created}</span>
          <button className="dp-card__open-btn" tabIndex={-1} aria-hidden>
            Open <ArrowUpRight size={12} />
          </button>
        </div>
      </div>
    </article>
  );
}

// ─── Empty state ─────────────────────────────────────────────────────────────

function EmptyState({ onNew }: { onNew: () => void }) {
  return (
    <div className="dp-empty">
      <Box size={40} className="dp-empty__icon" />
      <div>
        <p className="dp-empty__title">No Digital Parytys yet</p>
        <p className="dp-empty__sub">
          Create your first Digital Paryty to start observing, forecasting, and
          running Watif drills on your software systems.
        </p>
      </div>
      <button className="aef-btn aef-btn-active" onClick={onNew}>
        <Plus size={14} /> New Digital Paryty
      </button>
    </div>
  );
}

// ─── Wizard ───────────────────────────────────────────────────────────────────

const WIZARD_STEPS = ['Identity', 'Abilities', 'Confirm'];

function WizardStepDots({ current, total }: { current: number; total: number }) {
  return (
    <div className="dp-step-dots" aria-label={`Step ${current + 1} of ${total}`}>
      {Array.from({ length: total }).map((_, i) => (
        <span
          key={i}
          className={clsx('dp-step-dot', i === current && 'dp-step-dot--active')}
        />
      ))}
    </div>
  );
}

// Step 0 — Identity
function StepIdentity() {
  const name = useDashboardStore((s) => s.draft.name);
  const systemLabel = useDashboardStore((s) => s.draft.systemLabel);
  const setName = useDashboardStore((s) => s.setDraftName);
  const setSystem = useDashboardStore((s) => s.setDraftSystemLabel);

  return (
    <>
      <div className="dp-field">
        <label className="dp-field__label" htmlFor="dp-name">
          Digital Paryty name
        </label>
        <input
          id="dp-name"
          className="dp-field__input"
          type="text"
          placeholder="e.g. Payment Gateway Twin"
          value={name}
          onChange={(e) => setName(e.target.value)}
          autoFocus
          maxLength={80}
        />
      </div>
      <div className="dp-field">
        <label className="dp-field__label" htmlFor="dp-system">
          Software system being twinned
        </label>
        <input
          id="dp-system"
          className="dp-field__input"
          type="text"
          placeholder="e.g. Payment Gateway v3, Auth Service"
          value={systemLabel}
          onChange={(e) => setSystem(e.target.value)}
          maxLength={80}
        />
      </div>
      <p style={{
        fontFamily: 'var(--aef-font-body)',
        fontSize: 11,
        color: 'var(--aef-text-secondary)',
        lineHeight: 1.6,
      }}>
        A Digital Paryty is a live-telemetry-driven digital twin of one of your
        software systems. You can create as many as your subscription allows and
        add or change abilities at any time after creation.
      </p>
    </>
  );
}

// Step 1 — Ability selection
function StepAbilities() {
  const selected = useDashboardStore((s) => s.draft.selectedAbilities);
  const toggle = useDashboardStore((s) => s.toggleDraftAbility);

  return (
    <>
      <p style={{
        fontFamily: 'var(--aef-font-body)',
        fontSize: 11,
        color: 'var(--aef-text-secondary)',
        lineHeight: 1.6,
      }}>
        Select the abilities you want to activate on this Digital Paryty.
        Only selected abilities will be configured and billed. You can add more
        abilities later from the Digital Paryty settings.
      </p>
      <div className="dp-ability-grid">
        {ABILITY_CATALOGUE.map((ability) => {
          const isSelected = selected.includes(ability.id);
          return (
            <button
              key={ability.id}
              type="button"
              className={clsx('dp-ability-tile', isSelected && 'dp-ability-tile--selected')}
              onClick={() => toggle(ability.id)}
              aria-pressed={isSelected}
            >
              <CheckCircle2 size={14} className="dp-ability-tile__check" aria-hidden />
              <span className="dp-ability-tile__icon">
                {abilityIcon(ability.iconName, 16)}
              </span>
              <span className="dp-ability-tile__name">{ability.name}</span>
              <span className="dp-ability-tile__desc">{ability.description}</span>
            </button>
          );
        })}
      </div>
    </>
  );
}

// Step 2 — Confirm
function StepConfirm() {
  const draft = useDashboardStore((s) => s.draft);
  const enabledMetas = draft.selectedAbilities
    .map((id) => ABILITY_CATALOGUE.find((m) => m.id === id))
    .filter((m): m is AbilityMeta => m !== undefined);

  return (
    <>
      <p style={{
        fontFamily: 'var(--aef-font-body)',
        fontSize: 11,
        color: 'var(--aef-text-secondary)',
        lineHeight: 1.6,
      }}>
        Review your choices before creating the Digital Paryty.
      </p>

      <div>
        <div className="dp-confirm-row">
          <span className="dp-confirm-row__label">Name</span>
          <span className="dp-confirm-row__value">{draft.name || '—'}</span>
        </div>
        <div className="dp-confirm-row">
          <span className="dp-confirm-row__label">System</span>
          <span className="dp-confirm-row__value">{draft.systemLabel || '—'}</span>
        </div>
        <div className="dp-confirm-row">
          <span className="dp-confirm-row__label">Abilities ({enabledMetas.length})</span>
          {enabledMetas.length === 0 ? (
            <span className="dp-confirm-row__value" style={{ color: 'var(--aef-text-secondary)' }}>
              None selected — you can add abilities after creation.
            </span>
          ) : (
            <div className="dp-confirm-abilities">
              {enabledMetas.map((m) => (
                <AbilityBadge key={m.id} ability={m} />
              ))}
            </div>
          )}
        </div>
      </div>
    </>
  );
}

// Wizard modal
function CreateParytyWizard() {
  const wizardOpen   = useDashboardStore((s) => s.wizardOpen);
  const step         = useDashboardStore((s) => s.wizardStep);
  const draft        = useDashboardStore((s) => s.draft);
  const close        = useDashboardStore((s) => s.closeWizard);
  const next         = useDashboardStore((s) => s.nextStep);
  const prev         = useDashboardStore((s) => s.prevStep);
  const commit       = useDashboardStore((s) => s.commitDraft);
  const backdropRef  = useRef<HTMLDivElement>(null);

  // Close on Escape
  useEffect(() => {
    if (!wizardOpen) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') close();
    }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [wizardOpen, close]);

  if (!wizardOpen) return null;

  const isLastStep = step === WIZARD_STEPS.length - 1;
  const canAdvance = step === 0 ? draft.name.trim().length > 0 : true;

  const stepContent = [<StepIdentity />, <StepAbilities />, <StepConfirm />][step];
  const primaryLabel = isLastStep ? 'Create Digital Paryty' : 'Continue';

  return (
    <div
      className="aef-modal-overlay"
      ref={backdropRef}
      onClick={(e) => { if (e.target === backdropRef.current) close(); }}
      role="dialog"
      aria-modal="true"
      aria-label={`Create Digital Paryty — ${WIZARD_STEPS[step]}`}
    >
      <div className="aef-modal" style={{ maxWidth: 560 }}>
        {/* Header */}
        <div className="aef-modal-header">
          <span className="aef-modal-title">
            New Digital Paryty — {WIZARD_STEPS[step]}
          </span>
          <button className="aef-modal-close" onClick={close} aria-label="Close wizard">
            <X size={14} />
          </button>
        </div>

        {/* Body */}
        <div className="aef-modal-body">
          {stepContent}
        </div>

        {/* Footer */}
        <div className="aef-modal-footer">
          <WizardStepDots current={step} total={WIZARD_STEPS.length} />
          <div style={{ display: 'flex', gap: 'var(--aef-space-2)' }}>
            {step > 0 && (
              <button className="aef-btn aef-btn-inactive" onClick={prev}>
                Back
              </button>
            )}
            <button
              className="aef-btn aef-btn-active"
              onClick={isLastStep ? commit : next}
              disabled={!canAdvance}
              style={{ opacity: canAdvance ? 1 : 0.4 }}
            >
              {primaryLabel}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ─── Counter strip ────────────────────────────────────────────────────────────

type CounterVariant = 'neutral' | 'active' | 'variant-a' | 'variant-b';

function CounterCard({
  label, value, variant = 'neutral',
}: {
  label: string;
  value: string | number;
  variant?: CounterVariant;
}) {
  return (
    <div className={`aef-counter aef-counter-${variant}`}>
      <div className="aef-counter__icon">
        <ArrowUpRight size={12} />
      </div>
      <div className="aef-counter__body">
        <span className="aef-counter__label">{label}</span>
        <span className="aef-counter__value">{value}</span>
      </div>
    </div>
  );
}

// ─── Plan Usage Banner ────────────────────────────────────────────────────────

function PlanUsageBanner({
  totalParytys,
}: {
  totalParytys: number;
}) {
  const usageFor = usePlanStore((s) => s.usageFor);
  const isNearLimit = usePlanStore((s) => s.isNearLimit);
  const navigate = useNavigate();

  const usage = usageFor('twins', totalParytys);

  if (!usage) return null;

  const { current, max, percentage } = usage;

  // Determine banner style
  const isAtLimit = current >= max;
  const isWarning = isNearLimit('twins', totalParytys) && !isAtLimit;

  if (!isAtLimit && !isWarning && percentage < 50) return null;

  return (
    <div
      className="dp-usage-banner"
      style={{
        padding: 'var(--aef-space-3) var(--aef-space-4)',
        borderRadius: 'var(--aef-radius-md)',
        background: isAtLimit
          ? 'rgba(239, 68, 68, 0.1)'
          : 'rgba(245, 158, 11, 0.1)',
        border: `1px solid ${isAtLimit ? 'rgba(239, 68, 68, 0.3)' : 'rgba(245, 158, 11, 0.3)'}`,
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--aef-space-3)',
      }}
      data-testid="plan-usage-banner"
    >
      <AlertTriangle size={16} style={{ color: isAtLimit ? 'var(--aef-danger)' : 'var(--aef-warning)', flexShrink: 0 }} />
      <span style={{ fontFamily: 'var(--aef-font-body)', fontSize: 12, color: 'var(--aef-text-primary)' }}>
        {isAtLimit
          ? `Digital Paryty limit reached: ${current}/${max}.`
          : `${current}/${max} Digital Parytys used (${percentage}%).`}
      </span>
      {isAtLimit && (
        <button
          className="aef-btn aef-btn-active"
          style={{ marginLeft: 'auto', flexShrink: 0 }}
          onClick={() => navigate('/settings?tab=plan')}
        >
          Upgrade Plan
        </button>
      )}
    </div>
  );
}

// ─── Page ─────────────────────────────────────────────────────────────────────

export function DashboardPage() {
  const catalogue   = useDashboardStore((s) => s.catalogue);
  const openWizard  = useDashboardStore((s) => s.openWizard);
  const usageFor   = usePlanStore((s) => s.usageFor);
  const currentPlan = usePlanStore((s) => s.currentPlan);

  // Derived counts for the counter row
  const totalParytys   = catalogue.length;
  const healthyCount   = catalogue.filter((p) => p.health === 'healthy').length;
  const degradedCount  = catalogue.filter((p) => p.health === 'degraded' || p.health === 'unhealthy').length;
  const totalAbilities = catalogue.reduce((acc, p) => acc + p.abilities.length, 0);

  // Is the twin limit reached?
  const twinUsage = usageFor('twins', totalParytys);
  const atTwinLimit = twinUsage !== null && totalParytys >= twinUsage.max;
  const isBasic = currentPlan?.planName === 'basic' || currentPlan?.planName === 'Basic';

  return (
    <div className="dp-page">
      <div className="dp-page__inner">

        {/* Page header */}
        <div className="dp-header">
          <div className="dp-header__title-block">
            <h1 className="dp-header__title">Digital Parytys</h1>
            <p className="dp-header__sub">
              Your live digital twins — one per software system, each driven by
              real telemetry from your deployed agents.
            </p>
          </div>
          <div className="dp-header__actions">
            <button
              className="aef-btn aef-btn-active"
              onClick={openWizard}
              disabled={atTwinLimit}
              style={{ opacity: atTwinLimit ? 0.4 : 1 }}
              title={atTwinLimit ? 'Digital Paryty limit reached — upgrade your plan to create more' : undefined}
              data-testid="dashboard-new-twin"
            >
              <Plus size={14} /> New Digital Paryty
            </button>
          </div>
        </div>

        {/* Plan usage banner */}
        <PlanUsageBanner totalParytys={totalParytys} />

        {/* Upgrade CTA for Basic users */}
        {isBasic && totalParytys > 0 && (
          <div
            className="aef-container-card"
            style={{
              padding: 'var(--aef-space-3) var(--aef-space-4)',
              display: 'flex',
              alignItems: 'center',
              gap: 'var(--aef-space-3)',
              borderColor: 'var(--aef-accent)',
            }}
            data-testid="basic-upgrade-cta"
          >
            <ArrowUpRight size={14} style={{ color: 'var(--aef-accent)', flexShrink: 0 }} />
            <span style={{ fontFamily: 'var(--aef-font-body)', fontSize: 12, color: 'var(--aef-text-primary)', flex: 1 }}>
              On the Basic plan? Upgrade to Pro for more Digital Parytys, longer data retention, and Paryty-Intel AI forecasting.
            </span>
            <button
              className="aef-btn aef-btn-active"
              onClick={() => window.location.href = '/settings?tab=plan'}
              style={{ flexShrink: 0 }}
            >
              Upgrade
            </button>
          </div>
        )}

        {/* Counter row — only shown when at least one paryty exists */}
        {totalParytys > 0 && (
          <div className="dp-counters">
            <CounterCard label="Total"    value={totalParytys}  variant="neutral"   />
            <CounterCard label="Healthy"  value={healthyCount}  variant="active"    />
            {degradedCount > 0 && (
              <CounterCard label="Degraded" value={degradedCount} variant="variant-b" />
            )}
            <CounterCard label="Abilities" value={totalAbilities} variant="neutral"  />
          </div>
        )}

        {/* Catalogue or empty state */}
        {catalogue.length === 0 ? (
          <EmptyState onNew={openWizard} />
        ) : (
          <div className="dp-catalogue">
            {catalogue.map((p) => (
              <DigitalParytyCard key={p.id} paryty={p} />
            ))}
          </div>
        )}
      </div>

      {/* Wizard modal — rendered at page level, not in the catalogue */}
      <CreateParytyWizard />
    </div>
  );
}
