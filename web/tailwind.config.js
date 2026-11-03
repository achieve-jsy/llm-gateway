/** @type {import('tailwindcss').Config} */
export default {
  content: [
    './index.html',
    './src/**/*.{vue,js,ts,jsx,tsx}',
  ],
  darkMode: 'class',
  theme: {
    // DocuForge typography stack
    fontFamily: {
      sans:     ['Inter', '-apple-system', 'BlinkMacSystemFont', 'sans-serif'],
      display:  ['Plus Jakarta Sans', 'Inter', 'sans-serif'],
      mono:     ['Fira Code', 'JetBrains Mono', 'monospace'],
    },
    extend: {
      // ── DocuForge color palette ──────────────────────────────────────────
      colors: {
        primary: {
          DEFAULT: '#2563EB',   // Blue — links, active nav, primary CTAs
          hover:   '#1D4ED8',
          light:   '#EFF6FF',   // primary-bg
        },
        secondary: {
          DEFAULT: '#7C3AED',   // Purple — syntax accents, version badges, API methods
        },
        tertiary: {
          DEFAULT: '#6B7280',   // Gray — body text, secondary info, metadata
        },
        surface: {
          DEFAULT: '#FFFFFF',   // Content area, cards, panels
          page:    '#FAFAFA',   // Page background
        },
        success: {
          DEFAULT: '#16A34A',   // GET badge, 2xx responses
          bg:      '#F0FDF4',
        },
        warning: {
          DEFAULT: '#CA8A04',   // Deprecated, rate limits
          bg:      '#FFF7ED',
        },
        error: {
          DEFAULT: '#DC2626',   // DELETE badge, error responses
          hover:   '#B91C1C',
          bg:      '#FEF2F2',
        },
        info: {
          DEFAULT: '#2563EB',   // Info callouts, note blocks
          bg:      '#EFF6FF',
        },
        // Neutral scale (Tailwind zinc-compatible naming)
        zinc: {
          50:  '#FAFAFA',
          100: '#F4F4F5',
          200: '#E4E4E7',
          300: '#D4D4D8',
          400: '#A1A1AA',
          500: '#71717A',
          600: '#52525B',
          700: '#3F3F46',
          800: '#27272A',
          900: '#18181B',
        },
      },

      // ── DocuForge spacing scale ──────────────────────────────────────────
      spacing: {
        '0.5': '4px',
        '1':   '8px',
        '2':   '16px',
        '3':   '24px',
        '4':   '32px',
        '6':   '48px',
        '8':   '64px',
        '12':  '96px',
      },

      // ── DocuForge border radius ──────────────────────────────────────────
      borderRadius: {
        none: '0px',
        sm:   '4px',    // Inline code, small badges
        DEFAULT: '8px', // Cards, buttons, inputs, panels
        md:   '8px',
        lg:   '12px',   // Code blocks, callout boxes
        xl:   '16px',   // Feature cards, hero sections
        full: '9999px', // Pills, version badges, avatars
      },

      // ── DocuForge elevation / shadows ───────────────────────────────────
      boxShadow: {
        subtle:  '0 1px 2px rgba(0,0,0,0.04)',          // Cards at rest, table rows
        DEFAULT: '0 4px 12px rgba(0,0,0,0.06)',          // Hovered cards, dropdowns
        medium:  '0 4px 12px rgba(0,0,0,0.06)',
        large:   '0 12px 32px rgba(0,0,0,0.10)',         // Modals, expanded panels
        overlay: '0 0 0 9999px rgba(0,0,0,0.35)',        // Backdrop behind modals
        code:    'inset 0 0 0 1px #E4E4E7',              // Code block inset border
      },

      // ── DocuForge max-width / layout helpers ────────────────────────────
      maxWidth: {
        prose: '768px',
      },

      // ── DocuForge font sizes ─────────────────────────────────────────────
      fontSize: {
        // [size, { lineHeight, letterSpacing, fontWeight }]
        'display':  ['40px', { lineHeight: '1.15', letterSpacing: '0.02em', fontWeight: '800' }],
        'headline': ['30px', { lineHeight: '1.2',  letterSpacing: '0.01em', fontWeight: '700' }],
        'subhead':  ['22px', { lineHeight: '1.3',  fontWeight: '600' }],
        'body-lg':  ['18px', { lineHeight: '1.7' }],
        'body':     ['16px', { lineHeight: '1.7' }],
        'body-sm':  ['14px', { lineHeight: '1.6',  letterSpacing: '0.01em' }],
        'caption':  ['12px', { lineHeight: '1.4',  letterSpacing: '0.02em', fontWeight: '500' }],
        'overline': ['11px', { lineHeight: '1.4',  letterSpacing: '0.08em', fontWeight: '600' }],
        'code':     ['14px', { lineHeight: '1.7' }],
      },
    },
  },
  plugins: [],
}
