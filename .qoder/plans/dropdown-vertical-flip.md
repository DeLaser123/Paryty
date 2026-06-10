# Dropdown Vertical Edge-Flip (Drop-Up / Drop-Down)

## Context

All dropdowns in Paryty (UserMenu avatar, ParytySelect, Header time picker, sidebar profile) open
**downward only** via hardcoded CSS (`top: calc(100% + 4px)`). The `useDropdownEdge` hook only
detects **right-edge** overflow. When a dropdown extends past the bottom of the viewport,
`overflow: hidden` on ancestor containers clips it — the user's profile badge dropdown is getting
cut off.

The user wants **all dropdowns** to intelligently choose drop-up or drop-down based on available
viewport space, the same way `Tooltip` and `ContextMenu` already handle vertical edge detection.

---

## Files Changed (6 files)

| File | Change |
|------|--------|
| `src/hooks/useDropdownEdge.ts` | Return `{ flipRight, flipUp }` instead of `boolean`; add vertical measurement |
| `src/paryty_design_system/design-system-page.css` | New keyframe, `--flip-up` class, combined rule, sidebar CSS replacement, max-height, reduced-motion |
| `src/components/layout/UserMenu.tsx` | Destructure result, dual class application |
| `src/components/common/ParytySelect.tsx` | Destructure result, dual class application |
| `src/components/layout/Header.tsx` | Destructure result, dual class application |
| `src/components/layout/Sidebar.tsx` | Destructure result, dual class application |

---

## Task 1: Update `useDropdownEdge` hook

**File**: `src/hooks/useDropdownEdge.ts`

- Change return type from `boolean` to `{ flipRight: boolean; flipUp: boolean }`
- Export `DropdownEdgeResult` interface
- Add vertical measurement: `rect.bottom > window.innerHeight - 4`
- 4px fudge factor matches existing horizontal threshold

```ts
export interface DropdownEdgeResult {
  flipRight: boolean;
  flipUp: boolean;
}

export function useDropdownEdge(
  containerRef: RefObject<HTMLElement | null>,
  open: boolean,
): DropdownEdgeResult {
  const [result, setResult] = useState<DropdownEdgeResult>({
    flipRight: false,
    flipUp: false,
  });

  useLayoutEffect(() => {
    if (!open || !containerRef.current) {
      setResult({ flipRight: false, flipUp: false });
      return;
    }
    const menu = containerRef.current.querySelector('.aef-dropdown-menu') as HTMLElement | null;
    if (!menu) {
      setResult({ flipRight: false, flipUp: false });
      return;
    }
    const rect = menu.getBoundingClientRect();
    setResult({
      flipRight: rect.right > window.innerWidth - 4,
      flipUp: rect.bottom > window.innerHeight - 4,
    });
  }, [open, containerRef]);

  return result;
}
```

---

## Task 2: CSS changes

**File**: `src/paryty_design_system/design-system-page.css`

### 2a. New `aef-dropdown-in-up` keyframe (after existing `aef-dropdown-in`)

```css
@keyframes aef-dropdown-in-up {
  from { transform: translateY(6px) scaleY(0.92); opacity: 0; }
  to   { transform: translateY(0)    scaleY(1);    opacity: 1; }
}
```

Positive `translateY(6px)` pairs with `transform-origin: bottom center` for correct visual direction.

### 2b. New `--flip-up` modifier class (after existing `--flip`)

```css
.aef-dropdown-menu--flip-up {
  top: auto;
  bottom: calc(100% + 4px);
  transform-origin: bottom center;
  animation-name: aef-dropdown-in-up;
}
```

### 2c. Combined `--flip` + `--flip-up` rule

```css
.aef-dropdown-menu--flip.aef-dropdown-menu--flip-up {
  transform-origin: bottom right;
}
```

### 2d. Replace sidebar profile dropdown CSS

Remove the old conflated `.sidebar-profile-dropdown` + `.sidebar-profile-dropdown.aef-dropdown-menu--flip` rules. Replace with clean single-axis overrides:

```css
.sidebar-profile-dropdown {
  left: calc(100% + 4px);
  right: auto;
  top: auto;
  bottom: 0;
  transform-origin: left bottom;
}
.sidebar-profile-dropdown.aef-dropdown-menu--flip {
  left: auto;
  right: calc(100% + 4px);
  transform-origin: right bottom;
}
.sidebar-profile-dropdown.aef-dropdown-menu--flip-up {
  bottom: auto;
  top: 0;
  transform-origin: left top;
}
.sidebar-profile-dropdown.aef-dropdown-menu--flip.aef-dropdown-menu--flip-up {
  transform-origin: right top;
}
```

### 2e. Add max-height safety constraint to `.aef-dropdown-menu`

```css
.aef-dropdown-menu {
  /* ...existing... */
  max-height: calc(100vh - var(--aef-space-8));
  overflow-y: auto;
}
```

Ensures menus that don't fit in either direction become scrollable rather than clipped.

### 2f. Add reduced-motion entry for new animation

In the existing `@media (prefers-reduced-motion: reduce)` block, add `.aef-dropdown-menu--flip-up` alongside `.aef-dropdown-menu`.

---

## Task 3: Update consumer components

All four consumers follow the same pattern. Destroy the old single boolean and destructure both
signals, then apply both CSS classes independently.

### 3a. `UserMenu.tsx`
```
const flip = useDropdownEdge(ref, open)
→ const { flipRight, flipUp } = useDropdownEdge(ref, open)

clsx('aef-dropdown-menu', flip && 'aef-dropdown-menu--flip')
→ clsx('aef-dropdown-menu', flipRight && 'aef-dropdown-menu--flip', flipUp && 'aef-dropdown-menu--flip-up')
```

### 3b. `ParytySelect.tsx`
Same pattern as UserMenu.

### 3c. `Header.tsx`
```
const dropdownFlip = useDropdownEdge(dropdownRef, dropdownOpen)
→ const { flipRight, flipUp } = useDropdownEdge(dropdownRef, dropdownOpen)

clsx('aef-dropdown-menu', dropdownFlip && 'aef-dropdown-menu--flip')
→ clsx('aef-dropdown-menu', flipRight && 'aef-dropdown-menu--flip', flipUp && 'aef-dropdown-menu--flip-up')
```

### 3d. `Sidebar.tsx`
```
const profileFlip = useDropdownEdge(profileRef, profileOpen)
→ const { flipRight, flipUp } = useDropdownEdge(profileRef, profileOpen)

clsx('aef-dropdown-menu', 'sidebar-profile-dropdown', profileFlip && 'aef-dropdown-menu--flip')
→ clsx('aef-dropdown-menu', 'sidebar-profile-dropdown',
  flipRight && 'aef-dropdown-menu--flip', flipUp && 'aef-dropdown-menu--flip-up')
```

---

## Animation `transform-origin` Matrix

| `--flip` | `--flip-up` | `transform-origin` | Animation |
|----------|-------------|---------------------|-----------|
| — | — | `top center` | `aef-dropdown-in` |
| yes | — | `top right` | `aef-dropdown-in` |
| — | yes | `bottom center` | `aef-dropdown-in-up` |
| yes | yes | `bottom right` | `aef-dropdown-in-up` |

---

## Verification

1. **TypeScript**: `cd paryty-v1.0/frontend && npx tsc --noEmit`
2. **Build**: `cd paryty-v1.0/frontend && npm run build`
3. **Tests**: `cd paryty-v1.0/frontend && npx vitest run`
4. **Visual**: `npm run dev` — verify:
   - Header UserMenu drops up when near bottom of viewport
   - ParytySelect drops up when near bottom of viewport
   - Header time picker drops up when near bottom of viewport
   - Sidebar profile dropdown flips both axes independently when near right/bottom edges
   - All dropdowns animate correctly in all 4 direction combinations
   - Max-height cap works (tall dropdowns become scrollable)
   - Horizontal flip still works correctly alongside vertical flip
