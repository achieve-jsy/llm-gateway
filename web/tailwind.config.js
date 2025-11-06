/** @type {import('tailwindcss').Config} */
export default {
  content: [
    './index.html',
    './src/**/*.{vue,js,ts,jsx,tsx}',
  ],
  darkMode: 'class',
  theme: {
    fontFamily: {
      sans: ['-apple-system', 'BlinkMacSystemFont', 'PingFang SC', 'Microsoft YaHei', 'sans-serif'],
      mono: ['Fira Code', 'JetBrains Mono', 'monospace'],
    },
    extend: {
      colors: {
        primary: {
          DEFAULT: '#2BA8A2',
          light: '#3CC4BD',
          dark: '#1E8C86',
          bg: '#E8F6F5',
        },
        accent: {
          DEFAULT: '#FFD23F',
          light: '#FFE47A',
          dark: '#E6B800',
        },
        coral: {
          DEFAULT: '#EF6C4A',
          light: '#FF8A6A',
          dark: '#D45233',
        },
        cream: '#FFF8E7',
        sky: '#5DADE2',
        surface: {
          base: '#EFF8F7',
          card: '#FFFFFF',
        },
        success: '#27AE60',
        warning: '#EAB308',
        error: '#E74C3C',
        info: '#5DADE2',
      },
      borderRadius: {
        sm: '4px',
        DEFAULT: '8px',
        md: '12px',
        lg: '16px',
        xl: '20px',
        pill: '999px',
      },
      boxShadow: {
        sm: '0 1px 4px rgba(0, 0, 0, 0.08)',
        DEFAULT: '0 2px 8px rgba(0, 0, 0, 0.12)',
        md: '0 4px 16px rgba(0, 0, 0, 0.12)',
        lg: '0 8px 32px rgba(0, 0, 0, 0.16)',
        card: '0 2px 10px rgba(43, 168, 162, 0.10)',
        'coral-glow': '0 2px 10px rgba(239, 108, 74, 0.35)',
        'teal-glow': '0 2px 10px rgba(43, 168, 162, 0.30)',
        'accent-glow': '0 2px 10px rgba(255, 210, 63, 0.40)',
        'sky-glow': '0 2px 8px rgba(93, 173, 226, 0.30)',
      },
    },
  },
  plugins: [],
}
