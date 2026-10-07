# UI design reference

Status: approved. Date: 2026-10-07.

This document fixes the structure, density and base tokens of the web client. It is the written form of the approved mockups (four screens: desktop light with a task selected, desktop dark with nothing selected, phone list, phone task detail). Behaviour lives in `SPEC.md`, section 9; if the two disagree, `SPEC.md` wins and this file is corrected.

Values marked *proposed* were not shown in the mockups and may be adjusted during phase 3.

## 1. Principles

- Compact and quiet: little white space, one accent colour, no decoration.
- The task title is the content; everything else in a row is secondary and smaller.
- Priority, overdue state and completion are never conveyed by colour alone: each also has a label, icon or text style.
- Text contrast of at least 4.5:1 against its background in both themes; 3:1 for control outlines.
- Original visual identity. Other task apps are a reference for layout density only; do not copy their colours, icons or assets.

## 2. Layout

### Desktop (pointer devices)

Three panes in a row:

| Pane | Width | Notes |
|---|---|---|
| Sidebar | 232 px | Surface background, right border |
| Task list | Fills the rest, minimum 560 px | Padding 16 px vertical, 24 px horizontal |
| Task detail | 360 px | Left border. Rendered only while a task is selected; when none is, the list takes the space |

Sidebar, top to bottom: product name; Inbox, Today, Next 7 days (each with its open-task count); "Lists" heading with a "new list" button, then the lists with a colour dot and count; "Tags" heading, then the tags; pinned to the bottom, separated by a border: Trash, Settings.

Task list, top to bottom: header (view title, secondary info such as the date or task count, search field aligned right); quick-add field; task sections; the collapsed "Completed" section last.

Task detail, top to bottom: header bar (complete checkbox, due date button, priority button, close button); title; description; checklist with an "add item" field; a two-column field grid (List, Tags, Repeat, Reminders); footer bar (creation date, "Skip this occurrence" for recurring tasks, delete button).

### Phone (touch devices)

Single column. The sidebar becomes a drawer opened from a menu button in the header. Selecting a task opens the detail full screen with a back button. The quick-add field sits at the bottom of the list screen.

### Breakpoints (*proposed*)

| Viewport width | Layout |
|---|---|
| Below 768 px | Single column, drawer, full-screen detail |
| 768–1099 px | Sidebar and list; detail slides over the list from the right |
| 1100 px and above | Three panes |

## 3. Typography

Typeface: IBM Plex Sans (weights 400, 500, 600), with `system-ui, sans-serif` as fallback. Self-host the font files; do not load them from a third-party CDN.

| Role | Pointer | Touch |
|---|---|---|
| Body, task title in a row | 14 / 20, 400 | 16 / 22, 400 |
| View title | 20 / 28, 600 | 18 / 22, 600 |
| Detail title | 18 / 26, 600 | 20 / 28, 600 |
| Secondary row text (list, due, counts) | 12, 400 | 13 / 18, 400 |
| Section heading | 12, 600 | 13, 600 |
| Selected navigation item, selected row title | 500 | 500 |

Sizes are font-size / line-height in px.

## 4. Colour tokens

| Token | Light | Dark |
|---|---|---|
| Background | `#FFFFFF` | `#14171A` |
| Surface (sidebar, quick-add field) | `#F4F5F7` | `#1A1E22` |
| Border (panes, inputs) | `#E3E6EA` | `#2A3036` |
| Row divider | `#EEF0F3` | `#22272C` |
| Text | `#1C2024` | `#E6E8EB` |
| Text, secondary (description, selected-row meta) | `#414A55` | `#C3CAD2` |
| Text, muted (meta, headings, icons) | `#5B6470` | `#9AA4B0` |
| Text, completed | `#6B7480` | `#8A94A0` |
| Accent | `#0F766E` | `#2DD4BF` |
| Accent, text and icons | `#0B5A54` | `#99F6E4` |
| Selected background (navigation item, row) | `#DDEFEC` | `#1D3532` |
| Danger, overdue | `#B42318` | `#F97066` |
| Priority high | `#B42318` | `#F97066` |
| Priority medium | `#B45309` | `#FDB022` |
| Priority low | `#1D4ED8` | `#84ADFF` |
| Priority none (checkbox outline) | `#8A939E` | `#6F7985` |
| Tag chip background / text | `#EEF0F3` / `#414A55` | `#252B31` / `#C3CAD2` |
| Checked box fill / check mark | `#6B7480` / `#FFFFFF` | `#4A535D` / `#E6E8EB` |

Theme follows `prefers-color-scheme`; light when the system states no preference. Implement the tokens as CSS custom properties so both themes share one set of component styles.

List colour dots use a small fixed palette chosen by the user; the mockups use the accent, priority-medium and priority-low values.

## 5. Components and metrics

### Pointer devices

| Element | Metrics |
|---|---|
| Navigation item | Height 32, padding 0 10, gap 10, radius 6, icon 16 |
| Sidebar heading | Height 28 |
| Task row | Height 36, padding 0 12, gap 10, bottom divider |
| Selected task row | Selected background, radius 6, no divider |
| Section heading | Height 28, chevron 16, label and count |
| Quick-add field | Height 36, radius 6, surface background, plus icon |
| Search field | Height 32, width 220, 1 px border, radius 6 |
| Checkbox | 16 × 16, radius 4, 1.5 px outline in the priority colour |
| Tag chip | 12 px text, padding 1 6, radius 4 |
| Detail header / footer | Height 48 / 44 |
| Detail field row | Height 32; label column 96 wide |
| Icon button | 28 × 28, radius 6, icon 16 |

### Touch devices

| Element | Metrics |
|---|---|
| Any pressable target | At least 44 × 44 |
| Header bar | Height 56 |
| Task row | Minimum height 52, two lines: title, then meta |
| Section heading | Height 36 |
| Checkbox | 20 × 20 drawn, radius 5, 2 px outline, inside a 44 × 44 target |
| Detail field row / checklist row | Minimum height 48 |
| Quick-add field | Height 48, radius 10, margin 8 12 16 |

### Task row content, left to right

1. Checkbox; outline colour shows priority, and the accessible name states it ("Complete task, high priority").
2. Title, single line, truncated with an ellipsis.
3. Checklist progress as "done/total", only if the task has a checklist.
4. Repeat icon, only if recurring.
5. Reminder icon, only if it has a pending reminder.
6. Tag chips.
7. List name, only in views that mix lists (Today, Next 7 days, tag, search).
8. Due, right-aligned in a fixed-width column: time for today, weekday and date otherwise; danger colour when overdue.

On touch devices items 3 and 6–8 collapse into the meta line under the title, separated by " · "; the icons stay at the right edge.

### Sections in a view

- Today: "Overdue" (heading in the danger colour), "Today", then "Completed".
- A list: its open tasks in manual order with no heading, then "Completed".
- "Completed" is collapsed by default and shows its count. Expanded entries use the completed text colour with a line-through, a filled checkbox and the completion time on the right.

### Icons

Line icons, 16 px, 1.5 px stroke, round caps and joins, `currentColor`. Use one consistent open-source icon set; no emoji. Icons that carry meaning on their own (repeat, reminder) need an accessible name; decorative ones are hidden from assistive technology.

## 6. States not shown in the mockups (*proposed*)

- Hover on a row or navigation item: surface background.
- Keyboard focus: 2 px accent outline, visible in both themes.
- Empty view: one muted line of text under the quick-add field; no illustration.
- Loading: keep the previous content and avoid layout shift; no full-screen spinner.
- Undo toast: bottom centre on pointer devices, above the quick-add field on touch devices.
