# Figma Shell Bars Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Apply the Figma sidebar and playback bar treatment to the existing app shell while preserving current shell and playback behavior.

**Architecture:** Keep the work inside existing `packages/ui` shell components. `SidebarNav` owns navigation presentation, `PlayerBar` owns playback presentation, and tests verify visible behavior plus key class contracts.

**Tech Stack:** React 19, TypeScript, Tailwind 4, lucide-react, Vitest, Testing Library.

---

### Task 1: Sidebar Figma Styling

**Files:**
- Modify: `packages/ui/src/layout/SidebarNav.tsx`
- Test: `packages/ui/src/layout/AppShell.test.tsx`

- [ ] **Step 1: Write failing tests**

Add tests that assert the sidebar renders the Figma brand copy, removes `Help (soon)`, and gives the radio link the active Figma classes:

```tsx
expect(screen.getByText('Premium Account')).toBeTruthy()
expect(screen.queryByText('Help (soon)')).toBeNull()
expect(screen.getByRole('link', { name: 'Radio Stations' }).className).toContain('[&.active]:bg-[rgba(40,54,70,0.5)]')
```

- [ ] **Step 2: Run tests to verify failure**

Run: `pnpm --dir packages/ui test -- AppShell.test.tsx`

Expected: FAIL because subtitle still says `The Modern Craftsman`, help is still present, or active class contract is missing.

- [ ] **Step 3: Implement sidebar styling**

Update `SidebarNav.tsx` and `AppBrand.tsx` only if the brand component needs the subtitle text. Use existing lucide icons. Do not import Figma asset URLs.

- [ ] **Step 4: Run tests to verify pass**

Run: `pnpm --dir packages/ui test -- AppShell.test.tsx`

Expected: PASS.

### Task 2: Playback Bar Figma Styling and Radio Live Labels

**Files:**
- Modify: `packages/ui/src/layout/PlayerBar.tsx`
- Test: `packages/ui/src/layout/PlayerBar.test.tsx`

- [ ] **Step 1: Write failing tests**

Add tests that start radio playback, assert progress labels `LIVE` and `--:--`, assert seek disabled, and assert the player footer has the 72px Figma height class:

```tsx
expect(screen.getByText('LIVE')).toBeTruthy()
expect(screen.getByText('--:--')).toBeTruthy()
expect((screen.getByLabelText('Seek') as HTMLInputElement).disabled).toBe(true)
expect(screen.getByRole('contentinfo').className).toContain('h-[72px]')
```

- [ ] **Step 2: Run tests to verify failure**

Run: `pnpm --dir packages/ui test -- PlayerBar.test.tsx`

Expected: FAIL because current footer height/classes and radio live labels do not match the Figma contract.

- [ ] **Step 3: Implement playback bar styling**

Update `PlayerBar.tsx` to use the Figma footer layout, colors, 48px artwork, 40px rounded-square play button, live radio progress labels, and stable radio quality fallback.

- [ ] **Step 4: Run tests to verify pass**

Run: `pnpm --dir packages/ui test -- PlayerBar.test.tsx`

Expected: PASS.

### Task 3: Focused Verification

**Files:**
- Verify: `packages/ui/src/layout/SidebarNav.tsx`
- Verify: `packages/ui/src/layout/AppBrand.tsx`
- Verify: `packages/ui/src/layout/PlayerBar.tsx`
- Verify: `packages/ui/src/layout/AppShell.test.tsx`
- Verify: `packages/ui/src/layout/PlayerBar.test.tsx`

- [ ] **Step 1: Run package tests**

Run: `pnpm --dir packages/ui test`

Expected: PASS.

- [ ] **Step 2: Run focused Biome check**

Run: `pnpm --dir packages/ui exec biome check src/layout/SidebarNav.tsx src/layout/AppBrand.tsx src/layout/PlayerBar.tsx src/layout/AppShell.test.tsx src/layout/PlayerBar.test.tsx`

Expected: PASS.

- [ ] **Step 3: Update graph**

Run: `graphify update .`

Expected: exit 0.
