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
  const [systemLabel, setSystemLabel] = useState(initialValues?.config?.tenantLabel ?? '');
  const [agentIdsText, setAgentIdsText] = useState(
    initialValues?.config?.agentIds?.join(', ') ?? '',
  );

  const isValid = name.trim().length > 0;

  const handleSubmit = useCallback(
    (e: React.FormEvent) => {
      e.preventDefault();
      if (!isValid || isSubmitting) return;

      const agentIds = agentIdsText
        .split(',')
        .map((id) => id.trim())
        .filter((id) => id.length > 0);

      onSubmit({
        name: name.trim(),
        description: description.trim() || undefined,
        config: {
          agentIds: agentIds.length > 0 ? agentIds : undefined,
          tenantLabel: systemLabel.trim() || undefined,
        },
      });
    },
    [name, description, systemLabel, agentIdsText, isValid, isSubmitting, onSubmit],
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
          Digital Paryty name <span style={{ color: 'var(--aef-error)' }}>*</span>
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

      {/* System Label */}
      <div className="dp-field">
        <label className="dp-field__label" htmlFor="tc-system">
          Software system
        </label>
        <input
          id="tc-system"
          className="dp-field__input"
          type="text"
          placeholder="e.g. Payment Gateway v3"
          value={systemLabel}
          onChange={(e) => setSystemLabel(e.target.value)}
          readOnly={readOnly}
          disabled={readOnly}
          maxLength={80}
          data-testid="tc-system-input"
        />
      </div>

      {/* Agent IDs (comma-separated) */}
      <div className="dp-field">
        <label className="dp-field__label" htmlFor="tc-agents">
          Agent IDs
        </label>
        <input
          id="tc-agents"
          className="dp-field__input"
          type="text"
          placeholder="Comma-separated agent IDs (optional)"
          value={agentIdsText}
          onChange={(e) => setAgentIdsText(e.target.value)}
          readOnly={readOnly}
          disabled={readOnly}
          data-testid="tc-agents-input"
        />
        <span
          style={{
            fontFamily: 'var(--aef-font-body)',
            fontSize: 10,
            color: 'var(--aef-text-secondary)',
            marginTop: 'var(--aef-space-1)',
            display: 'block',
          }}
        >
          Leave empty to auto-assign agents when they connect.
        </span>
      </div>

      {/* Submit button */}
      {showSubmit && !readOnly && (
        <button
          type="submit"
          className="aef-btn aef-btn-active"
          disabled={!isValid || isSubmitting}
          style={{ opacity: isValid && !isSubmitting ? 1 : 0.4 }}
          data-testid="tc-submit"
        >
          {isSubmitting ? 'Saving…' : submitLabel}
        </button>
      )}
    </form>
  );
});
