---
name: dbtools
description: Multi-Engine Database Migration Authority & Local Dev-Loop
colors:
  primary: "#3b82f6"
  primary-glow: "#06b6d4"
  accent-emerald: "#10b981"
  accent-amber: "#f59e0b"
  neutral-bg: "#0b0f19"
  surface-card: "#111827"
  code-bg: "#030712"
  text-primary: "#f9fafb"
  text-secondary: "#9ca3af"
  text-muted: "#6b7280"
  border: "rgba(255, 255, 255, 0.08)"
  border-hover: "rgba(59, 130, 246, 0.4)"
typography:
  display:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif"
    fontSize: "clamp(2.25rem, 5vw, 3.25rem)"
    fontWeight: 800
    lineHeight: 1.15
    letterSpacing: "-0.02em"
  headline:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif"
    fontSize: "2.2rem"
    fontWeight: 700
    lineHeight: 1.2
    letterSpacing: "-0.01em"
  title:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif"
    fontSize: "1.25rem"
    fontWeight: 600
    lineHeight: 1.4
    letterSpacing: "normal"
  body:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.6
    letterSpacing: "normal"
  label:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif"
    fontSize: "0.85rem"
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: "0.02em"
  mono:
    fontFamily: "'JetBrains Mono', ui-monospace, SFMono-Regular, monospace"
    fontSize: "0.9rem"
    fontWeight: 500
    lineHeight: 1.5
    letterSpacing: "normal"
rounded:
  xs: "4px"
  sm: "6px"
  md: "8px"
  lg: "10px"
  xl: "14px"
  full: "9999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "24px"
  xl: "32px"
  2xl: "48px"
  3xl: "80px"
components:
  button-primary:
    backgroundColor: "rgba(255, 255, 255, 0.08)"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.sm}"
    padding: "8px 14px"
  button-primary-hover:
    backgroundColor: "rgba(255, 255, 255, 0.16)"
  feature-card:
    backgroundColor: "{colors.surface-card}"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.xl}"
    padding: "28px"
  badge-pill:
    backgroundColor: "rgba(16, 185, 129, 0.1)"
    textColor: "#6ee7b7"
    rounded: "{rounded.full}"
    padding: "6px 14px"
---

# Design System

## Overview

**Creative North Star: "The Cryptographic Vault"**

`dbtools` visual language embodies precision, immutability, and deterministic technical authority. Engineered for developers, platform teams, and autonomous AI agents, the interface draws aesthetic inspiration from mission-critical control panels and cryptographic hardware: deep obsidian surfaces, razor-sharp hairline borders, high-legibility typography, and focused neon telemetry signals.

Every visual element communicates state with mathematical clarity. Decorative noise is eliminated in favor of structured data, monospaced ledger streams, and subtle glassmorphic depth.

**Key Characteristics:**
- **Obsidian & Telemetry**: Deep dark canvas punctuated by electric blue focal points, cyan telemetry accents, and emerald ledger verifications.
- **Hairline Precision**: 1px subtle boundary strokes (`rgba(255, 255, 255, 0.08)`) that highlight on active focus or hover (`rgba(59, 130, 246, 0.4)`).
- **Dual-Layer Typography**: High-contrast geometric sans (`Inter`) for crisp structural hierarchy paired with monospaced code blocks (`JetBrains Mono`) for logs, CLI commands, and cryptographic hashes.

## Colors

High-contrast monochrome dark core with focused neon telemetry accents.

### Primary
- **Electric Vault Blue** (`#3b82f6`): Primary brand anchor, hero gradient starter, and interactive focus states.
- **Telemetry Cyan** (`#06b6d4`): Secondary gradient bridge, command line prompts, and status telemetry markers.

### Secondary
- **Ledger Emerald** (`#10b981`): Applied migrations, clean drift-free verification statuses (`✓ 0 drift detected`), and success indicators.
- **Safeguard Amber** (`#f59e0b`): Target protection warnings, interactive confirmations, and non-blocking audit notices.

### Neutral
- **Obsidian Black Canvas** (`#0b0f19`): Main viewport background.
- **Card Surface Slate** (`#111827` / `rgba(17, 24, 39, 0.7)`): Glassmorphic container and card backdrop.
- **Terminal Pitch** (`#030712`): Code blocks, command prompt boxes, and raw data outputs.
- **Hairline Border** (`rgba(255, 255, 255, 0.08)`): Crisp boundary divider.
- **Text Primary** (`#f9fafb`): High-contrast crisp white for titles and primary data.
- **Text Secondary** (`#9ca3af`): Muted gray for descriptions, secondary metadata, and documentation body.
- **Text Muted** (`#6b7280`): Deeper gray for timestamps, breadcrumbs, and subtle footer items.

### Named Rules
**The Telemetry Purpose Rule.** Accent colors are strictly semantic and never decorative filler. Blue signals navigation/action, Cyan signals prompt/telemetry, Emerald signals immutable cryptographic truth, and Amber signals protected execution boundaries.

## Typography

**Display Font:** Inter (fallback: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif)
**Body Font:** Inter
**Label/Mono Font:** JetBrains Mono (fallback: ui-monospace, SFMono-Regular, monospace)

**Character:** Razor-sharp sans-serif hierarchy paired with monospaced typographic data density.

### Hierarchy
- **Display** (800 weight, `clamp(2.25rem, 5vw, 3.25rem)`, 1.15 line-height, `-0.02em` tracking): Page and hero titles. Employs gradient text fill on key emphasis spans.
- **Headline** (700 weight, `2.2rem`, 1.2 line-height, `-0.01em` tracking): Major section headers.
- **Title** (600 weight, `1.25rem`, 1.4 line-height): Feature cards, terminal headers, and table section titles.
- **Body** (400 weight, `1rem`, 1.6 line-height, max line length 75ch): Explanatory text, feature descriptions, and documentation prose.
- **Label / Pill** (600 weight, `0.85rem`, 1.2 line-height, `0.02em` tracking): Version tags, pills, and interactive buttons.
- **Mono Data** (500 weight, `0.9rem`, 1.5 line-height): CLI snippets, schema SQL DDL, SHA-256 hashes, and terminal outputs.

### Named Rules
**The Code Is Truth Rule.** Any value representing a command, file name, environment variable, version number, or cryptographic hash must be typeset in monospace font.

## Layout

Spatial model built on a max-width container (`1140px`) centered with fluid horizontal padding (`24px`).

- **Grid Systems**: Auto-fitting responsive grids (`repeat(auto-fit, minmax(320px, 1fr))`) with consistent `24px` gutters.
- **Vertical Cadence**: Rhythmic section spacing of `80px` to `90px` between major content modules.
- **Header Alignment**: Sticky top navigation bar (`70px` height) with backdrop blur (`12px`) and subtle hairline bottom border.

## Elevation & Depth

Layered dark glassmorphism combined with ambient radial illumination.

### Shadow & Glow Vocabulary
- **Ambient Radial Glow**: Top-centered radial blur (`radial-gradient(circle, rgba(59, 130, 246, 0.15) 0%, rgba(6, 182, 212, 0.08) 40%, transparent 70%)` with `80px` blur filter) providing subtle depth behind the hero.
- **Card Rest**: Flat glassmorphic background (`rgba(17, 24, 39, 0.7)`) with backdrop filter (`blur(10px)`).
- **Card Hover Elevation**: `transform: translateY(-3px)` with border color transition to `rgba(59, 130, 246, 0.4)`.
- **Terminal Depth** (`box-shadow: 0 12px 32px rgba(0, 0, 0, 0.4)`): Heavy drop shadow grounding terminal and command observability windows.

### Named Rules
**The Ambient Depth Rule.** Depth is created through translucent glassmorphism (`backdrop-filter: blur(10px)`) and subtle border luminescence rather than aggressive solid shadows.

## Shapes

- **Micro Radii (4px - 6px)**: Inline code badges (`4px`), buttons and copy triggers (`6px`).
- **Container Radii (8px - 10px)**: Navigation action buttons (`8px`), icon badges and command input boxes (`10px`).
- **Surface Radii (14px)**: Feature cards, terminal emulator frames, and command reference tables (`14px`).
- **Pill Radii (9999px)**: Status badges, version chips, and hero taglines (`9999px`).

## Components

### Buttons
- **Shape**: Subtle curved edges (`6px` for inline copy buttons, `8px` for navigation triggers).
- **Primary / Copy**: Dark glass surface (`rgba(255, 255, 255, 0.08)`), crisp hairline border, text primary color.
- **Hover / Focus**: Lightens to `rgba(255, 255, 255, 0.16)` with fast `0.2s` ease transition.

### Badge Pills & Version Tags
- **Style**: Full rounded pills (`9999px`), emerald/blue translucent backgrounds (`rgba(16, 185, 129, 0.1)`), hairline border (`rgba(16, 185, 129, 0.25)`).
- **Typography**: Monospace or semi-bold sans with uppercase status dots (`●`).

### Feature Cards
- **Corner Style**: Rounded `14px`.
- **Background**: Translucent card surface (`rgba(17, 24, 39, 0.7)`) with `blur(10px)`.
- **Border**: Hairline `rgba(255, 255, 255, 0.08)`, shifts to blue hover glow `rgba(59, 130, 246, 0.4)`.
- **Internal Padding**: Generous `28px`.

### Terminal Observability Box
- **Header**: Dark title bar (`rgba(3, 7, 18, 0.8)`) with macOS-style window controls (red, yellow, green 12px dots) and monospace title.
- **Body**: Jet black background (`#030712`), monospace font (`JetBrains Mono`), color-coded terminal streams (prompt cyan, applied blue, hash gray, success emerald).

### Command Reference Table
- **Container**: `14px` border radius with glassmorphic backing.
- **Header**: Dark semi-transparent header (`rgba(3, 7, 18, 0.6)`) with high-contrast text.
- **Cells**: Clean row separators with monospace pill-styled command tags.

## Do's and Don'ts

### Do:
- **Do** format all commands, flags, migration versions, and cryptographic hashes in `JetBrains Mono`.
- **Do** preserve the 1px hairline border structure (`rgba(255, 255, 255, 0.08)`) across all surfaces and cards.
- **Do** use Emerald (`#10b981`) exclusively for verified/clean statuses and Amber (`#f59e0b`) for warnings/protections.
- **Do** maintain high visual contrast ratios (> 7:1) between text and dark surfaces for maximum technical readability.

### Don't:
- **Don't** use solid bright white or generic gray backgrounds; keep the canvas grounded in Obsidian Black (`#0b0f19`).
- **Don't** apply heavy drop shadows or skeumorphic bevels to buttons or cards.
- **Don't** mix unstyled serif fonts or decorative display scripts into technical surfaces.
- **Don't** clutter interfaces with unmetered accent colors; preserve the calm, high-contrast control room aesthetic.
