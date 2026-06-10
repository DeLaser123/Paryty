/**
 * UserMenu — avatar + dropdown for account actions.
 *
 * Shows the user's initials in a circular avatar.
 * Dropdown provides Sign out action.
 *
 * @module components/layout/UserMenu
 */

import { useState, useRef, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { LogOut } from 'lucide-react';
import clsx from 'clsx';
import { useAuthStore } from '../../stores/authStore';
import { useDropdownEdge } from '../../hooks/useDropdownEdge';
import { Tooltip } from '../common/Tooltip';

/**
 * User avatar with dropdown menu.
 *
 * Clicking the avatar toggles a dropdown with:
 * - Settings (navigates to /settings)
 * - Sign out (calls authStore.logout)
 */
export function UserMenu() {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const { flipRight, flipUp } = useDropdownEdge(ref, open);
  const navigate = useNavigate();
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);

  // Close on click outside
  useEffect(() => {
    if (!open) return;
    const handleOut = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', handleOut);
    return () => document.removeEventListener('mousedown', handleOut);
  }, [open]);

  // Close on Escape
  useEffect(() => {
    if (!open) return;
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [open]);

  const handleSignOut = useCallback(async () => {
    setOpen(false);
    await logout();
    navigate('/login');
  }, [logout, navigate]);

  // Compute initials from user name
  const initials = user?.name
    ? user.name
        .split(' ')
        .map((n) => n[0])
        .join('')
        .toUpperCase()
        .slice(0, 2)
    : 'U';

  return (
    <div className="aef-dropdown" ref={ref} data-testid="user-menu">
      <button
        className="user-menu__avatar"
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="true"
        aria-expanded={open}
        aria-label="User menu"
        data-testid="user-menu-trigger"
      >
        {initials}
      </button>
      {open && (
        <div className={clsx('aef-dropdown-menu', flipRight && 'aef-dropdown-menu--flip', flipUp && 'aef-dropdown-menu--flip-up')} role="menu" data-testid="user-menu-dropdown">
          <div className="aef-dropdown-item" role="menuitem" onClick={handleSignOut}>
            <LogOut size={14} />
            <Tooltip label="Sign out">
              <span className="aef-dropdown-item__label">Sign out</span>
            </Tooltip>
          </div>
        </div>
      )}
    </div>
  );
}
