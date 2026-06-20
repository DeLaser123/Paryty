/**
 * TwinConfigForm — reusable configuration form for creating and editing twins.
 *
 * Provides fields for twin name, description/system label, and agent selection.
 * Used by both TwinCreatePage and TwinEditPage to ensure consistent UX.
 *
 * @module components/Twin/TwinConfigForm
 */

import { memo, useCallback, useState } from 'react';
import clsx from 'clsx';
import type { CreateTwinPayload } from '../../api/twins';
import type { TwinConfig } from '../../types/digitalParyty';

// ─── Types ──────────────────────────────────────────────────────────────────

interface TwinConfigFormProps {
  /** Initial values for the form (used in edit mode). */
  initialValues?: Partial<CreateTwinPayload>;
  /** Called when the form is submitted with valid data. */
  onSubmit: (payload: CreateTwinPayload) => void;
  /** Whether the form is in a submitting state. */
  isSubmitting?: boolean;
  /** Whether the form should be read-only. */
  readOnly?: boolean;
  /** Optional additional CSS classes. */
  className?: string;
  /** Optional submit button label override. */
  submitLabel?: string;
  /** Whether to show the submit button (false when parent controls submission). */
  showSubmit?: boolean;
}

// ─── Component ──────────────────────────────────────────────────────────────

/**
 * Configuration form for Digital Paryty twins.
 *
 * Renders fields for name, description, and system label using the
 * `dp-field` and `aef-btn` design system patterns.
 *
 * The form validates that the name is non-empty before allowing submission.
 */
export const TwinConfigForm = memo(function TwinConfigForm({
  initialValues,
  onSubmit,
  isSubmitting = false,
  readOnly = false,
  className,
  submitLabel = 'Save',
  showSubmit = true,
}: TwinConfigFormProps) {
  const [name, setName] = useState(initialValues?.name ?? '');
  const [description, setDescription] = useState(initialValues?.description ?? '');
  const [agentLabelsText, setAgentLabelsText] = useState(
    initialValues?.config?.agentLabels
      ? Object.entries(initialValues.config.agentLabels).map(([k, v]) => `${k}=${v}`).join(', ')
      : '',
  );
  const [collectorsText, setCollectorsText] = useState(
    initialValues?.config?.enabledCollectors?.join(', ') ?? '',
  );
  const [intervalSeconds, setIntervalSeconds] = useState(
    initialValues?.config?.collectionIntervalSeconds?.toString() ?? '',
  );
  const [samplingRate, setSamplingRate] = useState(
    initialValues?.config?.samplingRate?.toString() ?? '',
  );

  const isValid = name.trim().length > 0;

  const handleSubmit = useCallback(
    (e: React.FormEvent) => {
      e.preventDefault();
      if (!isValid || isSubmitting) return;

      // Parse agent labels from "key=value, key2=value2" format
      const agentLabels: Record<string, string> = {};
      agentLabelsText
        .split(',')
        .map((pair) => pair.trim())
        .filter((pair) => pair.length > 0)
        .forEach((pair) => {
          const [k, ...rest] = pair.split('=');
          if (k && rest.length > 0) {
            agentLabels[k.trim()] = rest.join('=').trim();
          }
        });

      const collectors = collectorsText
        .split(',')
        .map((c) => c.trim())
        .filter((c) => c.length > 0);

      const parsedInterval = intervalSeconds ? parseInt(intervalSeconds, 10) : undefined;
      const parsedRate = samplingRate ? parseFloat(samplingRate) : undefined;

      const config: TwinConfig = {
        ...(Object.keys(agentLabels).length > 0 ? { agentLabels } : {}),
        ...(collectors.length > 0 ? { enabledCollectors: collectors } : {}),
        ...(parsedInterval && parsedInterval > 0 ? { collectionIntervalSeconds: parsedInterval } : {}),
        ...(parsedRate !== undefined && parsedRate >= 0 && parsedRate <= 1 ? { samplingRate: parsedRate } : {}),
      };

      onSubmit({
        name: name.trim(),
        description: description.trim() || undefined,
        config: Object.keys(config).length > 0 ? config : undefined,
      });
    },
    [name, description, agentLabelsText, collectorsText, intervalSeconds, samplingRate, isValid, isSubmitting, onSubmit],
  );

  return (
    <form
      className={clsx('twin-config-form', className)}
      onSubmit={handleSubmit}
      data-testid="twin-config-form"
    >
      {/* Twin Name */}
      <div className="dp-field">
        <label className="dp-field__label" htmlFor="tc-name">
          Digital Paryty name <span style={{ color: 'var(--aef-status-error)' }}>*</span>
        </label>
        <input
          id="tc-name"
          className="dp-field__input"
          type="text"
          placeholder="e.g. Payment Gateway Twin"
          value={name}
          onChange={(e) => setName(e.target.value)}
          readOnly={readOnly}
          disabled={readOnly}
          maxLength={80}
          required
          autoFocus
          data-testid="tc-name-input"
        />
      </div>

      {/* Description */}
      <div className="dp-field">
        <label className="dp-field__label" htmlFor="tc-description">
          Description
        </label>
        <textarea
          id="tc-description"
          className="dp-field__input"
          placeholder="Brief description of this Digital Paryty's purpose"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          readOnly={readOnly}
          disabled={readOnly}
          maxLength={500}
          rows={3}
          style={{ resize: 'vertical', minHeight: 60 }}
          data-testid="tc-description-input"
        />
      </div>

      {/* Agent Labels (key=value comma-separated) */}
      <div className="dp-field">
        <label className="dp-field__label" htmlFor="tc-agent-labels">
          Agent Labels
        </label>
        <input
          id="tc-agent-labels"
          className="dp-field__input"
          type="text"
          placeholder="e.g. region=us-east, env=prod (optional)"
          value={agentLabelsText}
          onChange={(e) => setAgentLabelsText(e.target.value)}
          readOnly={readOnly}
          disabled={readOnly}
          data-testid="tc-agent-labels-input"
        />
        <span
          style={{
            fontFamily: 'var(--aef-font-body)',
            fontSize: 'var(--aef-font-size-2xs)',
            color: 'var(--aef-text-secondary)',
            marginTop: 'var(--aef-space-1)',
            display: 'block',
          }}
        >
          Key=value pairs, comma-separated. Agents matching these labels will auto-assign.
        </span>
      </div>

      {/* Enabled Collectors */}
      <div className="dp-field">
        <label className="dp-field__label" htmlFor="tc-collectors">
          Enabled Collectors
        </label>
        <input
          id="tc-collectors"
          className="dp-field__input"
          type="text"
          placeholder="e.g. cpu, memory, disk (optional)"
          value={collectorsText}
          onChange={(e) => setCollectorsText(e.target.value)}
          readOnly={readOnly}
          disabled={readOnly}
          data-testid="tc-collectors-input"
        />
        <span
          style={{
            fontFamily: 'var(--aef-font-body)',
            fontSize: 'var(--aef-font-size-2xs)',
            color: 'var(--aef-text-secondary)',
            marginTop: 'var(--aef-space-1)',
            display: 'block',
          }}
        >
          Leave empty to use all default collectors.
        </span>
      </div>

      {/* Collection Interval */}
      <div className="dp-field">
        <label className="dp-field__label" htmlFor="tc-interval">
          Collection Interval (seconds)
        </label>
        <input
          id="tc-interval"
          className="dp-field__input"
          type="number"
          placeholder="e.g. 30"
          value={intervalSeconds}
          onChange={(e) => setIntervalSeconds(e.target.value)}
          readOnly={readOnly}
          disabled={readOnly}
          min={1}
          data-testid="tc-interval-input"
        />
      </div>

      {/* Sampling Rate */}
      <div className="dp-field">
        <label className="dp-field__label" htmlFor="tc-sampling">
          Sampling Rate (0.0–1.0)
        </label>
        <input
          id="tc-sampling"
          className="dp-field__input"
          type="number"
          placeholder="e.g. 0.5"
          value={samplingRate}
          onChange={(e) => setSamplingRate(e.target.value)}
          readOnly={readOnly}
          disabled={readOnly}
          min={0}
          max={1}
          step={0.1}
          data-testid="tc-sampling-input"
        />
      </div>

      {/* Submit button */}
      {showSubmit && !readOnly && (
        <button
          type="submit"
          className="aef-btn aef-btn-active"
          disabled={!isValid || isSubmitting}
          style={{ opacity: isValid && !isSubmitting ? 1 : 'var(--aef-disabled-opacity)' }}
          data-testid="tc-submit"
        >
          {isSubmitting ? 'Saving…' : submitLabel}
        </button>
      )}
    </form>
  );
});
