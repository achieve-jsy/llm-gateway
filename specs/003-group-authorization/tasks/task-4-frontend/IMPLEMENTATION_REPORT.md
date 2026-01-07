# Implementation Report — Task 4: Frontend — Model Group Open Mode + Authorization UI

## Summary

Successfully implemented the frontend UI for model group open_mode management and authorization (entitlement) management, matching the spec at `specs/003-group-authorization/spec.md` section 11.

## Files Changed

| File | Operation | Lines |
|------|-----------|-------|
| `web/src/types/group.ts` | Modified | +25 lines: Added `OpenMode` type, `open_mode` field to `Group`, `GroupEntitlement` interface, `GrantEntitlementRequest`, `EntitlementListResponse`, `EntitlementScopeType` |
| `web/src/api/groups.ts` | Modified | +20 lines: Added 4 API methods (`setOpenMode`, `listEntitlements`, `grantEntitlement`, `revokeEntitlement`) to `groupsApi` object |
| `web/src/views/GroupListPage.vue` | Modified | +60 lines: Added open_mode column with popconfirm toggle, "授权" action button for controlled groups, `GroupEntitlementDialog` integration, `handleToggleOpenMode` function, role-based visibility via `canManageEntitlements` computed |
| `web/src/components/GroupEntitlementDialog.vue` | Created | 359 lines: Full drawer component with two tabs (studio authorization + dept authorization), add/revoke dialogs, filter by studio, loading states, error handling |

## Implementation Details

### 1. Types (`types/group.ts`)

- `OpenMode = 'public' | 'controlled'` — Union type for group open mode
- `Group.open_mode: OpenMode` — Added to existing `Group` interface
- `GroupEntitlement` — Full interface matching backend spec (`id`, `group_id`, `scope_type`, `scope_id`, `scope_name`, `enabled`, `granted_by`, timestamps)
- `GrantEntitlementRequest` — Request body for granting authorization
- `EntitlementListResponse` — Response wrapper with `items` array

### 2. API Layer (`api/groups.ts`)

Added 4 methods following existing `groupsApi` object pattern:

- `setOpenMode(groupId, openMode)` — `PUT /api/v1/groups/:id/open-mode`
- `listEntitlements(groupId, scopeType?)` — `GET /api/v1/groups/:id/entitlements`
- `grantEntitlement(groupId, data)` — `POST /api/v1/groups/:id/entitlements`
- `revokeEntitlement(groupId, entitlementId)` — `DELETE /api/v1/groups/:id/entitlements/:eid`

### 3. Group List Page (`views/GroupListPage.vue`)

- **Open mode column**: `el-tag` (success=公开, warning=受控) with `el-popconfirm` for admin to toggle
- **Authorization button**: Visible only for controlled groups when `canManageEntitlements` is true
- **Role control**: `canManageEntitlements = computed(() => authStore.isAdmin)` — only admin can manage
- **Drawer integration**: `GroupEntitlementDialog` opens with selected group's id and name

### 4. Authorization Drawer (`components/GroupEntitlementDialog.vue`)

**Drawer** (`el-drawer`, 680px, right-to-left):
- **Tab 1 "工作室授权"**: Lists studio entitlements with add/revoke. Add uses nested dialog with studio selector (filters out already-granted studios).
- **Tab 2 "部门开通"**: Lists dept entitlements with studio filter dropdown. Add uses nested dialog with dept selector (filters to selected studio's ungranted depts).
- **Data loading**: Parallel fetch on open (entitlements + studios + departments via `Promise.all`)
- **Computed filters**: `availableStudios`, `availableDepts`, `filteredDeptEntitlements` all reactively filter granted items
- **Error handling**: `ElMessage.error` on all failure paths, `try/catch/finally` for loading state

## Design Decisions

1. **Drawer vs Dialog**: Used `el-drawer` (side panel) instead of `el-dialog` (modal) because authorization management involves browsing/switching between studio and dept tabs — drawer provides more space and matches the OrgTreePage pattern already in use.

2. **Nested dialogs**: Used `el-dialog` inside the drawer for add-studio and add-dept flows — simpler than adding more tabs, and matches the OrgTreePage pattern of nested dialogs within drawers.

3. **Role guard**: Only admin can see the authorization management UI. Route `/admin/groups` already requires `requiresAdmin: true`. Additional `canManageEntitlements` computed in the page provides defense-in-depth.

4. **v-model:visible pattern**: Drawer uses computed getter/setter for `localVisible` that emits `update:visible` on set, matching the parent's `v-model:visible` binding. The `closed` emit fires on `handleDrawerClose` for parent cleanup.

## Verification

- [x] All 4 files pass TypeScript LSP diagnostics with zero errors
- [x] Template structure verified (proper nesting, no duplicate content)
- [x] Type imports match across files
- [x] Props interface matches between `GroupListPage` (passes `groupId`/`groupName`) and `GroupEntitlementDialog` (receives `groupId`/`groupName`)
- [x] API endpoints match spec.md section 11 contract

## Not Done (Future Scope)

- Studio_admin/dept-admin partial visibility (current route is admin-only)
- Real-time KeyCache invalidation feedback (backend handles via event, frontend just shows success message)
- Batch authorization operations
- Authorization audit log view
