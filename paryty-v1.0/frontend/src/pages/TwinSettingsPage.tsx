/**
 * TwinSettingsPage — per-Digital-Paryty settings.
 *
 * Allows modifying the Digital Paryty's name, system label,
 * and managing its abilities.
 *
 * @module pages/TwinSettingsPage
 */

import { useState, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Save, ArrowLeft } from 'lucide-react';
import { useToastStore } from '../stores/toastStore';

export function TwinSettingsPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const addToast = useToastStore((s) => s.addToast);

  const [name, setName] = useState(id ?? '');
  const [systemLabel, setSystemLabel] = useState('');
  const [isSaving, setIsSaving] = useState(false);

  const handleSave = useCallback(async () => {
    if (!name.trim()) return;
    setIsSaving(true);
    try {
      // In production: PUT /api/v1/twins/:id
      addToast({ type: 'success', message: 'Settings saved.' });
    } catch {
      addToast({ type: 'error', message: 'Failed to save settings.' });
    } finally {
      setIsSaving(false);
    }
  }, [name, addToast]);

  return (
    <div className="dp-page" data-testid="twin-settings-page">
      <div className="dp-page__inner" style={{ maxWidth: 600 }}>
        <div className="dp-header">
          <div className="dp-header__title-block">
            <button
              className="aef-btn aef-btn-inactive"
              onClick={() => navigate(`/twins/${id}`)}
              style={{ marginBottom: 'var(--aef-space-2)' }}
            >
              <ArrowLeft size={14} /> Back
            </button>
            <h1 className="dp-header__title">Digital Paryty Settings</h1>
            <p className="dp-header__sub">Manage settings for {id}</p>
          </div>
        </div>

        <div className="dp-field">
          <label className="dp-field__label" htmlFor="tw-set-name">Name</label>
          <input
            id="tw-set-name" className="dp-field__input" type="text"
            value={name} onChange={(e) => setName(e.target.value)}
            maxLength={80} data-testid="tw-settings-name"
          />
        </div>

        <div className="dp-field">
          <label className="dp-field__label" htmlFor="tw-set-system">System Label</label>
          <input
            id="tw-set-system" className="dp-field__input" type="text"
            placeholder="e.g. Payment Gateway v3" value={systemLabel}
            onChange={(e) => setSystemLabel(e.target.value)} maxLength={80}
            data-testid="tw-settings-system"
          />
        </div>

        <button
          className="aef-btn aef-btn-active"
          onClick={handleSave}
          disabled={isSaving || !name.trim()}
          style={{ opacity: isSaving || !name.trim() ? 0.4 : 1 }}
          data-testid="tw-settings-save"
        >
          <Save size={14} /> {isSaving ? 'Saving…' : 'Save Changes'}
        </button>
      </div>
    </div>
  );
}
