/**
 * TwinCreatePage — 4-step Digital Paryty creation wizard.
 *
 * Steps:
 * 1. Identity — name, system label
 * 2. Agents — select agents to link (stub)
 * 3. Abilities — select abilities to enable
 * 4. Confirm — review and submit
 *
 * @module pages/TwinCreatePage
 */

import { useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { Plus, ArrowLeft, ArrowRight } from 'lucide-react';
import clsx from 'clsx';
import { useToastStore } from '../stores/toastStore';
import { getRestClient } from '../api/rest';
import { ABILITY_CATALOGUE } from '../types/ability';
import type { AbilityId } from '../types/ability';
import { StepIndicator } from '../components/common/StepIndicator';

// ─── Step Labels ──────────────────────────────────────────────────────

const STEPS = ['Identity', 'Agents', 'Abilities', 'Confirm'];

// ─── Page ─────────────────────────────────────────────────────────────

/**
 * Multi-step wizard for creating a new Digital Paryty.
 */
export function TwinCreatePage() {
  const navigate = useNavigate();
  const addToast = useToastStore((s) => s.addToast);

  const [step, setStep] = useState(0);
  const [name, setName] = useState('');
  const [systemLabel, setSystemLabel] = useState('');
  const [selectedAbilities, setSelectedAbilities] = useState<AbilityId[]>([]);
  const [isSubmitting, setIsSubmitting] = useState(false);

  const canAdvance = (): boolean => {
    switch (step) {
      case 0: return name.trim().length > 0;
      default: return true;
    }
  };

  const toggleAbility = useCallback((id: AbilityId) => {
    setSelectedAbilities((prev) =>
      prev.includes(id) ? prev.filter((a) => a !== id) : [...prev, id],
    );
  }, []);

  const handleSubmit = useCallback(async () => {
    setIsSubmitting(true);
    try {
      const client = getRestClient();
      const response = await client.post<{ id: string; name: string }>('/api/v1/twins', {
        name: name.trim(),
        description: systemLabel.trim(),
      });

      addToast({ type: 'success', message: `Digital Paryty "${response.name}" created!` });
      navigate(`/twins/${response.id}`);
    } catch {
      addToast({ type: 'error', message: 'Failed to create Digital Paryty.' });
    } finally {
      setIsSubmitting(false);
    }
  }, [name, systemLabel, addToast, navigate]);

  const handleNext = useCallback(() => {
    if (!canAdvance()) return;
    if (step === STEPS.length - 1) {
      handleSubmit();
      return;
    }
    setStep((s) => s + 1);
  }, [step, canAdvance, handleSubmit]);

  const isLastStep = step === STEPS.length - 1;

  return (
    <div className="dp-page" data-testid="twin-create-page">
      <div className="dp-page__inner" style={{ maxWidth: 600 }}>
        <div className="dp-header">
          <div className="dp-header__title-block">
            <h1 className="dp-header__title">New Digital Paryty</h1>
            <p className="dp-header__sub">
              Step {step + 1} of {STEPS.length}: {STEPS[step]}
            </p>
          </div>
        </div>

        {/* Step 1: Identity */}
        {step === 0 && (
          <>
            <div className="dp-field">
              <label className="dp-field__label" htmlFor="tw-name">Digital Paryty name</label>
              <input
                id="tw-name" className="dp-field__input" type="text"
                placeholder="e.g. Payment Gateway Twin" value={name}
                onChange={(e) => setName(e.target.value)} autoFocus maxLength={80}
              />
            </div>
            <div className="dp-field">
              <label className="dp-field__label" htmlFor="tw-system">Software system</label>
              <input
                id="tw-system" className="dp-field__input" type="text"
                placeholder="e.g. Payment Gateway v3" value={systemLabel}
                onChange={(e) => setSystemLabel(e.target.value)} maxLength={80}
              />
            </div>
            <p style={{
              fontFamily: 'var(--aef-font-body)', fontSize: 11,
              color: 'var(--aef-text-secondary)', lineHeight: 1.6,
            }}>
              A Digital Paryty is a live-telemetry-driven digital twin of your software system.
            </p>
          </>
        )}

        {/* Step 2: Agents (stub) */}
        {step === 1 && (
          <p style={{
            fontFamily: 'var(--aef-font-body)', fontSize: 12,
            color: 'var(--aef-text-secondary)', lineHeight: 1.6,
          }}>
            Agents will be automatically linked when they connect with your tenant&apos;s API key.
            You can also assign agents after creation from the Digital Paryty settings.
          </p>
        )}

        {/* Step 3: Abilities */}
        {step === 2 && (
          <>
            <p style={{
              fontFamily: 'var(--aef-font-body)', fontSize: 11,
              color: 'var(--aef-text-secondary)', lineHeight: 1.6,
            }}>
              Select the abilities to activate on this Digital Paryty.
            </p>
            <div className="dp-ability-grid">
              {ABILITY_CATALOGUE.map((ability) => {
                const isSelected = selectedAbilities.includes(ability.id);
                return (
                  <button
                    key={ability.id} type="button"
                    className={clsx('dp-ability-tile', isSelected && 'dp-ability-tile--selected')}
                    onClick={() => toggleAbility(ability.id)}
                    aria-pressed={isSelected}
                  >
                    <span className="dp-ability-tile__name">{ability.name}</span>
                    <span className="dp-ability-tile__desc">{ability.description}</span>
                  </button>
                );
              })}
            </div>
          </>
        )}

        {/* Step 4: Confirm */}
        {step === 3 && (
          <>
            <p style={{
              fontFamily: 'var(--aef-font-body)', fontSize: 11,
              color: 'var(--aef-text-secondary)', lineHeight: 1.6,
            }}>
              Review your choices before creating.
            </p>
            <div>
              <div className="dp-confirm-row">
                <span className="dp-confirm-row__label">Name</span>
                <span className="dp-confirm-row__value">{name || '—'}</span>
              </div>
              <div className="dp-confirm-row">
                <span className="dp-confirm-row__label">System</span>
                <span className="dp-confirm-row__value">{systemLabel || '—'}</span>
              </div>
              <div className="dp-confirm-row">
                <span className="dp-confirm-row__label">Abilities</span>
                <span className="dp-confirm-row__value">{selectedAbilities.length}</span>
              </div>
            </div>
          </>
        )}

        {/* Navigation */}
        <div style={{
          display: 'flex', alignItems: 'center', justifyContent: 'space-between',
          marginTop: 'var(--aef-space-6)',
        }}>
          <StepIndicator total={STEPS.length} current={step} />
          <div style={{ display: 'flex', gap: 'var(--aef-space-2)' }}>
            {step > 0 && (
              <button type="button" className="aef-btn aef-btn-inactive" onClick={() => setStep((s) => s - 1)}>
                <ArrowLeft size={14} /> Back
              </button>
            )}
            <button
              className="aef-btn aef-btn-active"
              onClick={handleNext}
              disabled={!canAdvance() || isSubmitting}
              style={{ opacity: canAdvance() && !isSubmitting ? 1 : 0.4 }}
              data-testid="tw-create-submit"
            >
              {isSubmitting ? 'Creating…' : isLastStep ? (
                <><Plus size={14} /> Create</>
              ) : (
                <>Continue <ArrowRight size={14} /></>
              )}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
