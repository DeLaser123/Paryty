/**
 * TwinSettingsPage — per-Digital-Paryty settings.
 *
 * Allows modifying the Digital Paryty's name, system label,
 * and managing its abilities.
 *
 * @module pages/TwinSettingsPage
 */

import { useState, useCallback, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Save, ArrowLeft, Trash2 } from 'lucide-react';
import { useToastStore } from '../stores/toastStore';
import { useDashboardStore } from '../stores/dashboardStore';
import { fetchTwinById, updateTwin } from '../api/twins';
import type { TwinDetails } from '../types/digitalParyty';

export function TwinSettingsPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const addToast = useToastStore((s) => s.addToast);
  const deleteTwin = useDashboardStore((s) => s.deleteTwin);
  const fetchTwins = useDashboardStore((s) => s.fetchTwins);

  const [twin, setTwin] = useState<TwinDetails | null>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);

  // Fetch twin data on mount
  useEffect(() => {
    if (!id) return;
    setIsLoading(true);
    fetchTwinById(id)
      .then((data) => {
        setTwin(data);
        setName(data.name);
        setDescription(data.description || '');
      })
      .catch((err) => {
        addToast({ type: 'error', message: `Failed to load twin: ${err instanceof Error ? err.message : 'Unknown error'}` });
      })
      .finally(() => setIsLoading(false));
  }, [id, addToast]);

  const handleSave = useCallback(async () => {
    if (!id || !name.trim()) return;
    setIsSaving(true);
    try {
      await updateTwin(id, { name: name.trim(), description: description.trim() });
      addToast({ type: 'success', message: 'Settings saved.' });
    } catch (err) {
      addToast({ type: 'error', message: `Failed to save: ${err instanceof Error ? err.message : 'Unknown error'}` });
    } finally {
      setIsSaving(false);
    }
  }, [id, name, description, addToast]);

  const handleDelete = useCallback(async () => {
    if (!id) return;
    setIsDeleting(true);
    try {
      await deleteTwin(id);
      addToast({ type: 'success', message: 'Digital Paryty deleted.' });
      await fetchTwins();
      navigate('/');
    } catch (err) {
      addToast({ type: 'error', message: `Failed to delete: ${err instanceof Error ? err.message : 'Unknown error'}` });
    } finally {
      setIsDeleting(false);
      setShowDeleteConfirm(false);
    }
  }, [id, deleteTwin, fetchTwins, navigate, addToast]);

  if (isLoading) {
    return (
      <div className="dp-page" data-testid="twin-settings-page">
        <div className="dp-page__inner" style={{ maxWidth: 600 }}>
          <p style={{ color: 'var(--aef-text-secondary)', fontFamily: 'var(--aef-font-body)', fontSize: 12 }}>
            Loading settings…
          </p>
        </div>
      </div>
    );
  }

  if (!twin) {
    return (
      <div className="dp-page" data-testid="twin-settings-page">
        <div className="dp-page__inner" style={{ maxWidth: 600 }}>
          <p style={{ color: 'var(--aef-text-error)', fontFamily: 'var(--aef-font-body)', fontSize: 12 }}>
            Twin not found.
          </p>
        </div>
      </div>
    );
  }

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
            <p className="dp-header__sub">Manage settings for {twin.name}</p>
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
          <label className="dp-field__label" htmlFor="tw-set-system">Description</label>
          <input
            id="tw-set-system" className="dp-field__input" type="text"
            placeholder="e.g. Payment Gateway v3" value={description}
            onChange={(e) => setDescription(e.target.value)} maxLength={80}
            data-testid="tw-settings-system"
          />
        </div>

        <div style={{ display: 'flex', gap: 'var(--aef-space-3)', alignItems: 'center' }}>
          <button
            className="aef-btn aef-btn-active"
            onClick={handleSave}
            disabled={isSaving || !name.trim()}
            style={{ opacity: isSaving || !name.trim() ? 0.4 : 1 }}
            data-testid="tw-settings-save"
          >
            <Save size={14} /> {isSaving ? 'Saving…' : 'Save Changes'}
          </button>

          {!showDeleteConfirm ? (
            <button
              className="aef-btn aef-btn-inactive"
              onClick={() => setShowDeleteConfirm(true)}
              style={{ marginLeft: 'auto', color: 'var(--aef-danger)' }}
              data-testid="tw-settings-delete"
            >
              <Trash2 size={14} /> Delete Twin
            </button>
          ) : (
            <div style={{
              marginLeft: 'auto',
              display: 'flex',
              gap: 'var(--aef-space-2)',
              alignItems: 'center',
              padding: 'var(--aef-space-2) var(--aef-space-3)',
              background: 'rgba(239, 68, 68, 0.1)',
              borderRadius: 'var(--aef-radius-control)',
              border: '1px solid rgba(239, 68, 68, 0.3)',
            }}>
              <span style={{ fontSize: 11, fontFamily: 'var(--aef-font-body)', color: 'var(--aef-text-primary)' }}>
                Confirm delete?
              </span>
              <button
                className="aef-btn aef-btn-inactive"
                onClick={handleDelete}
                disabled={isDeleting}
                style={{ fontSize: 11, color: 'var(--aef-danger)' }}
              >
                {isDeleting ? 'Deleting…' : 'Yes, delete'}
              </button>
              <button
                className="aef-btn aef-btn-inactive"
                onClick={() => setShowDeleteConfirm(false)}
                style={{ fontSize: 11 }}
              >
                Cancel
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
